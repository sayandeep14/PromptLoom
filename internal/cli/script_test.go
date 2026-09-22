package cli

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func scriptProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "scripts", "Release.lmscr"), `
description = "check, then deploy"

[[step]]
name = "check"
run = "eval"
args = ["--compare"]

[[step]]
name = "deploy"
run = "deploy"
args = ["--target", "{{vars.env}}"]
`)
	return dir
}

// scriptedRunner records every argv it was asked to run and replies with the next exit code.
type scriptedRunner struct {
	codes []int
	seen  [][]string
}

func (r *scriptedRunner) run(_ context.Context, cwd, binary string, argv []string, stdout, stderr io.Writer) (int, error) {
	r.seen = append(r.seen, append([]string{}, argv...))
	i := len(r.seen) - 1
	code := 0
	if i < len(r.codes) {
		code = r.codes[i]
	}
	return code, nil
}

func TestScriptRunCommand(t *testing.T) {
	dir := scriptProject(t)
	var out bytes.Buffer
	r := &scriptedRunner{}
	err := executeScriptRun(scriptRunParams{
		Name: "Release", Cwd: dir, Vars: map[string]string{"env": "staging"}, Stdout: &out,
		Runner: r.run,
	})
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	if len(r.seen) != 2 || strings.Join(r.seen[0], " ") != "eval --compare" || strings.Join(r.seen[1], " ") != "deploy --target staging" {
		t.Fatalf("%v", r.seen)
	}
	if !strings.Contains(out.String(), "✓ check") || !strings.Contains(out.String(), "✓ deploy") {
		t.Errorf("%s", out.String())
	}
}

func TestScriptRunMissingVariableFailsUpFront(t *testing.T) {
	dir := scriptProject(t)
	var out bytes.Buffer
	r := &scriptedRunner{}
	err := executeScriptRun(scriptRunParams{Name: "Release", Cwd: dir, Stdout: &out, Runner: r.run})
	if err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("err = %v", err)
	}
	if len(r.seen) != 0 {
		t.Error("nothing should have run")
	}
}

func TestScriptRunDryRunCallsNothing(t *testing.T) {
	dir := scriptProject(t)
	var out bytes.Buffer
	r := &scriptedRunner{}
	err := executeScriptRun(scriptRunParams{
		Name: "Release", Cwd: dir, Vars: map[string]string{"env": "staging"}, Stdout: &out,
		DryRun: true, Runner: r.run,
	})
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out.String())
	}
	if len(r.seen) != 0 {
		t.Error("dry-run must call nothing")
	}
	got := out.String()
	for _, want := range []string{"script: Release (2 step(s))", "check, then deploy", "loom eval --compare", "loom deploy --target staging", "nothing was sent"} {
		if !strings.Contains(got, want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
}

func TestScriptRunReportsFailureExitCode(t *testing.T) {
	dir := scriptProject(t)
	var out bytes.Buffer
	r := &scriptedRunner{codes: []int{1}}
	err := executeScriptRun(scriptRunParams{
		Name: "Release", Cwd: dir, Vars: map[string]string{"env": "staging"}, Stdout: &out,
		Runner: r.run,
	})
	if err == nil {
		t.Fatalf("expected an error, out:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "✗ check: exit 1") {
		t.Errorf("%s", out.String())
	}
	if len(r.seen) != 1 {
		t.Errorf("deploy must be skipped after check fails: %v", r.seen)
	}
}

func TestScriptListCommand(t *testing.T) {
	dir := scriptProject(t)
	t.Chdir(dir)
	resetOptimizeFlags()
	resetQuestFlags()
	scriptDir = ""
	out, err := captureStdout(t, func() error { return runScriptList(scriptListCmd, nil) })
	if err != nil || !strings.Contains(out, "Release") || !strings.Contains(out, "2 step(s)") || !strings.Contains(out, "check, then deploy") {
		t.Fatalf("%v\n%s", err, out)
	}
}
