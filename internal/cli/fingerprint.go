package cli

import (
	"fmt"

	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
)

var fingerprintCmd = &cobra.Command{
	Use:   "fingerprint <PromptName>",
	Short: "Print the stable fingerprint for a resolved prompt",
	Args:  cobra.ExactArgs(1),
	RunE:  runFingerprint,
}

func runFingerprint(cmd *cobra.Command, args []string) error {
	cwd, err := resolveProjectDir()
	if err != nil {
		return err
	}
	out, err := tui.RunFingerprint(args[0], cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", args[0], err)
	}
	fmt.Print(out)
	return nil
}
