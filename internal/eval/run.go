package eval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/contract"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
)

// Model is one model under test.
type Model struct {
	Label  string // "model" or "provider:model", as shown in reports and baselines
	Client Completer
}

// CaseResult is the outcome of one case on one model.
type CaseResult struct {
	Suite     string
	Case      string
	Model     string
	Response  string
	Criteria  []CriterionResult
	Score     int
	Threshold int
	// ContractFailures are violations of the prompt's own contract block (must_include, ...).
	ContractFailures []contract.Failure
	Passed           bool
	Err              error
	Duration         time.Duration
}

// Options controls a run.
type Options struct {
	Models    []Model
	Judge     Completer
	Threshold int // overrides every case's pass mark when > 0
}

// RunSuite runs every case of the suite on every model. Cases are independent: a failure of one
// (a model error, an unresolved variable) is recorded in its result and the rest still run.
func RunSuite(ctx context.Context, reg *registry.Registry, cfg *config.Config, s *Suite, opts Options) []CaseResult {
	node, ok := reg.LookupPrompt(s.Prompt)
	var results []CaseResult
	for _, m := range opts.Models {
		for _, c := range s.Cases {
			res := CaseResult{Suite: s.Name, Case: c.Name, Model: m.Label, Threshold: s.ThresholdFor(c, opts.Threshold)}
			start := time.Now()
			switch {
			case !ok:
				res.Err = fmt.Errorf("prompt %q not found (suite %s)", s.Prompt, s.Name)
			default:
				runCase(ctx, reg, cfg, s, c, node.Contract, m, opts, &res)
			}
			res.Duration = time.Since(start)
			results = append(results, res)
		}
	}
	return results
}

func runCase(ctx context.Context, reg *registry.Registry, cfg *config.Config, s *Suite, c Case, ct *ast.ContractBlock, m Model, opts Options, res *CaseResult) {
	rp, err := resolve.ResolveWithOptions(s.Prompt, reg, resolve.Options{Variables: nonNilVars(c.Vars)})
	if err != nil {
		res.Err = fmt.Errorf("resolve: %w", err)
		return
	}
	if len(rp.UnresolvedTokens) > 0 {
		res.Err = fmt.Errorf("unresolved variables: %s (give them under vars = { ... } in the case)", strings.Join(rp.UnresolvedTokens, ", "))
		return
	}
	system := render.Render(rp, cfg)

	response, err := m.Client.Complete(ctx, llm.Request{System: system, User: c.Input})
	if err != nil {
		res.Err = fmt.Errorf("model call failed: %w", err)
		return
	}
	res.Response = response
	res.ContractFailures = contract.Check(ct, response)

	verdict, err := Judge(ctx, opts.Judge, JudgeInput{Input: c.Input, Response: response, Criteria: nonEmpty(c.Criteria), Reference: c.Reference})
	if err != nil {
		res.Err = err
		return
	}
	res.Criteria = verdict
	res.Score = Mean(verdict)
	res.Passed = res.Score >= res.Threshold && len(res.ContractFailures) == 0
}

func nonNilVars(v map[string]string) map[string]string {
	if v == nil {
		return map[string]string{}
	}
	return v
}
