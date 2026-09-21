package cli

import (
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/spf13/cobra"
)

var impactJSON bool

var impactCmd = &cobra.Command{
	Use:   "impact <Name>",
	Short: "Show which prompts are affected when a prompt or block changes",
	Long: `Show the blast radius of a change: every prompt that would be affected if you edited or
removed the named prompt or block.

  prompt   the prompts that inherit from it, directly and further down the chain
  block    the prompts that use it, and everything that inherits from those

Use it before refactoring a base prompt or a shared block. With --json the result can be
consumed by scripts and CI.

Examples:
  loom impact BaseEngineer
  loom impact SecurityChecklist
  loom impact BaseEngineer --json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := resolveProjectDir()
		if err != nil {
			return err
		}
		out, err := tui.RunImpact(args[0], impactJSON, cwd)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	},
}

func init() {
	impactCmd.Flags().BoolVar(&impactJSON, "json", false, "print the result as JSON")
}
