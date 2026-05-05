package cli

import (
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var graphFormat string
var graphUnused bool
var graphNoInteractive bool

var graphCmd = &cobra.Command{
	Use:   "graph [PromptName]",
	Short: "Render the prompt dependency graph",
	Long: `Render the full prompt dependency graph or a subgraph for one prompt.

When running in a terminal the interactive split-panel browser is launched by default.
Use --no-interactive to get plain text output (useful for piping / CI).

Examples:
  loom graph                        # interactive graph browser (default)
  loom graph SecurityReviewer       # open browser pre-selected at SecurityReviewer
  loom graph --no-interactive       # plain ASCII tree
  loom graph --format mermaid       # Mermaid diagram (plain text)
  loom graph --format dot           # Graphviz DOT source (plain text)
  loom graph --unused               # list blocks not used by any prompt`,
	RunE: runGraph,
}

func init() {
	graphCmd.Flags().StringVar(&graphFormat, "format", "ascii", "output format: ascii, mermaid, dot")
	graphCmd.Flags().BoolVar(&graphUnused, "unused", false, "show blocks not used by any prompt")
	graphCmd.Flags().BoolVar(&graphNoInteractive, "no-interactive", false, "disable the interactive TUI browser")
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

	// Launch interactive browser when: ascii format, not --unused, stdout is a TTY,
	// and --no-interactive was not requested.
	if graphFormat == "ascii" && !graphUnused && !graphNoInteractive &&
		isatty.IsTerminal(os.Stdout.Fd()) {
		return tui.RunGraphInteractive(cwd, name)
	}

	out, hasErr := tui.RunGraph(name, graphFormat, graphUnused, cwd)
	fmt.Print(out)
	if hasErr {
		return fmt.Errorf("graph failed")
	}
	return nil
}
