package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDeployWritesTargets(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loom.toml"), `[project]
name = "deploy-test"
version = "0.1.0"

[paths]
prompts = "prompts"
blocks = "blocks"
out = "dist/prompts"

[render]
default_format = "markdown"
include_metadata = true

[[targets]]
prompt = "CodeAssistant"
format = "claude-code"
dest = "CLAUDE.md"

[[targets]]
prompt = "SecurityReviewer"
format = "copilot"
dest = ".github/copilot-instructions.md"
`)
	writeFile(t, filepath.Join(dir, "prompts", "base.prompt.loom"), `prompt BaseAssistant {
  objective :=
    Help the user.
  format :=
    - Summary
}`)
	writeFile(t, filepath.Join(dir, "prompts", "code.prompt.loom"), `prompt CodeAssistant inherits BaseAssistant {
  constraints :=
    - Keep code maintainable.
}`)
	writeFile(t, filepath.Join(dir, "prompts", "security.prompt.loom"), `prompt SecurityReviewer inherits CodeAssistant {
  constraints :=
    from(parent[0]) and {
      - Check for hardcoded secrets.
    }
}`)

	out, err := RunDeploy(DeployOptions{}, dir)
	if err != nil {
		t.Fatalf("RunDeploy: %v", err)
	}
	if !strings.Contains(out, "wrote") {
		t.Fatalf("expected deploy output to mention writes, got:\n%s", out)
	}

	claudeBody := mustReadFile(t, filepath.Join(dir, "CLAUDE.md"))
	if !strings.Contains(claudeBody, "# CodeAssistant") {
		t.Fatalf("CLAUDE.md missing rendered prompt:\n%s", claudeBody)
	}

	copilotBody := mustReadFile(t, filepath.Join(dir, ".github", "copilot-instructions.md"))
	if !strings.Contains(copilotBody, "# SecurityReviewer") {
		t.Fatalf("copilot instructions missing rendered prompt:\n%s", copilotBody)
	}
}

func TestRunDeployDryRunAndDiff(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loom.toml"), `[project]
name = "deploy-test"
version = "0.1.0"

[paths]
prompts = "prompts"
blocks = "blocks"
out = "dist/prompts"

[render]
default_format = "markdown"
include_metadata = true

[[targets]]
prompt = "CodeAssistant"
format = "copilot"
dest = ".github/copilot-instructions.md"
`)
	writeFile(t, filepath.Join(dir, "prompts", "base.prompt.loom"), `prompt BaseAssistant {
  objective :=
    Help the user.
  format :=
    - Summary
}`)
	writeFile(t, filepath.Join(dir, "prompts", "code.prompt.loom"), `prompt CodeAssistant inherits BaseAssistant {
  constraints :=
    - Keep code maintainable.
}`)
	writeFile(t, filepath.Join(dir, ".github", "copilot-instructions.md"), "# Old\n")

	out, err := RunDeploy(DeployOptions{DryRun: true, Diff: true, TargetFormat: "copilot"}, dir)
	if err != nil {
		t.Fatalf("RunDeploy dry-run: %v", err)
	}
	if !strings.Contains(out, "would write") {
		t.Fatalf("expected dry-run output, got:\n%s", out)
	}
	if !strings.Contains(out, "+++ new") || !strings.Contains(out, "--- current") {
		t.Fatalf("expected diff output, got:\n%s", out)
	}

	current := mustReadFile(t, filepath.Join(dir, ".github", "copilot-instructions.md"))
	if current != "# Old\n" {
		t.Fatalf("dry-run should not modify target, got:\n%s", current)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

const syncToml = `[project]
name = "sync-test"
version = "0.1.0"

[paths]
prompts = "prompts"
blocks = "blocks"
out = "dist"

[validation]
require_objective = false
require_format = false

[[targets]]
prompt = "Assistant"
format = "claude-code"
dest = "CLAUDE.md"

[[targets]]
prompt = "Assistant"
format = "markdown"
dest = "AGENTS.md"
`

func syncProject(t *testing.T, promptBody string) string {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loom.toml"), syncToml)
	writeFile(t, filepath.Join(dir, "prompts", "a.prompt.loom"), promptBody)
	return dir
}

const okPrompt = "prompt Assistant {\n  persona :=\n    You help.\n}\n"

// --check is the drift detector for CLAUDE.md / AGENTS.md / .cursor rules etc.
func TestDeployCheckDetectsDrift(t *testing.T) {
	dir := syncProject(t, okPrompt)

	// nothing deployed yet: both targets are missing
	out, err := RunDeploy(DeployOptions{Check: true}, dir)
	if !errors.Is(err, ErrTargetsOutOfSync) || strings.Count(out, "missing") != 2 {
		t.Fatalf("before any deploy: %v\n%s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "CLAUDE.md")); statErr == nil {
		t.Fatal("--check must never write")
	}

	if _, err := RunDeploy(DeployOptions{}, dir); err != nil {
		t.Fatal(err)
	}
	out, err = RunDeploy(DeployOptions{Check: true}, dir)
	if err != nil || !strings.Contains(out, "All 2 targets are in sync") {
		t.Fatalf("after deploy: %v\n%s", err, out)
	}

	// someone hand-edits one target: exactly that one is reported, and it is not overwritten
	claude := filepath.Join(dir, "CLAUDE.md")
	os.WriteFile(claude, []byte("hand edited\n"), 0o644)
	out, err = RunDeploy(DeployOptions{Check: true, Diff: true}, dir)
	if !errors.Is(err, ErrTargetsOutOfSync) || !strings.Contains(out, "out of sync") || strings.Count(out, "✗") != 1 || !strings.Contains(out, "CLAUDE.md") {
		t.Fatalf("after a hand edit: %v\n%s", err, out)
	}
	if got := mustReadFile(t, claude); got != "hand edited\n" {
		t.Errorf("--check overwrote the file: %q", got)
	}

	// changing the PROMPT (not the file) is drift too
	if _, err := RunDeploy(DeployOptions{}, dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "prompts", "a.prompt.loom"), "prompt Assistant {\n  persona :=\n    You help, briefly.\n}\n")
	if _, err := RunDeploy(DeployOptions{Check: true}, dir); !errors.Is(err, ErrTargetsOutOfSync) {
		t.Errorf("an edited prompt must make the targets stale: %v", err)
	}

	// --target limits the check
	if out, err := RunDeploy(DeployOptions{Check: true, TargetFormat: "markdown"}, dir); !errors.Is(err, ErrTargetsOutOfSync) || strings.Contains(out, "CLAUDE.md") {
		t.Errorf("format filter: %v\n%s", err, out)
	}
}

// A target that cannot be rendered used to print a ✗ and still exit 0.
func TestDeployReportsTargetsThatFailToRender(t *testing.T) {
	dir := syncProject(t, "prompt Assistant {\n  slot repo { required: true }\n  persona :=\n    About {{repo}}.\n}\n")
	for _, opts := range []DeployOptions{{}, {Check: true}, {DryRun: true}} {
		out, err := RunDeploy(opts, dir)
		var failed *DeployFailedError
		if !errors.As(err, &failed) || failed.Failed != 2 || failed.Total != 2 {
			t.Errorf("%+v: want a DeployFailedError for 2 of 2, got %v", opts, err)
		}
		if !strings.Contains(out, "unresolved variables: repo") {
			t.Errorf("%+v: the output must say why:\n%s", opts, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err == nil {
		t.Error("nothing may be written for a target that failed")
	}
}

func TestCIGatesOnDeployTargets(t *testing.T) {
	dir := syncProject(t, okPrompt)
	out, _, _ := RunCI(dir)
	if !strings.Contains(out, "deploy") || !strings.Contains(out, "out of sync") {
		t.Errorf("CI must include the deploy gate when targets are configured:\n%s", out)
	}
	if _, err := RunDeploy(DeployOptions{}, dir); err != nil {
		t.Fatal(err)
	}
	out, _, _ = RunCI(dir)
	if !strings.Contains(out, "all targets are in sync") {
		t.Errorf("%s", out)
	}
	// a project without targets has no such gate
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, "loom.toml"), strings.SplitN(syncToml, "[[targets]]", 2)[0])
	writeFile(t, filepath.Join(plain, "prompts", "a.prompt.loom"), okPrompt)
	if out, _, _ := RunCI(plain); strings.Contains(out, "targets are in sync") || strings.Contains(out, "deploy ") {
		t.Errorf("no targets, no gate:\n%s", out)
	}
}
