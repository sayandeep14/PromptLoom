package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var reviewSince string

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Generate a structured PR-friendly diff summary",
	Long: `Aggregate prompt diffs into a Markdown PR review summary.

Examples:
  loom review                   # compare all prompts against dist/
  loom review --since HEAD~3    # compare current renders vs 3 commits ago`,
	RunE: runReview,
}

func init() {
	reviewCmd.Flags().StringVar(&reviewSince, "since", "", "compare against a git ref (e.g. HEAD~3)")
}

func runReview(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	out, err := tui.RunWithSpinner("generating review…", func() (string, error) {
		return tui.RunReview(reviewSince, cwd)
	})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
