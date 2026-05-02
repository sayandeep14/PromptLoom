package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var diffAgainstDist bool
var diffAll bool
var diffExitCode bool
var diffSemantic bool

var diffCmd = &cobra.Command{
	Use:   "diff [PromptA] [PromptB]",
	Short: "Show field-aware diff between two prompts or against dist",
	Long: `Compare prompts field-by-field and show what changed.

Examples:
  loom diff SecurityReviewer CodeAssistant          # diff two resolved prompts
  loom diff SecurityReviewer --against-dist         # compare current vs last-rendered dist
  loom diff --all --against-dist                    # all prompts vs dist
  loom diff --all --against-dist --exit-code        # CI mode: exit 1 if any prompt stale
  loom diff SecurityReviewer CodeAssistant --semantic  # show semantic change classes`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().BoolVar(&diffAgainstDist, "against-dist", false, "compare current resolved prompt vs its last-rendered dist file")
	diffCmd.Flags().BoolVar(&diffAll, "all", false, "diff all prompts (requires --against-dist)")
	diffCmd.Flags().BoolVar(&diffExitCode, "exit-code", false, "exit 1 if any changes are found (CI mode)")
	diffCmd.Flags().BoolVar(&diffSemantic, "semantic", false, "show semantic change classifications instead of line diff")
}

func runDiff(cmd *cobra.Command, args []string) error {
	if diffAll && !diffAgainstDist {
		return fmt.Errorf("--all requires --against-dist")
	}
	if !diffAll && !diffAgainstDist && len(args) < 2 {
		return fmt.Errorf("specify two prompt names, or use --against-dist [PromptName]")
	}

	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	a, b := "", ""
	if len(args) >= 1 {
		a = args[0]
	}
	if len(args) >= 2 {
		b = args[1]
	}

	opts := tui.DiffOptions{
		AgainstDist: diffAgainstDist,
		All:         diffAll,
		ExitCode:    diffExitCode,
		Semantic:    diffSemantic,
	}

	spinLabel := "diffing…"
	if a != "" && b != "" {
		spinLabel = fmt.Sprintf("diffing %s vs %s…", a, b)
	} else if a != "" {
		spinLabel = fmt.Sprintf("diffing %s vs dist…", a)
	}

	var hasChanges bool
	out, err := tui.RunWithSpinner(spinLabel, func() (string, error) {
		result, changes, runErr := tui.RunDiff(a, b, opts, cwd)
		hasChanges = changes
		return result, runErr
	})
	if err != nil {
		return err
	}
	fmt.Print(out)

	if diffExitCode && hasChanges {
		os.Exit(1)
	}
	return nil
}
