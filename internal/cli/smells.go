package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var smellsCmd = &cobra.Command{
	Use:   "smells [PromptName]",
	Short: "Report heuristic quality issues in prompts",
	Long: `Standalone smell report for one prompt or the entire library.

Examples:
  loom smells                   # report all smells across the library
  loom smells SecurityReviewer  # smells for one prompt`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSmells,
}

func runSmells(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	all := len(args) == 0
	name := ""
	if !all {
		name = args[0]
	}

	out, err := tui.RunWithSpinner("detecting smells…", func() (string, error) {
		return tui.RunSmells(name, all, cwd)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
	fmt.Print(out)
	return nil
}
