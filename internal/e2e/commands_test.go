package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A realistic project (built by `loom init` + the reviewer recipe) run through the documented
// commands. Each row is a command, the exit code it must give and text it must print. This is
// the net under the CLI layer: the packages have unit tests, this checks the wiring.
func TestCommandsOnARealProject(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	must := func(args ...string) string {
		t.Helper()
		out, code := runLoom(t, bin, dir, args...)
		if code != 0 {
			t.Fatalf("loom %v: exit %d\n%s", args, code, out)
		}
		return out
	}
	must("init")
	must("recipe", "apply", "reviewer", "--language", "Go")
	must("thread", "prompt", "Extra", "--inherits", "BaseEngineer")
	must("weave", "--all", "--set", "repo_name=demo")

	// git history for blame / changelog
	git := func(args ...string) {
		t.Helper()
		out, code := runLoom(t, "git", dir, args...)
		if code != 0 {
			t.Skipf("git unavailable: %v %s", args, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("-c", "user.name=T", "-c", "user.email=t@t", "add", "-A")
	git("-c", "user.name=T", "-c", "user.email=t@t", "commit", "-q", "-m", "initial")

	cases := []struct {
		name string
		args []string
		code int
		want []string
	}{
		{"list", []string{"list"}, 0, []string{"CodeReviewer", "SecurityChecklist", "Prompts (6)"}},
		{"list --prompts", []string{"list", "--prompts"}, 0, []string{"GoReviewer"}},
		{"inspect", []string{"inspect"}, 0, []string{"Errors           0"}},
		{"trace", []string{"trace", "GoReviewer"}, 0, []string{"GoReviewer", "CodeReviewer", "BaseEngineer"}},
		{"trace unknown", []string{"trace", "Nope"}, 1, []string{"not found"}},
		{"unravel", []string{"unravel", "CodeReviewer"}, 0, []string{"senior Go engineer"}},
		{"unravel --with-source", []string{"unravel", "CodeReviewer", "--with-source"}, 0, []string{"CodeReviewer"}},
		{"contract", []string{"contract", "GoReviewer"}, 0, []string{"recommendation"}},
		{"stats", []string{"stats", "CodeReviewer"}, 0, []string{"CodeReviewer"}},
		{"doctor", []string{"doctor"}, 0, []string{"healthy"}},
		{"smells", []string{"smells"}, 0, nil},
		{"minimize", []string{"minimize"}, 0, nil},
		{"audit", []string{"audit"}, 0, []string{"PASS"}},
		{"audit one", []string{"audit", "SecurityReviewer"}, 0, []string{"SecurityReviewer"}},
		{"graph one prompt", []string{"graph", "CodeReviewer", "--no-interactive"}, 0, []string{"Inherits from", "BaseEngineer", "Inherited by", "GoReviewer"}},
		{"graph one prompt mermaid", []string{"graph", "GoReviewer", "--format", "mermaid"}, 0, []string{"CodeReviewer --> GoReviewer", "-.->|block|"}},
		{"graph unknown", []string{"graph", "Nope", "--no-interactive"}, 1, []string{"no prompt or block named"}},
		{"impact prompt", []string{"impact", "BaseEngineer"}, 0, []string{"affects", "CodeReviewer", "GoReviewer"}},
		{"impact block json", []string{"impact", "SecurityChecklist", "--json"}, 0, []string{`"kind": "block"`, "SecurityReviewer"}},
		{"impact unknown suggests", []string{"impact", "BaseEnginer"}, 1, []string{`did you mean "BaseEngineer"`}},
		{"todos", []string{"todos"}, 0, nil},
		{"stale", []string{"stale"}, 0, nil},
		{"fingerprint", []string{"fingerprint", "CodeReviewer"}, 0, []string{"sha256:"}},
		{"lock", []string{"lock"}, 0, nil},
		{"check-lock", []string{"check-lock"}, 0, []string{"matches"}},
		{"diff prompts", []string{"diff", "CodeReviewer", "TestWriter"}, 0, nil},
		{"diff against dist", []string{"diff", "--all", "--against-dist"}, 0, nil},
		{"review", []string{"review"}, 0, nil},
		{"fmt --check", []string{"fmt", "--check"}, 0, nil},
		{"fmt --migrate --check on v2", []string{"fmt", "--migrate", "--check"}, 0, []string{"Would migrate 0 of"}},
		{"mcp manifest", []string{"mcp", "manifest"}, 0, []string{"reviewer"}},
		{"journal add", []string{"journal", "add", "Tightened the reviewer", "--prompt", "CodeReviewer"}, 0, nil},
		{"journal list", []string{"journal", "list"}, 0, []string{"Tightened the reviewer"}},
		{"blame", []string{"blame", "CodeReviewer"}, 0, []string{"initial"}},
		{"changelog", []string{"changelog"}, 0, []string{"Prompt created"}},
		{"weave one --stdout", []string{"weave", "CodeReviewer", "--stdout", "--set", "repo_name=demo"}, 0, []string{"# CodeReviewer"}},
		{"weave format", []string{"weave", "CodeReviewer", "--stdout", "--format", "plain", "--set", "repo_name=demo"}, 0, nil},
		{"weave bad format", []string{"weave", "CodeReviewer", "--stdout", "--format", "nonsense", "--set", "repo_name=demo"}, 1, nil},
		{"deploy without targets", []string{"deploy", "--check"}, 1, []string{"no [[targets]]"}},
		{"eval without suites", []string{"eval"}, 1, []string{"no eval suites found"}},
		{"weave --all needs values", []string{"weave", "--all"}, 1, []string{"failed to render", "repo_name"}},
		{"ci", []string{"ci"}, 0, []string{"PASSED"}},
		{"import", []string{"import", "--help"}, 0, nil},
		{"recipe list", []string{"recipe", "list"}, 0, []string{"reviewer", "api-designer"}},
		{"recipe unknown", []string{"recipe", "apply", "nope"}, 1, []string{"not found"}},
		{"doctor --system", []string{"doctor", "--system"}, 0, []string{"project", "6 prompts", "git"}},
		{"completion bash", []string{"completion", "bash"}, 0, []string{"__start_loom", "complete"}},
		{"completion zsh", []string{"completion", "zsh"}, 0, []string{"#compdef"}},
		{"completion fish", []string{"completion", "fish"}, 0, []string{"complete -c loom"}},
		{"completion powershell", []string{"completion", "powershell"}, 0, []string{"Register-ArgumentCompleter"}},
		{"complete prompt names", []string{"__complete", "weave", ""}, 0, []string{"CodeReviewer\tprompt · inherits BaseEngineer", "TestWriter"}},
		{"complete prefix", []string{"__complete", "trace", "Sec"}, 0, []string{"SecurityReviewer"}},
		{"complete graph offers blocks", []string{"__complete", "graph", "Sec"}, 0, []string{"SecurityChecklist\tblock"}},
		{"complete format values", []string{"__complete", "weave", "--format", "j"}, 0, []string{"json-anthropic", "json-openai"}},
		{"complete stops after the name", []string{"__complete", "trace", "CodeReviewer", ""}, 0, []string{"NoFileComp"}},
		{"version", []string{"--version"}, 0, []string{"loom"}},
		{"unknown command", []string{"frobnicate"}, 1, []string{"unknown command"}},
		{"execute unknown", []string{"execute", "nothing"}, 1, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, code := runLoom(t, bin, dir, c.args...)
			if code != c.code {
				t.Fatalf("exit %d, want %d\n%s", code, c.code, out)
			}
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("output lacks %q:\n%s", w, out)
				}
			}
		})
	}

	// state written by commands above
	for _, f := range []string{"loom.lock", "loom/dist/CodeReviewer.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}
}

// Commands that only make sense with a network or model are checked for their failure mode:
// a clear message and a non-zero exit, never a hang or a panic.
func TestCommandsThatNeedServicesFailCleanly(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if _, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatal("init")
	}
	env := func(args ...string) (string, int) { return runLoom(t, bin, dir, args...) }

	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("LOOM_REGISTRY_URL", "")

	if out, code := env("install", "some-pack"); code == 0 || !strings.Contains(strings.ToLower(out), "registry") {
		t.Errorf("install without a registry must explain how to configure one: %d\n%s", code, out)
	}
	if out, code := env("test", "CodeReviewer"); code == 0 && !strings.Contains(out, "skipped") {
		t.Errorf("test without an API key must not pretend to pass: %d\n%s", code, out)
	}
	if out, code := env("summarize", "loom.toml"); code == 0 || !strings.Contains(out, "credentials") && !strings.Contains(out, "API key") {
		t.Errorf("summarize of a config file with no key: %d\n%s", code, out)
	}
	if out, code := env("check-output", "Nope", "missing.md"); code == 0 {
		t.Errorf("check-output on nothing: %d\n%s", code, out)
	}
}

