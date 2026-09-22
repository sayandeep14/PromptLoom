package lmscr

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// scriptedRunner records every argv it was asked to run and replies with the next exit code (and
// error, if any) from its queue.
type scriptedRunner struct {
	codes []int
	errs  []error
	seen  [][]string
}

func (r *scriptedRunner) run(_ context.Context, cwd, binary string, argv []string, stdout, stderr io.Writer) (int, error) {
	r.seen = append(r.seen, append([]string{}, argv...))
	i := len(r.seen) - 1
	fmt.Fprintf(stdout, "ran: %s\n", strings.Join(argv, " "))
	var err error
	if i < len(r.errs) {
		err = r.errs[i]
	}
	code := 0
	if i < len(r.codes) {
		code = r.codes[i]
	}
	return code, err
}

func scriptFrom(t *testing.T, body string) *Script {
	t.Helper()
	dir := t.TempDir()
	p := write(t, dir, "S.lmscr", body)
	s, err := LoadScript(p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRunExecutesEveryStepInOrderWithSubstitution(t *testing.T) {
	s := scriptFrom(t, `
vars = { env = "staging" }

[[step]]
name = "check"
run = "eval"
args = ["--compare"]

[[step]]
name = "deploy"
run = "deploy"
args = ["--target", "{{vars.env}}"]
`)
	r := &scriptedRunner{}
	var out bytes.Buffer
	oc, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: &out, Runner: r.run})
	if err != nil {
		t.Fatal(err)
	}
	if !oc.OK() || len(oc.Steps) != 2 {
		t.Fatalf("%+v", oc)
	}
	if len(r.seen) != 2 || strings.Join(r.seen[0], " ") != "eval --compare" || strings.Join(r.seen[1], " ") != "deploy --target staging" {
		t.Fatalf("%v", r.seen)
	}
	if !strings.Contains(out.String(), "ran: eval --compare") || !strings.Contains(out.String(), "ran: deploy --target staging") {
		t.Errorf("%s", out.String())
	}
}

func TestRunFailsUpFrontOnAMissingVariable(t *testing.T) {
	s := scriptFrom(t, `
[[step]]
name = "deploy"
run = "deploy"
args = ["--target", "{{vars.env}}"]
`)
	r := &scriptedRunner{}
	_, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Runner: r.run})
	if err == nil || !strings.Contains(err.Error(), "env") {
		t.Fatalf("err = %v", err)
	}
	if len(r.seen) != 0 {
		t.Error("nothing should have run")
	}
}

func TestRunSkipsOnSuccessStepsAfterAFailure(t *testing.T) {
	s := scriptFrom(t, `
[[step]]
name = "a"
run = "eval"

[[step]]
name = "b"
run = "deploy"

[[step]]
name = "cleanup"
run = "audit"
when = "always"
`)
	r := &scriptedRunner{codes: []int{1}}
	oc, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: io.Discard, Runner: r.run})
	if err != nil {
		t.Fatal(err)
	}
	if oc.OK() {
		t.Fatal("expected the run to be marked failed")
	}
	if len(r.seen) != 2 || strings.Join(r.seen[1], " ") != "audit" {
		t.Fatalf("%v", r.seen)
	}
	if !oc.Steps[1].Skipped || oc.Steps[2].Skipped {
		t.Fatalf("%+v", oc.Steps)
	}
}

func TestRunOnFailureStepRunsOnlyAfterAFailure(t *testing.T) {
	s := scriptFrom(t, `
[[step]]
name = "a"
run = "eval"

[[step]]
name = "notify"
run = "audit"
when = "on_failure"
`)
	// a passes: on_failure step is skipped
	r := &scriptedRunner{}
	oc, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: io.Discard, Runner: r.run})
	if err != nil || !oc.OK() || !oc.Steps[1].Skipped {
		t.Fatalf("%v %+v", err, oc)
	}

	// a fails: on_failure step runs
	r = &scriptedRunner{codes: []int{1}}
	oc, err = Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: io.Discard, Runner: r.run})
	if err != nil {
		t.Fatal(err)
	}
	if oc.Steps[1].Skipped || len(r.seen) != 2 {
		t.Fatalf("%+v", oc.Steps)
	}
}

func TestRunContinueOnFailDoesNotStopOrCountAsFailed(t *testing.T) {
	s := scriptFrom(t, `
[[step]]
name = "a"
run = "eval"
continue_on_fail = true

[[step]]
name = "b"
run = "deploy"
`)
	r := &scriptedRunner{codes: []int{1, 0}}
	oc, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: io.Discard, Runner: r.run})
	if err != nil {
		t.Fatal(err)
	}
	if !oc.OK() {
		t.Fatalf("continue_on_fail must not fail the run: %+v", oc)
	}
	if len(r.seen) != 2 || oc.Steps[1].Skipped {
		t.Fatalf("the next on_success step must still run: %+v", oc.Steps)
	}
}

func TestRunReportsAStartFailureAsFailed(t *testing.T) {
	s := scriptFrom(t, `
[[step]]
name = "a"
run = "eval"
`)
	r := &scriptedRunner{errs: []error{fmt.Errorf("boom")}}
	oc, err := Run(context.Background(), "/proj", s, Params{Binary: "loom", Stdout: io.Discard, Runner: r.run})
	if err != nil {
		t.Fatal(err)
	}
	if oc.OK() || oc.Steps[0].Err == nil {
		t.Fatalf("%+v", oc)
	}
}
