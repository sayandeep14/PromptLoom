package cli

import (
	"context"
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/optimize"
	"github.com/sayandeep14/PromptLoom/internal/usage"
	"github.com/spf13/cobra"
)

var (
	optimizeModels     string
	optimizeJudge      string
	optimizeRefiner    string
	optimizeDir        string
	optimizeIterations int
	optimizeTolerance  float64
	optimizeYes        bool
)

var optimizeCmd = &cobra.Command{
	Use:   "optimize <PromptName>",
	Short: "Improve a prompt's field content against its eval suite",
	Long: `Scores a prompt with its eval suite (see 'loom eval'), and if it is not passing, asks a
model to propose better field content, addressing the failing criteria. It always shows the diff.

Without --yes: a single preview. Nothing is written; run it again with --yes to apply and keep
going.

With --yes: applies the proposal, re-scores, and repeats (up to --iterations) as long as the score
keeps improving. If a change makes the score worse, it is reverted immediately. Optimize never
touches anything but the prompt's own fields — its name, inherits list, use lines, var/slot
declarations, variant/env blocks, and contract/capabilities block are never changed; a proposal
that would change any of those is rejected outright. See docs/AGENT_RUNTIME.md, "Exception: loom
optimize", for why this is allowed when general tool use is not.

Needs an eval suite for the prompt (evals/<Name>.eval.toml).

Examples:
  loom optimize CodeReviewer                  # preview a proposed change
  loom optimize CodeReviewer --yes            # apply it, and keep going until it passes
  loom optimize CodeReviewer --yes --iterations 5 --tolerance 5`,
	Args: cobra.ExactArgs(1),
	RunE: runOptimize,
}

func init() {
	optimizeCmd.Flags().StringVar(&optimizeModels, "models", "", "comma-separated models to test against: model or provider:model")
	optimizeCmd.Flags().StringVar(&optimizeJudge, "judge", "", "judge model (model or provider:model); default: the suite's judge_model, else [testing]")
	optimizeCmd.Flags().StringVar(&optimizeRefiner, "refiner", "", "model asked to propose changes; default: same as --judge")
	optimizeCmd.Flags().StringVar(&optimizeDir, "dir", "", "directory of eval suites (default: evals)")
	optimizeCmd.Flags().IntVar(&optimizeIterations, "iterations", optimize.DefaultMaxIterations, "with --yes: how many rounds to attempt")
	optimizeCmd.Flags().Float64Var(&optimizeTolerance, "tolerance", optimize.DefaultTolerance, "points the score may drop before a change is reverted as a regression")
	optimizeCmd.Flags().BoolVar(&optimizeYes, "yes", false, "apply accepted proposals instead of only previewing the first one")
}

func runOptimize(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	_, cfg, err := loader.Load(cwd)
	if err != nil {
		return err
	}

	refinerSpec := optimizeRefiner
	if refinerSpec == "" {
		refinerSpec = optimizeJudge
	}
	refiner, err := buildCompleter(cwd, cfg, refinerSpec, "optimize", "refiner")
	if err != nil {
		return fmt.Errorf("--refiner: %w", err)
	}

	var perm *agent.Permission
	if optimizeYes {
		perm, err = agent.LoadPermission(cwd)
		if err != nil {
			return err
		}
	}

	res, err := optimize.Loop(context.Background(), cwd, args[0], optimize.LoopOptions{
		Eval:          evalParamsFromFlags(optimizeModels, optimizeJudge, optimizeDir, 0, "optimize"),
		Refiner:       refiner,
		MaxIterations: optimizeIterations,
		Tolerance:     optimizeTolerance,
		Apply:         optimizeYes,
		Permission:    perm,
	})
	if err != nil {
		return err
	}
	fmt.Print(res.Text())
	if res.Final == nil || !res.Final.OK() {
		return fmt.Errorf("optimize did not reach a passing score")
	}
	return nil
}

// buildCompleter builds a model client from a "model" / "provider:model" spec (empty = the
// project's [testing] model), for anything (judge, refiner) that only needs to complete a prompt.
// Its calls are recorded to the project's usage ledger under command/role (see internal/usage).
func buildCompleter(cwd string, cfg *config.Config, spec, command, role string) (eval.Completer, error) {
	provider, model := llm.ParseSpec(spec)
	c, err := llm.New(cfg, provider, model)
	if err != nil {
		return nil, err
	}
	usage.Attach(c, usage.Open(usage.DefaultPath(cwd)), cfg, command, role)
	return c, nil
}
