package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var graphFormat string
var graphUnused bool

var graphCmd = &cobra.Command{
	Use:   "graph [PromptName]",
	Short: "Render the prompt dependency graph",
	Long: `Render the full prompt dependency graph or a subgraph for one prompt.

Examples:
  loom graph                        # ASCII tree of the full library
  loom graph SecurityReviewer       # subgraph rooted at SecurityReviewer
  loom graph --format mermaid       # Mermaid diagram
  loom graph --format dot           # Graphviz DOT source
  loom graph --unused               # list blocks not used by any prompt`,
	RunE: runGraph,
}

func init() {
	graphCmd.Flags().StringVar(&graphFormat, "format", "ascii", "output format: ascii, mermaid, dot")
	graphCmd.Flags().BoolVar(&graphUnused, "unused", false, "show blocks not used by any prompt")
}

func runGraph(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	out, hasErr := tui.RunGraph(name, graphFormat, graphUnused, cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("graph failed")
	}
	return nil
}
