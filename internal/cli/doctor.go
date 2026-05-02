package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor [PromptName]",
	Short: "Check prompt health and detect smells",
	Long: `Run structural checks and smell detection on one prompt or the entire library.

Examples:
  loom doctor                   # check all prompts
  loom doctor SecurityReviewer  # check one prompt`,
	Args: cobra.MaximumNArgs(1),
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	all := len(args) == 0
	name := ""
	if !all {
		name = args[0]
	}

	out, err := tui.RunWithSpinner("checking prompt health…", func() (string, error) {
		return tui.RunDoctor(name, all, cwd)
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, tui.ErrorStyle.Render("Error: "+err.Error()))
		os.Exit(1)
	}
	fmt.Print(out)
	return nil
}
