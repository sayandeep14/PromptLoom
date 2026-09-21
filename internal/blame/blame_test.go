package blame

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const loomToml = "[project]\nname = \"t\"\nversion = \"0.0.0\"\n[paths]\nprompts = \"prompts\"\nblocks = \"blocks\"\noverlays = \"overlays\"\nout = \"dist\"\n"

// repo builds a git repository whose history is controlled by the test.
type repo struct {
	t   *testing.T
	dir string
	n   int
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	r := &repo{t: t, dir: dir}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=Ann Author", "GIT_AUTHOR_EMAIL=ann@example.com",
		"GIT_COMMITTER_NAME=Ann Author", "GIT_COMMITTER_EMAIL=ann@example.com",
		"GIT_AUTHOR_DATE="+r.date(), "GIT_COMMITTER_DATE="+r.date(),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// date gives every commit its own day so date filters are deterministic.
func (r *repo) date() string {
	return time.Date(2024, 1, 1+r.n, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)
}

func (r *repo) write(rel, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) {
	r.t.Helper()
	r.n++
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
}

const reviewerV1 = `prompt Reviewer {
  persona :=
    You review code.

  constraints :=
    - Be kind
    - Be brief
}
`

func TestParseLinePorcelain(t *testing.T) {
	hash := strings.Repeat("ab", 20)
	data := hash + " 3 3 1\nauthor Ann Author\nauthor-mail <ann@example.com>\nauthor-time 1704110400\nauthor-tz +0000\ncommitter Someone Else\nsummary Tighten constraints\nfilename prompts/R.loom\n\t  - Be kind\n"
	ci, err := parseLinePorcelain([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if ci.Hash != "abababa" || ci.Author != "Ann Author" || ci.Summary != "Tighten constraints" || !ci.Date.Equal(time.Unix(1704110400, 0)) {
		t.Errorf("%+v", ci)
	}
	// a source line that looks like a hash must not be read as a header
	data = hash + " 1 1 1\nauthor A\nsummary s\nfilename f\n\t" + strings.Repeat("cd", 20) + " 9 9 9\n"
	if ci, _ := parseLinePorcelain([]byte(data)); ci.Hash != "abababa" {
		t.Errorf("%+v", ci)
	}
	// sha-256 repositories use 64-character ids
	sha256 := strings.Repeat("ef", 32)
	if ci, err := parseLinePorcelain([]byte(sha256 + " 1 1 1\nauthor A\nsummary s\n")); err != nil || ci.Hash != "efefefe" {
		t.Errorf("%+v %v", ci, err)
	}
	if _, err := parseLinePorcelain([]byte("garbage\n")); err == nil {
		t.Error("no hash means no data")
	}
	// uncommitted lines blame to the all-zero id
	zero := strings.Repeat("0", 40)
	if _, err := parseLinePorcelain([]byte(zero + " 1 1 1\nauthor Not Committed Yet\nsummary Version of x\n")); err == nil {
		t.Error("an uncommitted line has no commit")
	}
}

func TestTruncateAndCapitalizeAreRuneSafe(t *testing.T) {
	if truncate("short", 10) != "short" || truncate("abcdef", 3) != "abc…" {
		t.Error("ascii")
	}
	got := truncate("日本語日本語", 3)
	if got != "日本語…" {
		t.Errorf("%q (cut mid-character?)", got)
	}
	if capitalizeFirst("constraints") != "Constraints" || capitalizeFirst("") != "" || capitalizeFirst("élan") != "Élan" {
		t.Errorf("%q", capitalizeFirst("élan"))
	}
}

func TestRelativizeAndOrigin(t *testing.T) {
	if relativize(filepath.Join("/p", "prompts", "A.loom"), "/p") != filepath.Join("prompts", "A.loom") || relativize("/x/y", "") != "/x/y" {
		t.Error("relativize")
	}
	if got := fieldsToBlame("constraints"); len(got) != 1 || got[0] != "constraints" {
		t.Error(got)
	}
	if len(fieldsToBlame("")) != 9 {
		t.Error("all fields")
	}
}

func TestBlameAttributesEachItemToItsCommit(t *testing.T) {
	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Reviewer.prompt.loom", reviewerV1)
	r.commit("first version")
	r.write("prompts/Reviewer.prompt.loom", strings.Replace(reviewerV1, "- Be brief", "- Be brief\n    - Cite sources", 1))
	r.commit("Require citations")

	results, err := RunBlame("Reviewer", "constraints", "", "", r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Field != "constraints" || len(results[0].Items) != 3 {
		t.Fatalf("%+v", results)
	}
	items := results[0].Items
	if items[0].Commit.Summary != "first version" || items[2].Commit.Summary != "Require citations" {
		t.Errorf("%+v", items)
	}
	if items[0].File != filepath.Join("prompts", "Reviewer.prompt.loom") || items[0].Line != 6 || items[0].Commit.Author != "Ann Author" || items[0].Origin != "block composition" && items[0].Origin != "direct" {
		t.Errorf("%+v", items[0])
	}
	if items[0].Untracked {
		t.Error("committed lines are tracked")
	}

	// instruction filter (case-insensitive substring)
	got, _ := RunBlame("Reviewer", "constraints", "", "CITE", r.dir)
	if len(got) != 1 || len(got[0].Items) != 1 || !strings.Contains(got[0].Items[0].Value, "Cite") {
		t.Errorf("%+v", got)
	}

	// --since a date between the commits keeps only the newer item
	got, err = RunBlame("Reviewer", "constraints", "2024-01-03", "", r.dir)
	if err != nil || len(got) != 1 || len(got[0].Items) != 1 || !strings.Contains(got[0].Items[0].Value, "Cite") {
		t.Errorf("%+v %v", got, err)
	}
	// ... and so does a git ref (the newest commit)
	if got, err = RunBlame("Reviewer", "constraints", "HEAD", "", r.dir); err != nil || len(got) != 1 || len(got[0].Items) != 1 {
		t.Errorf("%+v %v", got, err)
	}
}

func TestSinceThatIsNeitherADateNorARefIsAnError(t *testing.T) {
	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Reviewer.prompt.loom", reviewerV1)
	r.commit("first")
	for _, bad := range []string{"2024-13-45", "no-such-ref", "--output=/tmp/x"} {
		if _, err := RunBlame("Reviewer", "constraints", bad, "", r.dir); err == nil || !strings.Contains(err.Error(), "--since") {
			t.Errorf("--since %q: %v", bad, err)
		}
	}
}

func TestUncommittedLinesAreUntracked(t *testing.T) {
	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Reviewer.prompt.loom", reviewerV1)
	r.commit("first")
	r.write("prompts/Reviewer.prompt.loom", strings.Replace(reviewerV1, "- Be brief", "- Be brief\n    - Work in progress", 1))

	results, err := RunBlame("Reviewer", "constraints", "", "", r.dir)
	if err != nil {
		t.Fatal(err)
	}
	last := results[0].Items[2]
	if !last.Untracked || last.Commit.Hash != "" {
		t.Errorf("an edited-but-uncommitted line must be reported as untracked, got %+v", last)
	}
	if results[0].Items[0].Untracked {
		t.Error("committed lines are still tracked")
	}
}

func TestBlameErrors(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, "loom.toml"), []byte(loomToml), 0o644)
	// t.TempDir may sit inside a git checkout on some machines; anchor with GIT_CEILING_DIRECTORIES.
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
	if _, err := RunBlame("X", "", "", "", plain); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("%v", err)
	}
	if _, err := BuildChangelog(plain, "", ""); err == nil {
		t.Error("changelog needs git too")
	}

	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Reviewer.prompt.loom", reviewerV1)
	r.commit("first")
	if _, err := RunBlame("Missing", "", "", "", r.dir); err == nil || !strings.Contains(err.Error(), "Missing") {
		t.Errorf("%v", err)
	}
}

func TestChangelog(t *testing.T) {
	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Reviewer.prompt.loom", reviewerV1)
	r.write("blocks/Guard.block.loom", "block Guard {\n  constraints :=\n    - Stay safe\n}\n")
	r.commit("create reviewer")

	v2 := strings.Replace(reviewerV1, "- Be brief", "- Be thorough", 1)
	v2 = strings.Replace(v2, "You review code.", "You review Go code.", 1)
	v2 = strings.Replace(v2, "prompt Reviewer {\n", "prompt Reviewer {\n  use Guard\n", 1)
	r.write("prompts/Reviewer.prompt.loom", v2)
	r.commit("rework reviewer")

	r.write("blocks/Guard.block.loom", "block Guard {\n  constraints :=\n    - Stay safe\n    - Stay private\n}\n")
	r.commit("guard update")

	cl, err := BuildChangelog(r.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(cl) != 1 || cl[0].Name != "Reviewer" {
		t.Fatalf("%+v", cl)
	}
	var msgs []string
	for _, e := range cl[0].Entries {
		msgs = append(msgs, e.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{
		"Prompt created", "Added block Guard", `Constraints added: "- Be thorough"`, `Constraints removed: "- Be brief"`,
		"Persona updated", `Constraints added: "- Stay private"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, "+=") || strings.Contains(all, "-=") {
		t.Errorf("v2 has no += / -= operators:\n%s", all)
	}
	for _, e := range cl[0].Entries {
		if e.Author != "Ann Author" || e.Date.IsZero() {
			t.Errorf("%+v", e)
		}
	}

	// --since a date and a ref
	cl, err = BuildChangelog(r.dir, "2024-01-03", "Reviewer")
	if err != nil || len(cl) != 1 || len(cl[0].Entries) != 1 || !strings.Contains(cl[0].Entries[0].Message, "Stay private") {
		t.Errorf("%+v %v", cl, err)
	}
	if cl, err = BuildChangelog(r.dir, "HEAD~1", ""); err != nil || len(cl) != 1 || len(cl[0].Entries) != 1 {
		t.Errorf("%+v %v", cl, err)
	}
	// prompt filter
	if cl, _ := BuildChangelog(r.dir, "", "Other"); len(cl) != 0 {
		t.Errorf("%+v", cl)
	}
	// a bad ref is an error, not an empty history
	if _, err := BuildChangelog(r.dir, "no-such-ref", ""); err == nil {
		t.Error("unknown --since ref must fail")
	}
	if _, err := BuildChangelog(r.dir, "--output=x", ""); err == nil {
		t.Error("option-like --since must fail")
	}
}

func TestChangelogHandlesAuthorsWithSeparatorsAndDeletedPrompts(t *testing.T) {
	r := newRepo(t)
	r.write("loom.toml", loomToml)
	r.write("prompts/Temp.prompt.loom", "prompt Temp {\n  persona :=\n    p\n}\n")
	r.write("prompts/Keep.prompt.loom", "prompt Keep {\n  persona :=\n    p\n}\n")
	r.n++
	r.git("add", "-A")
	cmd := exec.Command("git", "commit", "-q", "-m", "subject | with | pipes")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_AUTHOR_NAME=Ann | Pipe", "GIT_AUTHOR_EMAIL=a@b.c", "GIT_COMMITTER_NAME=Ann | Pipe", "GIT_COMMITTER_EMAIL=a@b.c",
		"GIT_AUTHOR_DATE="+r.date(), "GIT_COMMITTER_DATE="+r.date())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	r.git("rm", "-q", "prompts/Temp.prompt.loom")
	r.commit("drop temp")

	cl, err := BuildChangelog(r.dir, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range cl {
		names = append(names, c.Name)
		for _, e := range c.Entries {
			if c.Name == "Keep" && e.Author != "Ann | Pipe" {
				t.Errorf("author was cut at the separator: %q", e.Author)
			}
		}
	}
	// registry prompts first (Keep), then prompts that only exist in history (Temp)
	if strings.Join(names, ",") != "Keep,Temp" {
		t.Errorf("%v", names)
	}
	temp := cl[1].Entries
	if len(temp) != 2 || !strings.Contains(temp[0].Message, "deleted") && !strings.Contains(temp[1].Message, "deleted") {
		t.Errorf("%+v", temp)
	}
}

func TestSliceDiffAndLooksLikeDate(t *testing.T) {
	a, r := sliceDiff([]string{"x", "y"}, []string{"y", "z"})
	if strings.Join(a, ",") != "z" || strings.Join(r, ",") != "x" {
		t.Errorf("%v %v", a, r)
	}
	for s, want := range map[string]bool{"2024-01-02": true, "2024-01-02T10:00:00": true, "HEAD~5": false, "v1.0": false, "": false} {
		if looksLikeDate(s) != want {
			t.Errorf("looksLikeDate(%q)", s)
		}
	}
}
