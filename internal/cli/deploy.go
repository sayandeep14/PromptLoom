package cli

import (
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/spf13/cobra"
)

var deployDryRun bool
var deployDiff bool
var deployTarget string
var deployCheck bool

var deployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Write configured prompt targets to their destinations",
	Long: `Render configured [[targets]] from loom.toml and write them to disk.

Examples:
  loom deploy
  loom deploy --dry-run
  loom deploy --diff
  loom deploy --target copilot
  loom deploy --check            # CI: exit 1 if any target file is missing or out of date

--check writes nothing. It renders every target and compares it with the file on disk, so a
hand-edited CLAUDE.md, .cursor rule or AGENTS.md that drifted from its prompt is caught.`,
	RunE: runDeploy,
}

func init() {
	deployCmd.Flags().BoolVar(&deployDryRun, "dry-run", false, "preview which targets would be written")
	deployCmd.Flags().BoolVar(&deployDiff, "diff", false, "show a line diff for changed targets")
	deployCmd.Flags().BoolVar(&deployCheck, "check", false, "write nothing; exit 1 if any target file is missing or differs from its prompt")
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
			Check:        deployCheck,
			TargetFormat: deployTarget,
		}, cwd)
	})
	// The output says which targets failed or drifted, so it is printed even alongside an error.
	fmt.Print(out)
	return err
}
