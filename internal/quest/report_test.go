package quest

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var errBoom = errors.New("boom")

func TestOutcomeTextReportsEachStepAndAnOverallSummary(t *testing.T) {
	q := &Quest{Name: "Chain", Description: "does a thing", Steps: []Step{{Name: "one"}, {Name: "two"}}}
	out := &Outcome{Quest: q, Steps: []StepResult{
		{Step: Step{Name: "one"}, Model: "gemini:gemini-2.5-flash", Output: "first answer\n"},
		{Step: Step{Name: "two"}, Model: "gemini:gemini-2.5-flash", Output: "second answer"},
	}}
	text := out.Text()
	for _, want := range []string{"does a thing", "step 1: one", "first answer", "step 2: two", "second answer", "2/2 step(s) completed"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestOutcomeTextReportsAnErrorAndAStop(t *testing.T) {
	q := &Quest{Steps: []Step{{Name: "one"}, {Name: "two"}}}
	out := &Outcome{Quest: q, Stopped: true, Steps: []StepResult{
		{Step: Step{Name: "one"}, Err: errBoom},
	}}
	text := out.Text()
	if !strings.Contains(text, "✗") || !strings.Contains(text, "boom") || !strings.Contains(text, "stopped after 1/2") {
		t.Errorf("%s", text)
	}
}

func TestTranscriptWritesEveryStep(t *testing.T) {
	dir := t.TempDir()
	q := &Quest{Name: "Chain"}
	out := &Outcome{Quest: q, Steps: []StepResult{
		{Step: Step{Name: "one", Prompt: "Summarizer"}, Model: "gemini:gemini-2.5-flash", Input: "raw", Output: "a summary"},
	}}
	path := filepath.Join(dir, "sub", "transcript.md")
	if err := out.Transcript(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# loom quest: Chain", "Step 1: one", "Summarizer", "raw", "a summary"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("lacks %q:\n%s", want, data)
		}
	}
}
