package bench

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

const benchToml = `[project]
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

const benchPrompt = `prompt Reviewer {
  persona :=
    You are a reviewer.
}
`

func benchProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(benchToml), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "r.prompt.loom"), []byte(benchPrompt), 0o644)
	return dir
}

const benchTomlWithPricing = benchToml + `
[[pricing]]
provider = "gemini"
model = "gemini-2.5-flash"
input_per_million = 1
output_per_million = 2
`

func benchProjectWithPricing(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(benchTomlWithPricing), 0o644)
	os.MkdirAll(filepath.Join(dir, "prompts"), 0o755)
	os.WriteFile(filepath.Join(dir, "prompts", "r.prompt.loom"), []byte(benchPrompt), 0o644)
	return dir
}

// scripted answers with a fixed reply, or an error on one specific call.
type scripted struct {
	calls int
	errAt int // 1-indexed call number that errors; 0 means never
	seen  []llm.Request
}

func (s *scripted) Complete(_ context.Context, r llm.Request) (string, error) {
	s.calls++
	s.seen = append(s.seen, r)
	if s.errAt != 0 && s.calls == s.errAt {
		return "", errors.New("boom")
	}
	return "an answer", nil
}

func TestRunCallsEveryModelTheRequestedNumberOfTimes(t *testing.T) {
	dir := benchProject(t)
	m1, m2 := &scripted{}, &scripted{}
	models := map[string]*scripted{"gemini:a": m1, "openai:b": m2}
	out, err := Run(context.Background(), dir, "Reviewer", Params{
		Input: "review this", Models: []string{"gemini:a", "openai:b"}, Runs: 3,
		NewModel: func(_ *config.Config, provider, model string) (Completer, error) {
			return models[provider+":"+model], nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Models) != 2 {
		t.Fatalf("%+v", out)
	}
	for i, mr := range out.Models {
		if len(mr.Calls) != 3 || mr.Errors() != 0 {
			t.Fatalf("model %d: %+v", i, mr)
		}
	}
	if m1.calls != 3 || m2.calls != 3 {
		t.Fatalf("m1=%d m2=%d", m1.calls, m2.calls)
	}
	if m1.seen[0].System == "" || m1.seen[0].User != "review this" {
		t.Errorf("%+v", m1.seen[0])
	}
}

func TestRunRecordsErrorsWithoutStoppingOtherCalls(t *testing.T) {
	dir := benchProject(t)
	m := &scripted{errAt: 2}
	out, err := Run(context.Background(), dir, "Reviewer", Params{
		Input: "x", Runs: 3,
		NewModel: func(*config.Config, string, string) (Completer, error) { return m, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	mr := out.Models[0]
	if len(mr.Calls) != 3 || mr.Errors() != 1 || mr.Calls[1].Err == nil {
		t.Fatalf("%+v", mr)
	}
}

func TestAverageDurationTokensAndCost(t *testing.T) {
	mr := ModelResult{Calls: []Call{
		{Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}, CostUSD: 0.01, HasCost: true},
		{Usage: llm.Usage{InputTokens: 20, OutputTokens: 4}, CostUSD: 0.02, HasCost: true},
	}}
	if mr.AvgInput() != 15 || mr.AvgOutput() != 3 {
		t.Errorf("avg in/out: %v %v", mr.AvgInput(), mr.AvgOutput())
	}
	total, ok := mr.TotalCost()
	if !ok || total != 0.03 {
		t.Errorf("total cost: %v %v", total, ok)
	}
}

func TestTotalCostIsUnknownIfAnyCallLacksAPrice(t *testing.T) {
	mr := ModelResult{Calls: []Call{
		{CostUSD: 0.01, HasCost: true},
		{HasCost: false},
	}}
	if _, ok := mr.TotalCost(); ok {
		t.Error("a partial total must never be reported as if it were complete")
	}
}

// A real *llm.Client (against a fake Gemini endpoint) exercises the whole path, including the
// usage ledger and cost estimate — a fake Completer in the other tests never reaches that code.
func TestRunWithARealClientRecordsUsageAndCost(t *testing.T) {
	dir := benchProjectWithPricing(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.ReadAll(r.Body)
		io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"an answer"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2}}`)
	}))
	defer srv.Close()
	old := llm.GeminiBaseURL
	llm.GeminiBaseURL = srv.URL
	defer func() { llm.GeminiBaseURL = old }()

	log := usage.Open(filepath.Join(dir, "usage.jsonl"))
	out, err := Run(context.Background(), dir, "Reviewer", Params{
		Input: "x", Models: []string{"gemini:gemini-2.5-flash"}, Runs: 2, Log: log,
		NewModel: func(*config.Config, string, string) (Completer, error) {
			return &llm.Client{Provider: "gemini", Model: "gemini-2.5-flash"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mr := out.Models[0]
	if len(mr.Calls) != 2 || mr.Calls[0].Usage.InputTokens != 10 || mr.Calls[0].Usage.OutputTokens != 2 {
		t.Fatalf("%+v", mr)
	}
	total, ok := mr.TotalCost()
	if !ok || total <= 0 {
		t.Fatalf("cost not computed from the project's pricing: %v %v", total, ok)
	}

	got, err := usage.ReadAll(log.Path)
	if err != nil || len(got) != 2 {
		t.Fatalf("usage ledger: %v %v", got, err)
	}
	if got[0].Command != "bench" || !got[0].HasCost {
		t.Errorf("%+v", got[0])
	}
}
