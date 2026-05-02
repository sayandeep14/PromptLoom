package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var traceField string
var traceInstruction string
var traceTree bool

var traceCmd = &cobra.Command{
	Use:   "trace [PromptName]",
	Short: "Show inheritance chain and field resolution for a prompt",
	Args:  cobra.RangeArgs(0, 1),
	RunE:  runTrace,
}

func init() {
	traceCmd.Flags().StringVar(&traceField, "field", "", "show detailed trace for a single field")
	traceCmd.Flags().StringVar(&traceInstruction, "instruction", "", "find the source of an exact resolved instruction")
	traceCmd.Flags().BoolVar(&traceTree, "tree", false, "show the inheritance and block tree")
}

func runTrace(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" && !traceTree {
		return fmt.Errorf("specify a prompt name or use --tree")
	}
	out, err := tui.RunTrace(name, tui.TraceOptions{
		Field:       traceField,
		Instruction: traceInstruction,
		Tree:        traceTree,
	}, cwd)
	if err != nil {
		if name != "" {
			return fmt.Errorf("%s: %w", name, err)
		}
		return err
	}
	fmt.Print(out)
	return nil
}
