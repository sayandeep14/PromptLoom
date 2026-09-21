package cli

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/term"
)

// version is overridden at build time via -ldflags "-X <module>/loomlocker/cli.version=vX.Y.Z".
var version = "4.2.0"

// ReadPassword prompts with label and reads a masked password from stdin.
// Falls back to plain read if stdin is not a terminal.
func ReadPassword(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	fd := int(syscall.Stdin)
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr) // newline after masked input
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	// Non-terminal fallback (e.g., piped input in tests).
	var pwd string
	_, err := fmt.Scanln(&pwd)
	return pwd, err
}
