package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var contractCmd = &cobra.Command{
	Use:   "contract <PromptName>",
	Short: "Print the declared contract and capabilities for a prompt",
	Long: `Print the contract and capabilities blocks declared in a prompt file.

Examples:
  loom contract BugFixPlanner`,
	Args: cobra.ExactArgs(1),
	RunE: runContract,
}

func runContract(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	out, err := tui.RunWithSpinner("loading contract…", func() (string, error) {
		return tui.RunContract(args[0], cwd)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
	fmt.Print(out)
	return nil
}
