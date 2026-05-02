package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var unravelWithSource bool

var unravelCmd = &cobra.Command{
	Use:   "unravel <PromptName>",
	Short: "Show the fully expanded prompt before rendering",
	Long: `Print the fully resolved prompt fields to stdout without Markdown formatting.
Use --with-source to see which node last contributed each field.`,
	Args: cobra.ExactArgs(1),
	RunE: runUnravel,
}

func init() {
	unravelCmd.Flags().BoolVar(&unravelWithSource, "with-source", false, "show source attribution per field")
}

func runUnravel(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, err := tui.RunUnravel(args[0], unravelWithSource, cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	fmt.Print(out)
	return nil
}
