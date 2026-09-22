package quest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const questToml = `[project]
name = "t"
version = "0"
[paths]
prompts = "prompts"
blocks = "blocks"
overlays = "overlays"
out = "dist"
[validation]
require_objective = false
require_format = false
`

const questPrompts = `prompt Summarizer {
  persona :=
    You summarize things.
}

prompt Planner {
  persona :=
    You make plans.
  contract {
    must_include:
      - "step"
  }
}
`

func questProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(questToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "p.prompt.loom"), []byte(questPrompts), 0o644)
	return dir
}

func writeQuest(t *testing.T, dir, name, body string) *Quest {
	t.Helper()
	p := write(t, dir, filepath.Join("quests", name+SuiteSuffix), body)
	q, err := LoadQuest(p)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// queuedModel answers with the next reply in the queue, tagged with which step's system prompt
// produced it (so a test can tell steps apart) and what user message it received.
type queuedModel struct {
	replies []string
	seen    []llm.Request
}

func (m *queuedModel) Stream(_ context.Context, r llm.Request, onDelta func(string)) (string, llm.Usage, error) {
	m.seen = append(m.seen, r)
	text := m.replies[0]
	m.replies = m.replies[1:]
	if onDelta != nil {
		onDelta(text)
	}
	return text, llm.Usage{InputTokens: 5, OutputTokens: 1}, nil
}

func (m *queuedModel) Complete(ctx context.Context, r llm.Request) (string, error) {
	text, _, err := m.Stream(ctx, r, nil)
	return text, err
}

func newModelFn(m *queuedModel) func(*config.Config, string, string) (agent.Model, error) {
	return func(*config.Config, string, string) (agent.Model, error) { return m, nil }
}

func TestRunChainsStepsWithQuestInputAndPrevious(t *testing.T) {
	dir := questProject(t)
	q := writeQuest(t, dir, "Chain", `
[[step]]
name = "summarize"
prompt = "Summarizer"
input = "summarize: {{quest.input}}"

[[step]]
name = "plan"
prompt = "Planner"
input = "plan a step from: {{quest.previous}}"
`)
	m := &queuedModel{replies: []string{"a summary", "a plan with a step"}}
	perm, err := agent.LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Run(context.Background(), dir, q, Params{
		Input: "raw text", NewModel: newModelFn(m),
	}, perm)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK() || len(out.Steps) != 2 {
		t.Fatalf("%+v", out)
	}
	if out.Steps[0].Input != "summarize: raw text" {
		t.Errorf("step 1 input = %q", out.Steps[0].Input)
	}
	if out.Steps[1].Input != "plan a step from: a summary" {
		t.Errorf("step 2 input = %q", out.Steps[1].Input)
	}
	if out.Steps[0].Output != "a summary" || out.Steps[1].Output != "a plan with a step" {
		t.Errorf("%+v", out.Steps)
	}
	if len(m.seen) != 2 || !strings.Contains(m.seen[0].System, "You summarize things.") || !strings.Contains(m.seen[1].System, "You make plans.") {
		t.Errorf("%+v", m.seen)
	}
}

func TestRunStopsOnContractFailureUnlessToldToContinue(t *testing.T) {
	dir := questProject(t)
	q := writeQuest(t, dir, "Strict", `
[[step]]
name = "plan"
prompt = "Planner"
input = "{{quest.input}}"

[[step]]
name = "after"
prompt = "Summarizer"
input = "given {{quest.previous}}, summarize"
`)
	perm, err := agent.LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Planner's contract requires "step"; this reply lacks it, so the quest should stop.
	m := &queuedModel{replies: []string{"no such word here"}}
	out, err := Run(context.Background(), dir, q, Params{Input: "x", NewModel: newModelFn(m)}, perm)
	if err != nil {
		t.Fatal(err)
	}
	if out.OK() || !out.Stopped || len(out.Steps) != 1 {
		t.Fatalf("%+v", out)
	}
	if len(out.Steps[0].ContractFailures) == 0 {
		t.Errorf("expected a contract failure")
	}

	// With ContinueOnError, both steps run despite the failure.
	m = &queuedModel{replies: []string{"no such word here", "a summary"}}
	out, err = Run(context.Background(), dir, q, Params{Input: "x", NewModel: newModelFn(m), ContinueOnError: true}, perm)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 2 || out.OK() {
		t.Fatalf("%+v", out)
	}
}

func TestRunStepContinueOnFailOverridesForThatStepAlone(t *testing.T) {
	dir := questProject(t)
	q := writeQuest(t, dir, "PartlyLenient", `
[[step]]
name = "plan"
prompt = "Planner"
input = "{{quest.input}}"
continue_on_fail = true

[[step]]
name = "after"
prompt = "Summarizer"
input = "given {{quest.previous}}, summarize"
`)
	perm, err := agent.LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &queuedModel{replies: []string{"no such word here", "a summary"}}
	out, err := Run(context.Background(), dir, q, Params{Input: "x", NewModel: newModelFn(m)}, perm)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Steps) != 2 {
		t.Fatalf("expected both steps to run: %+v", out)
	}
}

func TestRunChecksPermissionOnAStepsWithSources(t *testing.T) {
	dir := questProject(t)
	os.WriteFile(filepath.Join(dir, ".loom.config"), []byte(`{"permission":{"read":[],"write":["*"]}}`), 0o644)
	q := writeQuest(t, dir, "Reads", `
[[step]]
name = "plan"
prompt = "Planner"
input = "{{quest.input}}"
with = ["file:secret.txt"]
`)
	perm, err := agent.LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &queuedModel{replies: []string{"has a step"}}
	out, err := Run(context.Background(), dir, q, Params{Input: "x", NewModel: newModelFn(m)}, perm)
	if err == nil || !strings.Contains(err.Error(), "permission.read") {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestRunReturnsTheModelErrorAndStops(t *testing.T) {
	dir := questProject(t)
	q := writeQuest(t, dir, "Erroring", `
[[step]]
name = "plan"
prompt = "Planner"
input = "{{quest.input}}"

[[step]]
name = "after"
prompt = "Summarizer"
input = "{{quest.previous}}"
`)
	perm, err := agent.LoadPermission(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := &erroringModel{}
	out, err := Run(context.Background(), dir, q, Params{Input: "x", NewModel: newModelFn2(m)}, perm)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Stopped || len(out.Steps) != 1 || out.Steps[0].Err == nil {
		t.Fatalf("%+v", out)
	}
}

type erroringModel struct{}

func (erroringModel) Stream(context.Context, llm.Request, func(string)) (string, llm.Usage, error) {
	return "", llm.Usage{}, context.DeadlineExceeded
}
func (erroringModel) Complete(context.Context, llm.Request) (string, error) {
	return "", context.DeadlineExceeded
}

func newModelFn2(m agent.Model) func(*config.Config, string, string) (agent.Model, error) {
	return func(*config.Config, string, string) (agent.Model, error) { return m, nil }
}
