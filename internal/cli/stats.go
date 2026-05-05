package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var statsAll bool
var statsLimit int

var statsCmd = &cobra.Command{
	Use:   "stats [PromptName]",
	Short: "Show per-field token estimates for a prompt",
	Long: `Show a per-field token breakdown for one prompt, or all prompts sorted by size.

Examples:
  loom stats SecurityReviewer       # per-field breakdown
  loom stats --all                  # all prompts sorted by total tokens
  loom stats --all --limit 4096     # flag prompts that exceed the threshold`,
	RunE: runStats,
}

func init() {
	statsCmd.Flags().BoolVar(&statsAll, "all", false, "show stats for all prompts")
	statsCmd.Flags().IntVar(&statsLimit, "limit", 0, "warn when total tokens exceeds this value")
}

func runStats(cmd *cobra.Command, args []string) error {
	if !statsAll && len(args) == 0 {
		return fmt.Errorf("specify a prompt name or use --all")
	}
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	out, hasErr := tui.RunStats(name, statsAll, statsLimit, cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("stats failed")
	}
	return nil
}
