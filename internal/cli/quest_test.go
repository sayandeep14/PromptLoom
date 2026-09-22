package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/llm"
)

const questCliToml = `[project]
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

const questCliPrompts = `prompt Summarizer {
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

const chainQuest = `
description = "summarize, then plan"

[[step]]
name = "summarize"
prompt = "Summarizer"
input = "{{quest.input}}"

[[step]]
name = "plan"
prompt = "Planner"
input = "make a step from: {{quest.previous}}"
`

func questCliProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(questCliToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "p.prompt.loom"), []byte(questCliPrompts), 0o644)
	os.MkdirAll(filepath.Join(dir, "quests"), 0o755)
	os.WriteFile(filepath.Join(dir, "quests", "Chain.quest.toml"), []byte(chainQuest), 0o644)
	return dir
}

func resetQuestFlags() {
	questInput, questInputFile, questModel, questOut = "", "", "", ""
	questJSON, questDryRun, questContinueOnError, questNoStream = false, false, false, false
	questDir = ""
}

// questGate replies "a summary" to the first call and "a plan with a step" to the second,
// regardless of which system prompt was sent (there is no judge here, unlike score_test.go's gate).
type questGate struct{ n int }

func (g *questGate) reply() string {
	g.n++
	if g.n == 1 {
		return "a summary"
	}
	return "a plan with a step"
}

// geminiSSEFor wraps text as a one-event Gemini SSE stream, the format llm.Client.Stream expects.
func geminiSSEFor(text string) string {
	b, _ := json.Marshal(map[string]any{"candidates": []any{map[string]any{
		"content": map[string]any{"parts": []any{map[string]any{"text": text}}}}}})
	return "data: " + string(b) + "\n\n"
}

// startQuestServer points llm.GeminiBaseURL at a fake streaming endpoint driven by g.
func startQuestServer(t *testing.T, g *questGate) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, geminiSSEFor(g.reply()))
	}))
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	t.Cleanup(func() { srv.Close(); llm.GeminiBaseURL = old })
}

// newFailingServer always answers with text lacking the word "step", to trip Planner's contract.
func newFailingServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, geminiSSEFor("no such word here"))
	}))
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL + "/v1beta"
	t.Cleanup(func() { srv.Close(); llm.GeminiBaseURL = old })
}

func TestQuestRunCommand(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetQuestFlags()

	startQuestServer(t, &questGate{})

	out, err := captureStdout(t, func() error { return runQuestRun(questRunCmd, []string{"Chain"}) })
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	for _, want := range []string{"step 1: summarize", "a summary", "step 2: plan", "a plan with a step"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
}

func TestQuestRunJSONAndOut(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetQuestFlags()

	g := &questGate{}
	startQuestServer(t, g)

	questJSON = true
	questOut = "transcript.md"
	questInput = "raw"
	out, err := captureStdout(t, func() error { return runQuestRun(questRunCmd, []string{"Chain"}) })
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, `"quest": "Chain"`) || !strings.Contains(out, `"ok": true`) {
		t.Errorf("json output: %s", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "transcript.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "a summary") || !strings.Contains(string(data), "a plan with a step") {
		t.Errorf("transcript: %s", data)
	}
}

func TestQuestRunDryRunCallsNothing(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	resetQuestFlags()
	questDryRun = true
	questInput = "the input"
	out, err := captureStdout(t, func() error { return runQuestRun(questRunCmd, []string{"Chain"}) })
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if !strings.Contains(out, "quest: Chain (2 step(s))") || !strings.Contains(out, "nothing was sent") || !strings.Contains(out, "the input") {
		t.Errorf("%s", out)
	}
}

func TestQuestRunStopsOnContractFailure(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	t.Setenv("GEMINI_API_KEY", "k")
	resetQuestFlags()
	os.WriteFile(filepath.Join(dir, "quests", "Strict.quest.toml"), []byte(`
[[step]]
name = "plan"
prompt = "Planner"
input = "{{quest.input}}"

[[step]]
name = "after"
prompt = "Summarizer"
input = "{{quest.previous}}"
`), 0o644)

	newFailingServer(t) // replies without the required word "step"
	out, err := captureStdout(t, func() error { return runQuestRun(questRunCmd, []string{"Strict"}) })
	if err == nil {
		t.Fatalf("expected an error, out:\n%s", out)
	}
	if !strings.Contains(out, "contract") {
		t.Errorf("%s", out)
	}
}

func TestQuestListCommand(t *testing.T) {
	dir := questCliProject(t)
	t.Chdir(dir)
	resetQuestFlags()
	out, err := captureStdout(t, func() error { return runQuestList(questListCmd, nil) })
	if err != nil || !strings.Contains(out, "Chain") || !strings.Contains(out, "2 step(s)") || !strings.Contains(out, "summarize, then plan") {
		t.Fatalf("%v\n%s", err, out)
	}
}
