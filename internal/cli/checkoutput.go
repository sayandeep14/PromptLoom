package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var checkOutputCmd = &cobra.Command{
	Use:   "check-output <PromptName> <output-file>",
	Short: "Validate a model output against a prompt's contract",
	Long: `Read an output file and check it against the contract declared in the prompt.
Exits 0 if all requirements are met, 1 if there are violations.

Examples:
  loom check-output BugFixPlanner response.md`,
	Args: cobra.ExactArgs(2),
	RunE: runCheckOutput,
}

func runCheckOutput(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	type result struct {
		out    string
		passed bool
	}
	var res result

	_, _ = tui.RunWithSpinner("checking output…", func() (string, error) {
		out, passed, err := tui.RunCheckOutput(args[0], args[1], cwd)
		res.out = out
		res.passed = passed
		return "", err
	})

	fmt.Print(res.out)
	if !res.passed {
		os.Exit(1)
	}
	return nil
}
