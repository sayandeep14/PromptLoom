package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var deployDryRun bool
var deployDiff bool
var deployTarget string

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Write configured prompt targets to their destinations",
	Long: `Render configured [[targets]] from loom.toml and write them to disk.

Examples:
  loom deploy
  loom deploy --dry-run
  loom deploy --diff
  loom deploy --target copilot`,
	RunE: runDeploy,
}

func init() {
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "preview which targets would be written")
	deployCmd.Flags().BoolVar(&deployDiff, "diff", false, "show a line diff for changed targets")
	deployCmd.Flags().StringVar(&deployTarget, "target", "", "only deploy targets of a specific format")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, err := tui.RunWithSpinner("deploying targets…", func() (string, error) {
		return tui.RunDeploy(tui.DeployOptions{
			DryRun:       deployDryRun,
			Diff:         deployDiff,
			TargetFormat: deployTarget,
		}, cwd)
	})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
