package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayandeepgiri/promptloom/loomlocker/internal/config"
	"github.com/sayandeepgiri/promptloom/loomlocker/internal/locker"
	"github.com/spf13/cobra"
)

var recoverCmd = &cobra.Command{
	Use:   "recover",
	Short: "Restore real values after loomlocker ended while secrets were locked",
	Long: `In recoverable mode ("recoverable": true in .loom.config) loomlocker saves the real
values, encrypted with your session password, in .loom.secret.lock before it locks anything.

If loomlocker crashed, was killed, or the machine lost power while secrets were locked,
run this command with the same password to put the real values back. It refuses to run
while loomlocker itself is running (use 'loomlocker unlock' or 'loomlocker stop' then).`,
	Args: cobra.NoArgs,
	RunE: runRecover,
}

func runRecover(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, workDir, err := config.Load(cwd)
	if err != nil {
		return err
	}
	journal := filepath.Join(workDir, locker.JournalFilename)
	if _, err := os.Stat(journal); errors.Is(err, os.ErrNotExist) {
		fmt.Println("Nothing to recover: no " + locker.JournalFilename + " found.")
		return nil
	}
	if isPortOpen(cfg.Locker.Port) {
		return fmt.Errorf("loomlocker is running on port %s; use 'loomlocker unlock' or 'loomlocker stop' instead", cfg.Locker.Port)
	}

	password, err := ReadPassword("Session password used when the secrets were locked")
	if err != nil {
		return err
	}
	mapping, err := locker.ReadJournal(journal, password)
	if err != nil {
		return err
	}
	n, err := locker.RestoreFromMapping(mapping)
	if err != nil {
		return fmt.Errorf("restore failed, the recovery file was kept: %w", err)
	}
	if err := os.Remove(journal); err != nil {
		return fmt.Errorf("values restored, but could not delete %s: %w", locker.JournalFilename, err)
	}
	fmt.Printf("✓  restored %d value(s); removed %s\n", n, locker.JournalFilename)
	return nil
}
