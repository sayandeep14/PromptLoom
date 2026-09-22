package optimize

import (
	"context"
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/eval"
)

// DefaultMaxIterations bounds loom optimize so it cannot run away.
const DefaultMaxIterations = 3

// DefaultTolerance is how many points the score may drop after a change before it counts as a
// regression (judge scores wobble a little between runs, the same reasoning as eval's baselines).
const DefaultTolerance = 3.0

// LoopOptions controls a full optimize run.
type LoopOptions struct {
	Eval          eval.Params
	Refiner       eval.Completer
	MaxIterations int
	Tolerance     float64
	// Apply, when true, writes each accepted candidate and re-scores before deciding whether to
	// continue. When false, Loop does exactly one Step and stops: a preview, never a write.
	Apply      bool
	Permission *agent.Permission // required when Apply is true
}

// LoopResult is everything a run of `loom optimize` produced.
type LoopResult struct {
	Prompt string
	Steps  []*StepResult
	Final  *Score
	Reason string
}

// Loop repeatedly scores the prompt and asks the refiner for a better version, applying and
// re-scoring accepted candidates, until one of: the prompt passes, a step is rejected, an applied
// change makes the score worse (in which case it is reverted immediately), the score stops
// improving, or the iteration limit is reached. With Apply false it produces exactly one preview
// step and never writes anything.
func Loop(ctx context.Context, cwd, name string, opts LoopOptions) (*LoopResult, error) {
	max := opts.MaxIterations
	if max <= 0 {
		max = DefaultMaxIterations
	}
	tol := opts.Tolerance
	if tol <= 0 {
		tol = DefaultTolerance
	}
	if opts.Apply && opts.Permission == nil {
		return nil, fmt.Errorf("internal error: Apply requires Permission")
	}

	res := &LoopResult{Prompt: name}
	for i := 0; i < max; i++ {
		step, err := Step(ctx, cwd, name, StepOptions{Eval: opts.Eval, Refiner: opts.Refiner})
		if err != nil {
			return nil, err
		}
		res.Steps = append(res.Steps, step)
		res.Final = step.Before

		if step.Before.OK() {
			res.Reason = "already passing"
			return res, nil
		}
		if !step.HasCandidate() {
			res.Reason = step.Message
			return res, nil
		}
		if !opts.Apply {
			res.Reason = "preview only — pass --yes to apply and continue"
			return res, nil
		}

		if err := Apply(opts.Permission, step.File, step.NewSrc); err != nil {
			return nil, err
		}
		after, err := ComputeScore(ctx, cwd, name, opts.Eval)
		if err != nil {
			return nil, err
		}
		step.After = after
		res.Final = after

		if after.Mean < step.Before.Mean-tol {
			if rerr := Apply(opts.Permission, step.File, step.OrigSrc); rerr != nil {
				return nil, fmt.Errorf("the change made the score worse (%.1f -> %.1f) and reverting %s failed: %w — the file is left with the worse version, please check it",
					step.Before.Mean, after.Mean, step.File, rerr)
			}
			res.Reason = fmt.Sprintf("the change made the score worse (%.1f -> %.1f); reverted", step.Before.Mean, after.Mean)
			res.Final = step.Before
			return res, nil
		}
		if after.OK() {
			res.Reason = "reached a passing score"
			return res, nil
		}
		if after.Mean <= step.Before.Mean+tol {
			res.Reason = fmt.Sprintf("the score stopped improving (%.1f -> %.1f)", step.Before.Mean, after.Mean)
			return res, nil
		}
		// improved but not yet passing: loop again
	}
	res.Reason = fmt.Sprintf("reached the iteration limit (%d)", max)
	return res, nil
}