func TestEvalCommandReportsProblemsClearly(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if _, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatal("init")
	}
	if _, code := runLoom(t, bin, dir, "recipe", "apply", "reviewer"); code != 0 {
		t.Fatal("recipe")
	}
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")

	// a broken suite: the error names the file and the mistake
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "Bad.eval.toml"), []byte("prompt = \"CodeReviewer\"\n[[case]]\nname = \"a\"\ninput = \"x\"\n"), 0o644)
	out, code := runLoom(t, bin, dir, "eval")
	if code != 1 || !strings.Contains(out, "Bad.eval.toml") || !strings.Contains(out, "needs at least one criterion") {
		t.Errorf("exit %d\n%s", code, out)
	}

	// a valid suite but no API key: nothing is attempted and the error says what to set
	os.WriteFile(filepath.Join(dir, "evals", "Bad.eval.toml"), []byte("prompt = \"CodeReviewer\"\n[[case]]\nname = \"a\"\ninput = \"x\"\ncriteria = [\"is useful\"]\nvars = { repo_name = \"demo\" }\n"), 0o644)
	out, code = runLoom(t, bin, dir, "eval")
	if code != 1 || !strings.Contains(out, "$GEMINI_API_KEY") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// an unknown suite name lists what exists
	out, code = runLoom(t, bin, dir, "eval", "Nope")
	if code != 1 || !strings.Contains(out, "suites: Bad") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

