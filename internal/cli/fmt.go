package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var fmtCheck bool

var fmtCmd = &cobra.Command{
	Use:   "fmt",
	Short: "Format .loom source files canonically",
	Long: `Rewrite every .loom source file in the project to canonical formatting.

Use --check to report unformatted files without modifying them (useful in CI).`,
	RunE: runFmt,
}

func init() {
	fmtCmd.Flags().BoolVar(&fmtCheck, "check", false, "report unformatted files without modifying them (exit 1 if any found)")
}

func runFmt(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, err := tui.RunFmt(fmtCheck, cwd)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}
