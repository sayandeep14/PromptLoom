package cli

import (
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var playgroundCmd = &cobra.Command{
	Use:   "playground <Name>",
	Short: "Live interactive preview of a prompt with variant/format/overlay controls",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := resolveProjectDir()
		if err != nil {
			return err
		}
		return tui.RunPlayground(args[0], cwd)
	},
}

func init() {
	rootCmd.AddCommand(playgroundCmd)
}
