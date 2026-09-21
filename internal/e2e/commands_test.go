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
		{"graph --help", []string{"graph", "--help"}, 0, nil},
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
		{"weave --all needs values", []string{"weave", "--all"}, 1, []string{"failed to render", "repo_name"}},
		{"ci", []string{"ci"}, 0, []string{"PASSED"}},
		{"import", []string{"import", "--help"}, 0, nil},
		{"recipe list", []string{"recipe", "list"}, 0, []string{"reviewer", "api-designer"}},
		{"recipe unknown", []string{"recipe", "apply", "nope"}, 1, []string{"not found"}},
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
