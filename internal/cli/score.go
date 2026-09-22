package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/optimize"
	"github.com/spf13/cobra"
)

var (
	scoreModels    string
	scoreJudge     string
	scoreDir       string
	scoreFailUnder int
	scoreJSON      bool
)

var scoreCmd = &cobra.Command{
	Use:   "score <PromptName>",
	Short: "Score a prompt with its eval suite as a single number",
	Long: `Runs the eval suite(s) for one prompt (see 'loom eval') and reports the mean of every
case's score as a single 0-100 number — a compact result for scripts, dashboards, or gating a
release at a quality bar, as opposed to 'loom eval', which reports every case.

Needs an eval suite for the prompt (evals/<Name>.eval.toml); there is nothing honest to score
without one.

Examples:
  loom score CodeReviewer
  loom score CodeReviewer --fail-under 80   # exit 1 if the mean is below 80
  loom score CodeReviewer --json`,
	Args: cobra.ExactArgs(1),
	RunE: runScore,
}

func init() {
	scoreCmd.Flags().StringVar(&scoreModels, "models", "", "comma-separated models to test: model or provider:model")
	scoreCmd.Flags().StringVar(&scoreJudge, "judge", "", "judge model (model or provider:model); default: the suite's judge_model, else [testing]")
	scoreCmd.Flags().StringVar(&scoreDir, "dir", "", "directory of eval suites (default: evals)")
	scoreCmd.Flags().IntVar(&scoreFailUnder, "fail-under", 0, "exit 1 if the score is below this (1-100); default: exit 1 unless every case passed")
	scoreCmd.Flags().BoolVar(&scoreJSON, "json", false, "print {\"prompt\",\"mean\",\"cases\",\"passed\",\"errored\"} instead of text")
}

func runScore(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	s, err := optimize.ComputeScore(context.Background(), cwd, args[0], evalParamsFromFlags(scoreModels, scoreJudge, scoreDir, 0, "score"))
	if err != nil {
		return err
	}
	if scoreJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(map[string]any{
			"prompt": s.Prompt, "mean": s.Mean, "cases": s.Cases, "passed": s.Passed, "errored": s.Errored,
		}); err != nil {
			return err
		}
	} else {
		fmt.Print(s.Text())
	}
	if scoreFailUnder > 0 {
		if s.Mean < float64(scoreFailUnder) {
			return fmt.Errorf("score %.1f is below --fail-under %d", s.Mean, scoreFailUnder)
		}
		return nil
	}
	if !s.OK() {
		return fmt.Errorf("not every case passed")
	}
	return nil
}

// evalParamsFromFlags builds an eval.Params the way loom eval, loom score and loom optimize all
// parse --models/--judge/--dir/--threshold. command labels the usage ledger ("score", "optimize").
func evalParamsFromFlags(models, judge, dir string, threshold int, command string) eval.Params {
	var m []string
	for _, x := range strings.Split(models, ",") {
		if x = strings.TrimSpace(x); x != "" {
			m = append(m, x)
		}
	}
	return eval.Params{Models: m, Judge: judge, Dir: dir, Threshold: threshold, Command: command}
}
