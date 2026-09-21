package context

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveFileRelativeAndAbsolute(t *testing.T) {
	dir := t.TempDir()
	abs := write(t, dir, "src/main.go", "package main\n")
	for _, spec := range []string{"file:src/main.go", "text:src/main.go", "file:" + abs} {
		s, err := Resolve(spec, dir)
		if err != nil || s.Content != "package main\n" || !strings.HasPrefix(s.Label, "File: ") {
			t.Errorf("%s: %+v %v", spec, s, err)
		}
	}
	if _, err := Resolve("file:missing.go", dir); err == nil || !strings.Contains(err.Error(), "missing.go") {
		t.Errorf("missing file: %v", err)
	}
}

func TestResolveUnknownSourceListsTheChoices(t *testing.T) {
	_, err := Resolve("ftp://x", t.TempDir())
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"file:", "dir:", "git:diff", "git:staged", "stdin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error should list %q: %v", want, err)
		}
	}
}

func TestResolveDirSkipsSubdirectoriesAndCredentials(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.go", "package app\n")
	write(t, dir, "README.md", "# hi\n")
	write(t, dir, "sub/nested.go", "package nested\n")
	// the files an accidental `dir:.` must never paste into a prompt
	write(t, dir, ".loom.secret", "GEMINI_API_KEY=abc123-real-key\n")
	write(t, dir, ".env", "DB_PASSWORD=hunter2\n")
	write(t, dir, ".env.production", "TOKEN=prod-token\n")
	write(t, dir, "server.pem", "-----BEGIN PRIVATE KEY-----\n")
	write(t, dir, "id_rsa", "PRIVATE\n")

	s, err := Resolve("dir:.", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"abc123-real-key", "hunter2", "prod-token", "PRIVATE"} {
		if strings.Contains(s.Content, leak) {
			t.Errorf("dir: attached a credential (%q)", leak)
		}
	}
	for _, want := range []string{"### app.go", "package app", "### README.md"} {
		if !strings.Contains(s.Content, want) {
			t.Errorf("dir: missing %q:\n%s", want, s.Content)
		}
	}
	if strings.Contains(s.Content, "nested") {
		t.Error("subdirectories are not descended into")
	}
	if _, err := Resolve("dir:nope", dir); err == nil {
		t.Error("a missing directory is an error")
	}
}

func TestExplicitFileSourceIsNotFiltered(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".env.example", "KEY=\n")
	if _, err := Resolve("file:.env.example", dir); err != nil {
		t.Errorf("an explicitly named file is the user's deliberate choice: %v", err)
	}
}

func TestIsSensitiveName(t *testing.T) {
	yes := []string{".env", ".env.local", "prod.env", ".loom.secret", ".loomsecret", ".loom.config", "id_rsa", "id_ed25519",
		"server.pem", "tls.key", "cert.p12", "app.keystore", "credentials.json", "secrets.yaml", ".npmrc", ".netrc",
		"deep/path/.env", "DB.SECRET", "API.SECRETS"}
	no := []string{"main.go", "README.md", ".env.example", ".env.sample", "config.yaml", "keyboard.go", "monkey.txt",
		"environment.md", "pem.md", "loom.toml", ""}
	for _, n := range yes {
		if !IsSensitiveName(n) {
			t.Errorf("%q should be treated as sensitive", n)
		}
	}
	for _, n := range no {
		if n != "" && IsSensitiveName(n) {
			t.Errorf("%q should not be treated as sensitive", n)
		}
	}
}

// ---- git ----

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	write(t, dir, "a.txt", "one\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return dir
}

func TestGitDiffAndStaged(t *testing.T) {
	dir := gitRepo(t)
	write(t, dir, "a.txt", "one\ntwo\n")

	s, err := Resolve("git:diff", dir)
	if err != nil || s.Label != "diff" || !strings.Contains(s.Content, "+two") {
		t.Errorf("git:diff: %+v %v", s, err)
	}
	if s, _ := Resolve("git:staged", dir); strings.Contains(s.Content, "+two") {
		t.Error("nothing is staged yet")
	}
	cmd := exec.Command("git", "add", "a.txt")
	cmd.Dir = dir
	cmd.Run()
	if s, err := Resolve("git:staged", dir); err != nil || s.Label != "staged" || !strings.Contains(s.Content, "+two") {
		t.Errorf("git:staged: %+v %v", s, err)
	}
	if _, err := Resolve("git:diff", t.TempDir()); err == nil {
		t.Error("outside a repository, git:diff must fail with an error")
	}
}

func TestStdinSource(t *testing.T) {
	old := os.Stdin
	defer func() { os.Stdin = old }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	go func() { w.WriteString("line one\nline two"); w.Close() }()
	s, err := Resolve("stdin", t.TempDir())
	if err != nil || s.Label != "stdin" || s.Content != "line one\nline two\n" {
		t.Errorf("%+v %v", s, err)
	}
}

// ---- rendering ----

