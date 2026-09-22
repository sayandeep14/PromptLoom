package lmscr

import (
	"strings"
	"testing"
	"time"
)

func TestOutcomeTextReportsEachStepAndASummary(t *testing.T) {
	s := &Script{Description: "check, then deploy"}
	out := &Outcome{Script: s, Steps: []StepResult{
		{Step: Step{Name: "check"}, ExitCode: 0, Duration: 5 * time.Millisecond},
		{Step: Step{Name: "deploy"}, ExitCode: 1, Duration: 5 * time.Millisecond},
		{Step: Step{Name: "cleanup", When: "always"}, Skipped: true},
	}}
	text := out.Text()
	for _, want := range []string{"check, then deploy", "✓ check", "✗ deploy: exit 1", "○ cleanup (skipped: always)", "2 step(s) run, 1 skipped, 1 failed"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestOutcomeTextMarksAContinueOnFailStepDifferently(t *testing.T) {
	out := &Outcome{Script: &Script{}, Steps: []StepResult{
		{Step: Step{Name: "a", ContinueOnFail: true}, ExitCode: 1},
	}}
	text := out.Text()
	if !strings.Contains(text, "! a: exit 1") {
		t.Errorf("%s", text)
	}
	if strings.Contains(text, "1 failed") {
		t.Errorf("a continue_on_fail step must not count as failed:\n%s", text)
	}
}
