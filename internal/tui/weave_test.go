package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const weaveToml = `[project]
name = "t"
version = "0.0.0"

[paths]
prompts  = "prompts"
blocks   = "blocks"
overlays = "overlays"
out      = "dist"

[validation]
require_objective        = false
require_format           = false
`

// A library whose base prompt has a required slot: the base and its child both need a value.
func slotProject(t *testing.T) string {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "loom.toml"), weaveToml)
	writeFile(t, filepath.Join(dir, "prompts", "Base.prompt.loom"),
		"prompt Base {\n  slot repo { required: true }\n\n  persona :=\n    Works on {{repo}}.\n}\n")
	writeFile(t, filepath.Join(dir, "prompts", "Child.prompt.loom"),
		"prompt Child inherits Base {\n  objective :=\n    Help.\n}\n")
	writeFile(t, filepath.Join(dir, "prompts", "Plain.prompt.loom"),
		"prompt Plain {\n  persona :=\n    Plain.\n}\n")
	return dir
}

// `weave --all` used to say "3 prompts rendered" and exit 0 while skipping two of them.
func TestWeaveAllReportsFailuresHonestly(t *testing.T) {
	dir := slotProject(t)
	out, err := RunWeave("", true, WeaveOptions{}, dir)

	var failed *WeaveFailedError
	if !errors.As(err, &failed) || failed.Failed != 2 || failed.Total != 3 {
		t.Fatalf("want a WeaveFailedError for 2 of 3, got %v", err)
	}
	if !strings.Contains(err.Error(), "2 of 3 prompts failed") {
		t.Errorf("%v", err)
	}
	// the output still says which ones failed and which one worked
	for _, want := range []string{"Base", "Child", "repo", "1 of 3 prompts"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "3 of 3") {
		t.Errorf("must not claim every prompt rendered:\n%s", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dist", "Plain.md")); statErr != nil {
		t.Error("the prompt that could render is still written")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "dist", "Child.md")); statErr == nil {
		t.Error("a prompt with unresolved variables must not be written")
	}
}

func TestWeaveAllSucceedsWhenValuesAreSupplied(t *testing.T) {
	dir := slotProject(t)
	out, err := RunWeave("", true, WeaveOptions{Variables: map[string]string{"repo": "demo"}}, dir)
	if err != nil || !strings.Contains(out, "3 of 3 prompts") {
		t.Fatalf("%v\n%s", err, out)
	}
	if body := mustReadFile(t, filepath.Join(dir, "dist", "Child.md")); !strings.Contains(body, "Works on demo.") {
		t.Errorf("%s", body)
	}
}

// After weaving with values, `loom ci`'s diff gate compared the dist files (real values) with
// a render that had {{repo}} unfilled, and reported them stale forever.
func TestDiffAgainstDistSkipsPromptsThatNeedValues(t *testing.T) {
	dir := slotProject(t)
	if _, err := RunWeave("", true, WeaveOptions{Variables: map[string]string{"repo": "demo"}}, dir); err != nil {
		t.Fatal(err)
	}
	out, changed, err := RunDiff("", "", DiffOptions{All: true, AgainstDist: true, ExitCode: true}, dir)
	if err != nil || changed {
		t.Fatalf("changed=%v err=%v\n%s", changed, err, out)
	}
	if !strings.Contains(out, "skipped") || !strings.Contains(out, "repo") {
		t.Errorf("the skip should be visible:\n%s", out)
	}

	// a real change to a prompt that needs no values is still detected
	writeFile(t, filepath.Join(dir, "prompts", "Plain.prompt.loom"), "prompt Plain {\n  persona :=\n    Changed.\n}\n")
	if _, changed, _ = RunDiff("", "", DiffOptions{All: true, AgainstDist: true, ExitCode: true}, dir); !changed {
		t.Error("an edited prompt must still be reported stale")
	}
}
