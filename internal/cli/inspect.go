package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Validate all prompts and blocks",
	RunE:  runInspect,
}

func runInspect(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	type result struct {
		out    string
		hasErr bool
	}
	var res result
	_, _ = tui.RunWithSpinner("inspecting prompt library…", func() (string, error) {
		res.out, res.hasErr = tui.RunInspect(cwd)
		return "", nil
	})

	fmt.Print(res.out)
	if res.hasErr {
		os.Exit(1)
	}
	return nil
}
