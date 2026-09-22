package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/agent"
	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/optimize"
	"github.com/spf13/cobra"
)

var (
	evalModels    string
	evalJudge     string
	evalRecord    bool
	evalCompare   bool
	evalTolerance int
	evalStrict    bool
	evalThreshold int
	evalDir       string
	evalRefine    bool
	evalYes       bool
)

var evalCmd = &cobra.Command{
	Use:   "eval [Name...]",
	Short: "Score a prompt's answers with a judge model; catch regressions",
	Long: `Run eval suites (evals/<Name>.eval.toml): each case sends an input through a prompt to a
model, and a judge model grades the answer against the case's criteria from 0 to 100. A case
passes when its mean reaches the threshold (default 70) and the prompt's own contract holds.

A suite:

  prompt    = "CodeReviewer"
  threshold = 70                       # optional pass mark for every case
  judge_model = "anthropic:claude-x"   # optional: who grades

  [[case]]
  name     = "flags SQL injection"
  input    = "Review: db.Query(\"SELECT * FROM t WHERE id=\" + id)"
  criteria = ["names the injection risk", "suggests a parameterised query"]
  min_score = 80                       # optional, overrides the threshold
  vars     = { repo_name = "demo" }    # optional values for the prompt's slots
  # reference = "..."                  # optional model answer to compare against

Compare models side by side with --models (each is "model" or "provider:model"). Save the
scores with --record, and later --compare to fail when a change to a prompt made answers worse.

--refine asks a model to propose a fix for every prompt that did not pass, and shows the diff
(see 'loom optimize', which this uses for one iteration per prompt). Add --yes to apply it.

The model under test and the judge use the provider, key and model of [testing] in loom.toml.
The judge sees the answer as data and is told to ignore any instructions inside it.

Examples:
  loom eval
  loom eval CodeReviewer --models gemini-2.5-flash,anthropic:claude-sonnet-4-6
  loom eval --record
  loom eval --compare                 # exit 1 on a regression (drop of more than 5 points)
  loom eval --compare --strict        # any drop counts
  loom eval --refine                  # preview a fix for anything that failed
  loom eval --refine --yes            # apply it`,
	RunE: runEval,
}

func init() {
	evalCmd.Flags().StringVar(&evalModels, "models", "", "comma-separated models to compare: model or provider:model")
	evalCmd.Flags().StringVar(&evalJudge, "judge", "", "judge model (model or provider:model); default: the suite's judge_model, else [testing]")
	evalCmd.Flags().BoolVar(&evalRecord, "record", false, "save the scores as the baseline")
	evalCmd.Flags().BoolVar(&evalCompare, "compare", false, "compare with the recorded baseline; exit 1 on a regression")
	evalCmd.Flags().IntVar(&evalTolerance, "tolerance", eval.DefaultTolerance, "points a score may drop before it counts as a regression")
	evalCmd.Flags().BoolVar(&evalStrict, "strict", false, "with --compare: any drop is a regression (tolerance 0)")
	evalCmd.Flags().IntVar(&evalThreshold, "threshold", 0, "override the pass mark of every case (1-100)")
	evalCmd.Flags().StringVar(&evalDir, "dir", "", "directory of eval suites (default: evals)")
	evalCmd.Flags().BoolVar(&evalRefine, "refine", false, "propose a fix (see loom optimize) for every prompt that did not pass")
	evalCmd.Flags().BoolVar(&evalYes, "yes", false, "with --refine: apply the fix instead of only previewing it")
}

func runEval(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	if evalThreshold < 0 || evalThreshold > 100 {
		return fmt.Errorf("--threshold must be between 1 and 100")
	}
	var models []string
	for _, m := range strings.Split(evalModels, ",") {
		if m = strings.TrimSpace(m); m != "" {
			models = append(models, m)
		}
	}
	ctx := context.Background()
	evalOpts := eval.Params{
		Names: args, Models: models, Judge: evalJudge,
		Record: evalRecord, Compare: evalCompare, Tolerance: evalTolerance, Strict: evalStrict,
		Threshold: evalThreshold, Dir: evalDir, Command: "eval",
	}
	out, err := eval.RunProject(ctx, cwd, evalOpts)
	if err != nil {
		return err
	}
	fmt.Print(out.Text())

	if !evalRefine {
		if !out.OK() {
			return fmt.Errorf("eval failed")
		}
		return nil
	}
	return runRefine(ctx, cwd, out, models)
}

// runRefine proposes (and, with --yes, applies) one fix per prompt that did not pass.
func runRefine(ctx context.Context, cwd string, out *eval.Outcome, models []string) error {
	_, cfg, err := loader.Load(cwd)
	if err != nil {
		return err
	}
	refiner, err := buildCompleter(cwd, cfg, evalJudge, "eval", "refiner")
	if err != nil {
		return fmt.Errorf("--refine needs a working model: %w", err)
	}
	var perm *agent.Permission
	if evalYes {
		perm, err = agent.LoadPermission(cwd)
		if err != nil {
			return err
		}
	}

	seen := map[string]bool{}
	fmt.Println("\n── refine ──")
	for _, so := range out.Suites {
		name := so.Suite.Prompt
		if seen[name] || eval.Summarize(so.Results).OK() {
			continue
		}
		seen[name] = true
		res, err := optimize.Loop(ctx, cwd, name, optimize.LoopOptions{
			Eval:          eval.Params{Models: models, Judge: evalJudge, Threshold: evalThreshold, Dir: evalDir, Command: "eval"},
			Refiner:       refiner,
			MaxIterations: 1,
			Apply:         evalYes,
			Permission:    perm,
		})
		if err != nil {
			return err
		}
		fmt.Print(res.Text())
	}

	if !evalYes {
		if !out.OK() {
			return fmt.Errorf("eval failed")
		}
		return nil
	}
	// re-run eval for an honest exit code: refine may have fixed some or all of it
	final, err := eval.RunProject(ctx, cwd, eval.Params{Names: nil, Models: models, Judge: evalJudge, Threshold: evalThreshold, Dir: evalDir})
	if err != nil {
		return err
	}
	if !final.OK() {
		return fmt.Errorf("eval still fails after refining")
	}
	return nil
}
