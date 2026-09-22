package usage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

func TestLogAppendAndReadAllRoundTrip(t *testing.T) {
	dir := t.TempDir()
	log := Open(filepath.Join(dir, "sub", "usage.jsonl"))
	r1 := Record{Time: time.Now(), Command: "run", Provider: "gemini", Model: "gemini-2.5-flash", Input: 10, Output: 5}
	r2 := Record{Time: time.Now(), Command: "eval", Role: "judge", Provider: "anthropic", Model: "claude-x", Input: 20, Output: 8, CostUSD: 0.01, HasCost: true}
	if err := log.Append(r1); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(r2); err != nil {
		t.Fatal(err)
	}
	got, err := ReadAll(log.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Command != "run" || got[1].Role != "judge" || !got[1].HasCost {
		t.Fatalf("%+v", got)
	}
}

func TestReadAllOfAMissingLedgerIsEmptyNotAnError(t *testing.T) {
	got, err := ReadAll(filepath.Join(t.TempDir(), "nope.jsonl"))
	if err != nil || got != nil {
		t.Fatalf("%v %v", got, err)
	}
}

func TestReadAllSkipsAnUnparsableLineRatherThanFailingEntirely(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.jsonl")
	os.WriteFile(path, []byte("{\"command\":\"run\"}\nnot json\n{\"command\":\"eval\"}\n"), 0o644)
	got, err := ReadAll(path)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
}

func TestAppendIsANoOpOnANilLog(t *testing.T) {
	var log *Log
	if err := log.Append(Record{Command: "run"}); err != nil {
		t.Fatal(err)
	}
}

func TestEstimateCostMatchesOnlyAConfiguredModelCaseInsensitively(t *testing.T) {
	cfg := &config.Config{Pricing: []config.Price{
		{Provider: "Gemini", Model: "gemini-2.5-flash", Input: 0.30, Output: 2.50},
	}}
	cost, ok := EstimateCost(cfg, "gemini", "gemini-2.5-flash", llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if !ok || cost != 2.80 {
		t.Fatalf("cost=%v ok=%v", cost, ok)
	}
	if _, ok := EstimateCost(cfg, "gemini", "gemini-2.5-pro", llm.Usage{InputTokens: 100}); ok {
		t.Error("an unpriced model must never get an invented cost")
	}
	if _, ok := EstimateCost(nil, "gemini", "gemini-2.5-flash", llm.Usage{}); ok {
		t.Error("a nil config must never get an invented cost")
	}
}

func TestAttachRecordsEveryCallAndPricesItWhenKnown(t *testing.T) {
	dir := t.TempDir()
	log := Open(filepath.Join(dir, "usage.jsonl"))
	cfg := &config.Config{Pricing: []config.Price{{Provider: "gemini", Model: "gemini-2.5-flash", Input: 1, Output: 2}}}
	c := &llm.Client{Provider: "gemini", Model: "gemini-2.5-flash"}
	Attach(c, log, cfg, "run", "")
	if c.OnUsage == nil {
		t.Fatal("Attach must set OnUsage on a real *llm.Client")
	}
	c.OnUsage(llm.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})

	got, err := ReadAll(log.Path)
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %v", got, err)
	}
	r := got[0]
	if r.Command != "run" || r.Provider != "gemini" || r.Model != "gemini-2.5-flash" || !r.HasCost || r.CostUSD != 3 {
		t.Fatalf("%+v", r)
	}
}

func TestAttachIsANoOpOnAFakeClient(t *testing.T) {
	type fake struct{}
	dir := t.TempDir()
	log := Open(filepath.Join(dir, "usage.jsonl"))
	// must not panic on a type that is not *llm.Client, and must not create the ledger file
	Attach(&fake{}, log, nil, "run", "")
	if _, err := os.Stat(log.Path); err == nil {
		t.Error("Attach on a fake must not touch the ledger")
	}
}

func TestFilterBySinceCommandAndModel(t *testing.T) {
	now := time.Now()
	recs := []Record{
		{Time: now.Add(-2 * time.Hour), Command: "run", Provider: "gemini", Model: "gemini-2.5-flash"},
		{Time: now, Command: "eval", Provider: "anthropic", Model: "claude-x"},
		{Time: now, Command: "run", Provider: "gemini", Model: "gemini-2.5-pro"},
	}
	if got := (Filter{Since: now.Add(-time.Hour)}).Apply(recs); len(got) != 2 {
		t.Fatalf("since: %+v", got)
	}
	if got := (Filter{Command: "Run"}).Apply(recs); len(got) != 2 {
		t.Fatalf("command: %+v", got)
	}
	if got := (Filter{Model: "gemini-2.5-flash"}).Apply(recs); len(got) != 1 {
		t.Fatalf("bare model: %+v", got)
	}
	if got := (Filter{Model: "anthropic:claude-x"}).Apply(recs); len(got) != 1 {
		t.Fatalf("provider:model: %+v", got)
	}
}

func TestSummarizeAggregatesByCommandAndModel(t *testing.T) {
	recs := []Record{
		{Command: "run", Provider: "gemini", Model: "gemini-2.5-flash", Input: 10, Output: 2},
		{Command: "run", Provider: "gemini", Model: "gemini-2.5-flash", Input: 5, Output: 1, CostUSD: 0.02, HasCost: true},
		{Command: "eval", Provider: "anthropic", Model: "claude-x", Input: 100, Output: 20},
	}
	s := Summarize(recs)
	if s.Calls != 3 || s.Input != 115 || s.Output != 23 {
		t.Fatalf("%+v", s)
	}
	if !s.AnyCost || s.CostUSD != 0.02 {
		t.Fatalf("cost: %+v", s)
	}
	if len(s.ByCommand) != 2 || len(s.ByModel) != 2 {
		t.Fatalf("%+v", s)
	}
	if !s.UnknownPricedAt["anthropic:claude-x"] {
		t.Errorf("claude-x had no cost on any record and should be listed as unpriced: %+v", s.UnknownPricedAt)
	}
}

func TestSummaryTextOfNoRecordsSaysSo(t *testing.T) {
	if got := Summarize(nil).Text(); got != "no usage recorded yet\n" {
		t.Errorf("%q", got)
	}
}

func TestSummaryTextReportsCostAndUnpricedModels(t *testing.T) {
	s := Summarize([]Record{
		{Command: "run", Provider: "gemini", Model: "gemini-2.5-flash", Input: 10, Output: 2, CostUSD: 0.5, HasCost: true},
		{Command: "eval", Provider: "anthropic", Model: "claude-x", Input: 100, Output: 20},
	})
	text := s.Text()
	for _, want := range []string{"2 call(s)", "110 input", "22 output", "$0.50", "by command:", "by model:", "no price on file for: anthropic:claude-x"} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}
