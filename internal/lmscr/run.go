package lmscr

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// StepResult is what happened when one step ran (or why it was skipped).
type StepResult struct {
	Step     Step
	Args     []string // resolved (after {{vars.NAME}} substitution)
	Skipped  bool     // its "when" condition did not match
	ExitCode int
	Err      error // a problem starting the command itself (not a non-zero exit)
	Duration time.Duration
}

// Failed reports whether this step's failure counts against the script (it ran, did not skip, and
// exited non-zero or could not start, and continue_on_fail was not set).
func (r StepResult) Failed() bool {
	return !r.Skipped && (r.ExitCode != 0 || r.Err != nil) && !r.Step.ContinueOnFail
}

// Outcome is a full run of a script.
type Outcome struct {
	Script *Script
	Steps  []StepResult
}

// OK reports whether every step that counted succeeded.
func (o *Outcome) OK() bool {
	for _, s := range o.Steps {
		if s.Failed() {
			return false
		}
	}
	return true
}

// Runner starts one `loom <argv>` and waits for it, streaming its own stdout/stderr through
// (inherited, not captured, so a step behaves exactly as if it had been typed by hand — including
// a streamed model reply or an interactive prompt).
type Runner func(ctx context.Context, cwd, binary string, argv []string, stdout, stderr io.Writer) (exitCode int, err error)

// DefaultRunner execs the loom binary itself, never a shell — a step's Run is always one of
// loom's own (sub)commands, its Args passed as argv, so there is no shell injection surface.
func DefaultRunner(ctx context.Context, cwd, binary string, argv []string, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, binary, argv...)
	cmd.Dir = cwd
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if ok := asExitError(err, &exitErr); ok {
		return exitErr.ExitCode(), nil
	}
	return -1, err // the command could not even be started
}

func asExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

// Params controls a run.
type Params struct {
	Vars   map[string]string // merged over the script's own vars; --set at the CLI
	Binary string            // the loom binary to invoke; default: os.Executable()
	Stdout io.Writer         // default os.Stdout
	Stderr io.Writer         // default os.Stderr
	Runner Runner            // default DefaultRunner
}

// Run executes every step of s in order. It stops nothing on a missing variable — that is checked
// up front, before any step runs, exactly like a missing slot value on `loom run`.
func Run(ctx context.Context, cwd string, s *Script, p Params) (*Outcome, error) {
	vars := map[string]string{}
	maps.Copy(vars, s.Vars)
	maps.Copy(vars, p.Vars)
	if missing := missingVars(s, vars); len(missing) > 0 {
		return nil, fmt.Errorf("missing value(s) for %s (pass --set NAME=value)", strings.Join(missing, ", "))
	}

	if p.Binary == "" {
		bin, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("finding the loom binary: %w", err)
		}
		p.Binary = bin
	}
	if p.Stdout == nil {
		p.Stdout = os.Stdout
	}
	if p.Stderr == nil {
		p.Stderr = os.Stderr
	}
	if p.Runner == nil {
		p.Runner = DefaultRunner
	}

	out := &Outcome{Script: s}
	lastFailed := false
	for _, step := range s.Steps {
		res := StepResult{Step: step}
		if !shouldRun(step.Effective(), lastFailed) {
			res.Skipped = true
			out.Steps = append(out.Steps, res)
			continue
		}

		args := make([]string, len(step.Args))
		for i, a := range step.Args {
			args[i] = Substitute(a, vars)
		}
		res.Args = args
		argv := append(strings.Fields(step.Run), args...)

		start := time.Now()
		code, err := p.Runner(ctx, cwd, p.Binary, argv, p.Stdout, p.Stderr)
		res.Duration = time.Since(start)
		res.ExitCode, res.Err = code, err
		out.Steps = append(out.Steps, res)

		lastFailed = (code != 0 || err != nil) && !step.ContinueOnFail
	}
	return out, nil
}

func shouldRun(when string, lastFailed bool) bool {
	switch when {
	case "on_failure":
		return lastFailed
	case "always":
		return true
	default: // on_success
		return !lastFailed
	}
}

// missingVars is referencedVars minus whatever vars already supplies, sorted.
func missingVars(s *Script, vars map[string]string) []string {
	var out []string
	for _, name := range s.referencedVars() {
		if _, ok := vars[name]; !ok {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
