package optimize

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/loader"
)

// StepOptions controls one refine attempt.
type StepOptions struct {
	Eval    eval.Params    // Models, Judge, Threshold, Dir; Names is overridden to just the target prompt
	Refiner eval.Completer // the model asked to propose a rewrite (often the same as the judge)
}

// StepResult is the outcome of one Step: always returned with Before filled in; a candidate is
// present only when there was something to fix and the refiner's proposal was accepted.
type StepResult struct {
	Prompt  string
	Before  *Score
	After   *Score // filled in by Loop after applying and re-scoring; nil otherwise
	Message string // why there is no candidate, or what happened
	File    string // the prompt's source file (once a candidate exists)
	OrigSrc string
	NewSrc  string
	Diff    string
}

// HasCandidate reports whether Step produced a proposal ready to Apply.
func (r *StepResult) HasCandidate() bool { return r.NewSrc != "" }

// Step scores the prompt, and if it is not already passing, asks the refiner for one improved
// version. It never writes anything; Apply does that, once the caller has seen the diff (and, for
// an unattended run, decided to accept it). A rejected or unusable proposal is reported in
// Message, not returned as an error — only a problem with running eval itself is fatal.
func Step(ctx context.Context, cwd, name string, opts StepOptions) (*StepResult, error) {
	before, err := ComputeScore(ctx, cwd, name, opts.Eval)
	if err != nil {
		return nil, err
	}
	res := &StepResult{Prompt: name, Before: before}
	if before.OK() {
		res.Message = "already passing; nothing to refine"
		return res, nil
	}

	feedback := collectFeedback(before.Outcome)
	if len(feedback) == 0 {
		res.Message = "every failing case errored (see the eval output above) rather than scoring low; there is no judge feedback to refine from"
		return res, nil
	}

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return nil, err
	}
	node, ok := reg.LookupPrompt(name)
	if !ok {
		return nil, fmt.Errorf("prompt %q not found", name)
	}
	file := node.Pos.File
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}

	cand, err := Propose(ctx, opts.Refiner, file, string(src), name, feedback)
	if err != nil {
		res.Message = "the refiner's proposal was rejected: " + err.Error()
		return res, nil
	}
	newSrc, err := format.ReplaceFields(file, string(src), name, cand.Fields)
	if err != nil {
		res.Message = "the refiner's proposal was rejected: " + err.Error()
		return res, nil
	}
	res.File, res.OrigSrc, res.NewSrc = file, string(src), newSrc
	res.Diff = unifiedDiff(string(src), newSrc)
	res.Message = "proposed a change"
	return res, nil
}

// collectFeedback gathers failing-case feedback from every suite and model the outcome covers,
// keeping at most one entry per case (the worst-scoring one, if it was tested on several models).
func collectFeedback(out *eval.Outcome) []Feedback {
	best := map[string]eval.CaseResult{}
	var order []string
	for _, so := range out.Suites {
		for _, r := range so.Results {
			if r.Err != nil || r.Passed {
				continue
			}
			if prev, ok := best[r.Case]; !ok || r.Score < prev.Score {
				if !ok {
					order = append(order, r.Case)
				}
				best[r.Case] = r
			}
		}
	}
	sort.Strings(order)
	var out2 []eval.CaseResult
	for _, c := range order {
		out2 = append(out2, best[c])
	}
	return feedbackFrom(out2)
}
