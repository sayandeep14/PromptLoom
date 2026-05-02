package cli

import (
	"fmt"
	"os"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Generate or update loom.lock",
	Long: `Compute fingerprints for all prompts and blocks and write them to loom.lock.

Examples:
  loom lock`,
	Args: cobra.NoArgs,
	RunE: runLock,
}

var checkLockCmd = &cobra.Command{
	Use:   "check-lock",
	Short: "Verify current fingerprints match loom.lock",
	Long: `Compare the current resolved fingerprints against loom.lock.
Exits 0 if everything matches, 1 if there are mismatches.

Examples:
  loom check-lock`,
	Args: cobra.NoArgs,
	RunE: runCheckLock,
}

func runLock(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, err := tui.RunWithSpinner("generating loom.lock…", func() (string, error) {
		return tui.RunLock(cwd)
	})
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func runCheckLock(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}

	type result struct {
		out      string
		mismatch bool
	}
	var res result
	_, _ = tui.RunWithSpinner("checking loom.lock…", func() (string, error) {
		res.out, res.mismatch, _ = tui.RunCheckLock(cwd)
		return "", nil
	})

	fmt.Print(res.out)
	if res.mismatch {
		os.Exit(1)
	}
	return nil
}
