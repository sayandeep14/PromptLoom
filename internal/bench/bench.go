// Package bench times and prices a prompt's answer across one or more models: not a quality
// judgement (see internal/eval for that), only how long an answer took and what it cost, so
// choosing a model can weigh speed and price alongside loom eval's scores.
package bench

import (
	"context"
	"fmt"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/sayandeep14/PromptLoom/internal/usage"
)

// Completer is the minimal model interface bench needs.
type Completer interface {
	Complete(ctx context.Context, r llm.Request) (string, error)
}

// Call is one request to one model.
type Call struct {
	Duration time.Duration
	Usage    llm.Usage
	CostUSD  float64
	HasCost  bool
	Err      error
}

// ModelResult is every call made against one model.
type ModelResult struct {
	Label           string // "provider:model"
	Provider, Model string
	Calls           []Call
}

// Errors counts calls that failed.
func (m ModelResult) Errors() int {
	n := 0
	for _, c := range m.Calls {
		if c.Err != nil {
			n++
		}
	}
	return n
}

// ok are the calls that succeeded — the ones the averages below are computed over.
func (m ModelResult) ok() []Call {
	var out []Call
	for _, c := range m.Calls {
		if c.Err == nil {
			out = append(out, c)
		}
	}
	return out
}

// AvgDuration is the mean latency of the successful calls (0 if none succeeded).
func (m ModelResult) AvgDuration() time.Duration {
	ok := m.ok()
	if len(ok) == 0 {
		return 0
	}
	var total time.Duration
	for _, c := range ok {
		total += c.Duration
	}
	return total / time.Duration(len(ok))
}

// AvgInput and AvgOutput are the mean token counts of the successful calls.
func (m ModelResult) AvgInput() float64 {
	return m.avg(func(c Call) int { return c.Usage.InputTokens })
}
func (m ModelResult) AvgOutput() float64 {
	return m.avg(func(c Call) int { return c.Usage.OutputTokens })
}

func (m ModelResult) avg(f func(Call) int) float64 {
	ok := m.ok()
	if len(ok) == 0 {
		return 0
	}
	sum := 0
	for _, c := range ok {
		sum += f(c)
	}
	return float64(sum) / float64(len(ok))
}

// TotalCost sums the successful calls' cost; ok is false unless every one of them had a known
// price (a partial total would understate the real cost without saying so).
func (m ModelResult) TotalCost() (cost float64, ok bool) {
	calls := m.ok()
	if len(calls) == 0 {
		return 0, false
	}
	for _, c := range calls {
		if !c.HasCost {
			return 0, false
		}
		cost += c.CostUSD
	}
	return cost, true
}

// Outcome is a whole bench run: one prompt, one input, one or more models.
type Outcome struct {
	Prompt string
	Input  string
	Models []ModelResult
}

// Params controls a run.
type Params struct {
	Input     string
	Variables map[string]string // render/slot variables, as for `loom weave`/`loom run`
	Models    []string          // "model" or "provider:model"; empty means just the project's default
	Runs      int               // calls per model; <= 0 means 1

	// NewModel builds the client for one model. Tests replace it; the default uses llm.New. Usage
	// is only recorded (see internal/usage) when the returned Completer is a real *llm.Client —
	// exactly the commands its other callers already follow.
	NewModel func(cfg *config.Config, provider, model string) (Completer, error)
	// Log records token/cost history under command "bench"; nil means <project>/.loom/usage.jsonl.
	Log *usage.Log
}

func defaultNewModel(cfg *config.Config, provider, model string) (Completer, error) {
	c, err := llm.New(cfg, provider, model)
	if err != nil {
		return nil, err
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return c, nil
}

// Run sends p.Input to name (rendered exactly as `loom run` would, with no variables — bench
// compares models on a fixed prompt/input pair, not a templated one) p.Runs times per model.
func Run(ctx context.Context, cwd, name string, p Params) (*Outcome, error) {
	runs := p.Runs
	if runs <= 0 {
		runs = 1
	}
	specs := p.Models
	if len(specs) == 0 {
		specs = []string{""}
	}
	newModel := p.NewModel
	if newModel == nil {
		newModel = defaultNewModel
	}
	log := p.Log
	if log == nil {
		log = usage.Open(usage.DefaultPath(cwd))
	}

	prep, err := tui.PrepareRun(name, tui.WeaveOptions{Variables: p.Variables}, cwd)
	if err != nil {
		return nil, err
	}

	out := &Outcome{Prompt: name, Input: p.Input}
	for _, spec := range specs {
		provider, model := llm.ParseSpec(spec)
		client, err := newModel(prep.Config, provider, model)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", spec, err)
		}
		resolvedProvider, resolvedModel, _, err := llm.Resolve(prep.Config, provider, model)
		if err != nil {
			return nil, fmt.Errorf("model %q: %w", spec, err)
		}
		mr := ModelResult{Label: resolvedProvider + ":" + resolvedModel, Provider: resolvedProvider, Model: resolvedModel}

		var last llm.Usage
		if lc, ok := client.(*llm.Client); ok {
			lc.OnUsage = func(u llm.Usage) {
				last = u
				rec := usage.Record{Time: time.Now(), Command: "bench", Provider: resolvedProvider, Model: resolvedModel, Input: u.InputTokens, Output: u.OutputTokens}
				if cost, ok := usage.EstimateCost(prep.Config, resolvedProvider, resolvedModel, u); ok {
					rec.CostUSD, rec.HasCost = cost, true
				}
				log.Append(rec)
			}
		}

		for i := 0; i < runs; i++ {
			last = llm.Usage{}
			start := time.Now()
			_, cerr := client.Complete(ctx, llm.Request{System: prep.Body, User: p.Input})
			call := Call{Duration: time.Since(start), Err: cerr}
			if cerr == nil {
				call.Usage = last
				if cost, ok := usage.EstimateCost(prep.Config, resolvedProvider, resolvedModel, last); ok {
					call.CostUSD, call.HasCost = cost, true
				}
			}
			mr.Calls = append(mr.Calls, call)
		}
		out.Models = append(out.Models, mr)
	}
	return out, nil
}
