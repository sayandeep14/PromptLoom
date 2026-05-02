package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var listOnlyPrompts bool
var listOnlyBlocks bool

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all prompts and blocks in the project",
	RunE:  runList,
}

func init() {
	listCmd.Flags().BoolVar(&listOnlyPrompts, "prompts", false, "list only prompts")
	listCmd.Flags().BoolVar(&listOnlyBlocks, "blocks", false, "list only blocks")
}

func runList(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	fmt.Print(tui.RunList(cwd, listOnlyPrompts, listOnlyBlocks))
	return nil
}