// `loom init` used to write .loom.config to loom/, where loom execute, loomlocker and the client
// libraries never look, and whose relative secret paths then resolved to loom/loom/....
func TestInitPutsLoomConfigWhereEverythingLooksForIt(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if out, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatalf("init: %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, ".loom.config")); err != nil {
		t.Fatalf(".loom.config must be at the project root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "loom", ".loom.config")); err == nil {
		t.Error("nothing may be written to loom/.loom.config")
	}
	if gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore")); !strings.Contains(string(gi), ".loom.config") || !strings.Contains(string(gi), "loom/.loom.secret") {
		t.Errorf(".gitignore: %s", gi)
	}
	// loom execute now finds it (an unknown command is reported, not a missing config)
	out, code := runLoom(t, bin, dir, "execute", "nothing")
	if code != 1 || strings.Contains(out, "not found") || !strings.Contains(out, "unknown command") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// from a subdirectory too
	sub := filepath.Join(dir, "loom", "src")
	if out, _ := runLoom(t, bin, sub, "execute", "nothing"); !strings.Contains(out, "unknown command") {
		t.Errorf("%s", out)
	}

	// a project made by an older version: init moves the misplaced file instead of duplicating it
	legacy := t.TempDir()
	os.MkdirAll(filepath.Join(legacy, "loom"), 0o755)
	os.WriteFile(filepath.Join(legacy, "loom", ".loom.config"), []byte(`{"custom":{"hello":"echo hi"}}`), 0o644)
	if out, code := runLoom(t, bin, legacy, "init"); code != 0 || !strings.Contains(out, "moved") {
		t.Fatalf("init on a legacy project: %d\n%s", code, out)
	}
	if b, err := os.ReadFile(filepath.Join(legacy, ".loom.config")); err != nil || !strings.Contains(string(b), "echo hi") {
		t.Errorf("the user's config must be kept: %s %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(legacy, "loom", ".loom.config")); err == nil {
		t.Error("the old copy must be gone")
	}
}

func TestRunCommandThroughTheBinary(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if _, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatal("init")
	}
	if _, code := runLoom(t, bin, dir, "recipe", "apply", "reviewer"); code != 0 {
		t.Fatal("recipe")
	}
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	// a dry run needs no key and shows the rendered prompt and the message
	out, code := runLoom(t, bin, dir, "run", "CodeReviewer", "--dry-run", "--input", "review this", "--set", "repo_name=demo")
	if code != 0 || !strings.Contains(out, "senior Generic engineer") && !strings.Contains(out, "engineer") || !strings.Contains(out, "review this") || !strings.Contains(out, "nothing was sent") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// without a key: a clear message, no attempt
	out, code = runLoom(t, bin, dir, "run", "CodeReviewer", "--input", "x", "--set", "repo_name=demo")
	if code != 1 || !strings.Contains(out, "$GEMINI_API_KEY") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// a locked key file: the placeholder is recognised
	t.Setenv("GEMINI_API_KEY", "lk_7f3a9b2c1d4e5a6b")
	out, code = runLoom(t, bin, dir, "run", "CodeReviewer", "--input", "x", "--set", "repo_name=demo")
	if code != 1 || !strings.Contains(out, "LoomLocker token") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// a restricted permission.read refuses an attachment before anything happens
	os.WriteFile(filepath.Join(dir, ".loom.config"), []byte(`{"permission":{"read":["loom/**"],"write":["*"]}}`), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("private"), 0o644)
	out, code = runLoom(t, bin, dir, "run", "CodeReviewer", "--dry-run", "--with", "file:notes.txt", "--set", "repo_name=demo")
	if code != 1 || !strings.Contains(out, "permission.read") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// unknown prompt / missing slot
	if out, code := runLoom(t, bin, dir, "run", "Nope", "--dry-run"); code != 1 || !strings.Contains(out, "not found") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

func TestScoreAndOptimizeCommandsThroughTheBinary(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()
	if _, code := runLoom(t, bin, dir, "init"); code != 0 {
		t.Fatal("init")
	}
	if _, code := runLoom(t, bin, dir, "recipe", "apply", "reviewer"); code != 0 {
		t.Fatal("recipe")
	}
	t.Setenv("GEMINI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")

	// no eval suite for the prompt: a clear error, not a silent zero
	out, code := runLoom(t, bin, dir, "score", "CodeReviewer")
	if code != 1 || !strings.Contains(out, "eval suite") {
		t.Errorf("exit %d\n%s", code, out)
	}
	// with a suite but no API key: score/optimize both fail clearly rather than hang
	os.MkdirAll(filepath.Join(dir, "evals"), 0o755)
	os.WriteFile(filepath.Join(dir, "evals", "CodeReviewer.eval.toml"), []byte("prompt = \"CodeReviewer\"\n[[case]]\nname = \"a\"\ninput = \"review this\"\ncriteria = [\"is useful\"]\nvars = { repo_name = \"demo\" }\n"), 0o644)
	if out, code := runLoom(t, bin, dir, "score", "CodeReviewer"); code != 1 || !strings.Contains(out, "API key") {
		t.Errorf("score: exit %d\n%s", code, out)
	}
	if out, code := runLoom(t, bin, dir, "optimize", "CodeReviewer"); code != 1 || !strings.Contains(out, "API key") {
		t.Errorf("optimize: exit %d\n%s", code, out)
	}
	// unknown prompt (no eval suite for it either)
	if out, code := runLoom(t, bin, dir, "score", "Nope"); code != 1 || !strings.Contains(out, "no eval suite") {
		t.Errorf("exit %d\n%s", code, out)
	}
}
