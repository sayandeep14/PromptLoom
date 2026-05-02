package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var ciCmd = &cobra.Command{
	Use:   "ci",
	Short: "Run all CI gates (inspect + doctor + check-lock + diff)",
	Long: `A single command for pull-request pipelines. Runs:
  1. loom inspect  — syntax, refs, cycles
  2. loom doctor   — health, smells, freshness
  3. loom check-lock — dist files match lockfile
  4. loom diff --all --against-dist — rendered output not stale

Exits 0 only if all checks pass.

Examples:
  loom ci`,
	Args: cobra.NoArgs,
	RunE: runCI,
}

func runCI(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	type result struct {
		out    string
		failed bool
	}
	var res result
	_, _ = tui.RunWithSpinner("running CI gates…", func() (string, error) {
		res.out, res.failed, _ = tui.RunCI(cwd)
		return "", nil
	})

	fmt.Print(res.out)
	if res.failed {
		os.Exit(1)
	}
	return nil
}
