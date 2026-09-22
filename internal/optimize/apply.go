package optimize

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sayandeep14/PromptLoom/internal/agent"
)

// Apply writes newSrc to file, after checking permission.write in .loom.config. The write is
// atomic (temp file + rename in the same directory) and keeps the file's existing mode, so a
// crash or a full disk can never leave a half-written prompt file.
func Apply(perm *agent.Permission, file, newSrc string) error {
	if err := perm.CheckWrite(file); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".loomoptimize-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(newSrc); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, file); err != nil {
		os.Remove(name)
		return fmt.Errorf("writing %s: %w", file, err)
	}
	return nil
}
