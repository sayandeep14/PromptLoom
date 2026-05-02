package tui

import (
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
	writeFile(t, filepath.Join(dir, "prompts", "base.prompt"), `prompt BaseAssistant {
  objective:
    Help the user.
  format:
    - Summary
}`)
	writeFile(t, filepath.Join(dir, "prompts", "code.prompt"), `prompt CodeAssistant inherits BaseAssistant {
  constraints:
    - Keep code maintainable.
}`)
	writeFile(t, filepath.Join(dir, "prompts", "security.prompt"), `prompt SecurityReviewer inherits CodeAssistant {
  constraints +=
    - Check for hardcoded secrets.
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
	writeFile(t, filepath.Join(dir, "prompts", "base.prompt"), `prompt BaseAssistant {
  objective:
    Help the user.
  format:
    - Summary
}`)
	writeFile(t, filepath.Join(dir, "prompts", "code.prompt"), `prompt CodeAssistant inherits BaseAssistant {
  constraints:
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
