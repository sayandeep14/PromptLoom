package cli

import (
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/tui"
	"github.com/spf13/cobra"
)

var (
	fmtCheck   bool
	fmtMigrate bool
)

var fmtCmd = &cobra.Command{
	Use:   "fmt",
	Short: "Format .loom source files canonically",
	Long: `Rewrite every .loom source file in the project to canonical formatting.

Comments, env blocks, tags, variables (including secret slots) and every other
declaration are preserved; a file that could not be formatted without losing
something is reported and left untouched.

Use --check to report unformatted files without modifying them. It exits with
status 1 when any file needs formatting, so it can gate CI.

Use --migrate to upgrade v1 source to v2 syntax instead of formatting:

  extends B            →  inherits B
  field:               →  field :=
  list += (child)      →  list := from(parent[*]) and { ... }
  list += (block)      →  list :=

Constructs with no mechanical equivalent (-=, += on an inherited scalar, += in a
prompt whose used block defines the same field, += inside variant/env blocks) are
listed by file and line and left exactly as written. With --check nothing is written
and the exit status is 1 while files still need migrating.`,
	RunE: runFmt,
}

func init() {
	fmtCmd.Flags().BoolVar(&fmtCheck, "check", false, "report unformatted files without modifying them (exit 1 if any found)")
	fmtCmd.Flags().BoolVar(&fmtMigrate, "migrate", false, "rewrite v1 syntax (extends, field:, +=) to v2 instead of formatting")
}

func runFmt(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	if fmtMigrate {
		out, err := tui.RunMigrate(fmtCheck, cwd)
		fmt.Print(out)
		return err
	}
	out, err := tui.RunFmt(fmtCheck, cwd)
	fmt.Print(out)
	return err
}