func TestAppendContextSection(t *testing.T) {
	if got := AppendContextSection("body", nil); got != "body" {
		t.Errorf("no sources must leave the body untouched: %q", got)
	}
	out := AppendContextSection("# P\n", []Source{
		{Label: "File: src/a.go", Content: "package a"},
		{Label: "File: notes.txt", Content: "plain\n"},
		{Label: "diff", Content: "+x\n"},
	})
	for _, want := range []string{"## Attached Context", "### File: src/a.go", "```go\npackage a\n```", "### File: notes.txt\nplain\n", "```diff\n+x\n```"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A Markdown file (or any text with ```) used to close its own fence and spill into the prompt.
func TestFenceIsLongerThanAnyBacktickRunInTheContent(t *testing.T) {
	content := "# Doc\n\n```bash\necho hi\n```\n\n````\nfour\n````\n"
	out := AppendContextSection("", []Source{{Label: "File: README.md", Content: content}})
	if !strings.Contains(out, "`````markdown\n") {
		t.Errorf("the fence must be longer than the 4-backtick run inside the content:\n%s", out)
	}
	if strings.Count(out, "`````\n") != 1 {
		t.Errorf("expected exactly one closing fence:\n%s", out)
	}
	// with no backticks inside, the ordinary 3-backtick fence is used
	plain := AppendContextSection("", []Source{{Label: "File: a.go", Content: "package a\n"}})
	if !strings.Contains(plain, "```go\n") || strings.Contains(plain, "````") {
		t.Errorf("ordinary content should use a plain fence:\n%s", plain)
	}
}

func TestGuessLang(t *testing.T) {
	cases := map[string]string{
		"File: /x/Main.GO": "go", "File: a.tsx": "tsx", "File: a.yml": "yaml", "File: Makefile": "",
		"diff": "diff", "staged": "diff", "File: notes.txt": "", "File: a.PY": "python",
	}
	for label, want := range cases {
		if got := guessLang(label); got != want {
			t.Errorf("guessLang(%q) = %q, want %q", label, got, want)
		}
	}
}

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 || EstimateTokens("abcd") != 1 || EstimateTokens(strings.Repeat("x", 400)) != 100 {
		t.Error("characters / 4")
	}
}

// ---- bundles ----

func TestParseBundle(t *testing.T) {
	b, err := parseBundle("web", `
// a comment
# another
context web {
  include:
    - src/*.go
    - docs/*.md
  exclude:
    - *_test.go
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.Include, ",") != "src/*.go,docs/*.md" || strings.Join(b.Exclude, ",") != "*_test.go" || b.Name != "web" {
		t.Errorf("%+v", b)
	}
}

func TestLoadBundle(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "contexts/api.context", "context api {\n  include:\n    - *.go\n}\n")
	b, err := LoadBundle("api", dir)
	if err != nil || len(b.Include) != 1 {
		t.Errorf("%+v %v", b, err)
	}
	if _, err := LoadBundle("nope", dir); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("missing bundle: %v", err)
	}
	for _, bad := range []string{"../api", "../../etc/passwd", "a/b", ".hidden", ""} {
		if _, err := LoadBundle(bad, dir); err == nil {
			t.Errorf("bundle name %q must be refused", bad)
		}
	}
}

func TestResolveBundleGlobsAndExcludes(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "src/a.go", "package a\n")
	write(t, dir, "src/a_test.go", "package a\n")
	write(t, dir, "src/deep/b.go", "package b\n")
	write(t, dir, "docs/x.md", "# x\n")
	b := &Bundle{Name: "t", Include: []string{"src/*.go", "docs/*.md"}, Exclude: []string{"*_test.go"}}
	got, err := ResolveBundle(b, dir)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, s := range got {
		labels = append(labels, s.Label)
	}
	want := "File: src/a.go,File: docs/x.md"
	if strings.Join(labels, ",") != want {
		t.Errorf("got %v, want %s (excludes apply; '*' does not cross directories)", labels, want)
	}
}

func TestBundleNeverAttachesCredentials(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "app.go", "package app\n")
	write(t, dir, ".env", "DB_PASSWORD=hunter2\n")
	write(t, dir, ".loom.secret", "KEY=abc\n")
	write(t, dir, "key.pem", "PRIVATE\n")
	got, err := ResolveBundle(&Bundle{Include: []string{"*", ".*"}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if strings.Contains(s.Content, "hunter2") || strings.Contains(s.Content, "abc") || strings.Contains(s.Content, "PRIVATE") {
			t.Errorf("a credential file was attached: %s", s.Label)
		}
	}
	if len(got) != 1 || got[0].Label != "File: app.go" {
		t.Errorf("only the ordinary file should remain: %+v", got)
	}
}

// A bundle is project content and may come from a repository you did not write.
func TestBundleCannotReachOutsideTheProject(t *testing.T) {
	outer := t.TempDir()
	write(t, outer, "outside-secret.txt", "TOP SECRET")
	proj := filepath.Join(outer, "proj")
	write(t, proj, "ok.txt", "fine")

	for _, pattern := range []string{"../outside-secret.txt", "../*", filepath.Join(outer, "outside-secret.txt"), "sub/../../outside-secret.txt", `..\outside-secret.txt`} {
		_, err := ResolveBundle(&Bundle{Name: "evil", Include: []string{pattern}}, proj)
		if err == nil || !strings.Contains(err.Error(), "inside the project") {
			t.Errorf("pattern %q must be refused: %v", pattern, err)
		}
	}
}

func TestBundleDoesNotFollowSymlinksOutOfTheProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	outer := t.TempDir()
	secret := write(t, outer, "outside-secret.txt", "TOP SECRET")
	proj := filepath.Join(outer, "proj")
	write(t, proj, "ok.txt", "fine")
	if err := os.Symlink(secret, filepath.Join(proj, "link.txt")); err != nil {
		t.Skip("cannot create symlinks here")
	}
	got, err := ResolveBundle(&Bundle{Include: []string{"*.txt"}}, proj)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if strings.Contains(s.Content, "TOP SECRET") {
			t.Errorf("a symlink pointing outside the project was followed: %s", s.Label)
		}
	}
	if len(got) != 1 || got[0].Label != "File: ok.txt" {
		t.Errorf("got %+v", got)
	}
}
