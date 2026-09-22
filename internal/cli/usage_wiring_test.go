package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

// These tests exercise the DEFAULT NewModel/ClientFor of each command (never overridden), against
// a fake Gemini server, so the usage.Attach wiring in run.go/quest/run.go/eval/project.go/
// optimize.go actually runs — the fakes used elsewhere in these packages' own tests bypass it.

func TestRunCommandRecordsUsage(t *testing.T) {
	dir := runProject(t, "")
	t.Setenv("GEMINI_API_KEY", "k")
	resetUsageFlags()
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})

	p := runParams{
		Name: "Reviewer", Cwd: dir, Input: "review this", NoStream: true,
		Weave: tui.WeaveOptions{Variables: map[string]string{"repo": "demo"}},
		Stdin: strings.NewReader(""), Stdout: new(strings.Builder), Stderr: new(strings.Builder),
		TurnContext: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
	}
	if err := executeRun(p); err != nil {
		t.Fatal(err)
	}
	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) != 1 || got[0].Command != "run" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestQuestRunCommandRecordsUsage(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetQuestFlags()
	startQuestServer(t, &questGate{})

	questInput = "raw"
	if _, err := captureStdout(t, func() error { return runQuestRun(questRunCmd, []string{"Chain"}) }); err != nil {
		t.Fatal(err)
	}
	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	for _, r := range got {
		if r.Command != "quest run" {
			t.Errorf("%+v", r)
		}
	}
}

func TestEvalCommandRecordsUsageForModelAndJudge(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})

	if _, err := captureStdout(t, func() error { return runEval(evalCmd, []string{"Reviewer"}) }); err != nil {
		t.Fatal(err)
	}
	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	roles := map[string]bool{}
	for _, r := range got {
		if r.Command != "eval" {
			t.Errorf("%+v", r)
		}
		roles[r.Role] = true
	}
	if !roles[""] || !roles["judge"] {
		t.Errorf("expected a primary and a judge record: %+v", got)
	}
}

func TestScoreCommandRecordsUsageUnderScore(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()
	newGateServer(t, &gate{modelReply: "verdict: because reasons"})

	if _, err := captureStdout(t, func() error { return runScore(scoreCmd, []string{"Reviewer"}) }); err != nil {
		t.Fatal(err)
	}
	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %+v", err, got)
	}
	for _, r := range got {
		if r.Command != "score" {
			t.Errorf("%+v", r)
		}
	}
}

func TestOptimizeCommandRecordsUsageIncludingRefiner(t *testing.T) {
	dir := scoreProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetOptimizeFlags()
	newGateServer(t, &gate{modelReply: "verdict: nope", refined: refinedReviewer})

	// no --yes: a preview that does not reach a passing score, so runOptimize returns an error —
	// expected (see TestOptimizeCommandPreviewAndApply in score_test.go); the refiner still ran.
	captureStdout(t, func() error { return runOptimize(optimizeCmd, []string{"Reviewer"}) })
	got, err := usage.ReadAll(usage.DefaultPath(dir))
	if err != nil || len(got) == 0 {
		t.Fatalf("%v %+v", err, got)
	}
	sawRefiner := false
	for _, r := range got {
		if r.Command != "optimize" {
			t.Errorf("%+v", r)
		}
		if r.Role == "refiner" {
			sawRefiner = true
		}
	}
	if !sawRefiner {
		t.Errorf("expected a refiner record: %+v", got)
	}
}
