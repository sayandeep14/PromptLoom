package quest

import (
	"context"
	"fmt"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/contract"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/tui"
)

// StepResult is the outcome of one step.
type StepResult struct {
	Step             Step
	Model            string
	Input            string
	Output           string
	Usage            llm.Usage
	ContractFailures []contract.Failure
	Err              error
	Duration         time.Duration
}

// Outcome is a whole quest run.
type Outcome struct {
	Quest   *Quest
	Steps   []StepResult
	Stopped bool // true when a step failed (or broke its contract) and the rest were skipped
}

// OK reports whether every step ran and passed its contract (steps with no contract always
// count as OK).
func (o *Outcome) OK() bool {
	for _, s := range o.Steps {
		if s.Err != nil || len(s.ContractFailures) > 0 {
			return false
		}
	}
	return !o.Stopped
}

// Params controls a run.
type Params struct {
	Input           string
	Vars            map[string]string // applied to every step; a step's own `vars` overrides these
	Model           string            // "model" or "provider:model"; overrides every step's default
	ContinueOnError bool
	Stream          bool
	// NewModel builds the client for one step. Tests replace it; the default uses llm.New.
	NewModel func(cfg *config.Config, provider, model string) (agent.Model, error)
	// OnDelta, if set, is called with each piece of a step's answer as it streams in.
	OnDelta func(stepIndex int, delta string)
}

func defaultNewModel(cfg *config.Config, provider, model string) (agent.Model, error) {
	c, err := llm.New(cfg, provider, model)
	if err != nil {
		return nil, err
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	return c, nil
}

// Run executes a quest's steps in order. Each step renders its own prompt (exactly as `loom run`
// would, including the permission check on any --with sources) and sends it one message, built
// from the step's `input`/`input_file` with {{quest.input}} and {{quest.previous}} substituted.
// A step that errors, or whose prompt declares a contract the answer breaks, stops the quest
// (skipping the rest) unless the step or Params asks to continue.
func Run(ctx context.Context, cwd string, q *Quest, p Params, perm *agent.Permission) (*Outcome, error) {
	if p.NewModel == nil {
		p.NewModel = defaultNewModel
	}
	out := &Outcome{Quest: q}
	previous := ""

	for i, step := range q.Steps {
		if err := perm.CheckSources(cwd, step.With, ""); err != nil {
			return nil, fmt.Errorf("step %q: %w", step.Name, err)
		}

		vars := map[string]string{}
		for k, v := range p.Vars {
			vars[k] = v
		}
		for k, v := range step.Vars {
			vars[k] = v
		}

		prep, err := tui.PrepareRun(step.Prompt, tui.WeaveOptions{
			Variables: vars, Variant: step.Variant, Overlays: step.Overlay, Env: step.Env, WithSources: step.With,
		}, cwd)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", step.Name, err)
		}

		provider, model := llm.ParseSpec(p.Model)
		client, err := p.NewModel(prep.Config, provider, model)
		if err != nil {
			return nil, fmt.Errorf("step %q: %w", step.Name, err)
		}
		resolvedProvider, resolvedModel, _, _ := llm.Resolve(prep.Config, provider, model)

		input := substitute(step.Input, p.Input, previous)
		sess := &agent.Session{Model: client, System: prep.Body, Contract: prep.Contract, Stream: p.Stream}

		res := StepResult{Step: step, Input: input, Model: resolvedProvider + ":" + resolvedModel}
		start := time.Now()
		var onDelta func(string)
		if p.OnDelta != nil {
			idx := i
			onDelta = func(d string) { p.OnDelta(idx, d) }
		}
		reply, err := sess.Send(ctx, input, onDelta)
		res.Duration = time.Since(start)
		res.Output = reply.Text
		res.Usage = reply.Usage
		res.Err = err
		res.ContractFailures = reply.ContractFailures
		out.Steps = append(out.Steps, res)

		if err != nil || len(reply.ContractFailures) > 0 {
			if !p.ContinueOnError && !step.ContinueOnFail {
				out.Stopped = true
				return out, nil
			}
		}
		previous = reply.Text
	}
	return out, nil
}
