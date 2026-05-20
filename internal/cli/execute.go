package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/sayandeepgiri/promptloom/internal/lockerclient"
	"github.com/sayandeepgiri/promptloom/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var executeUnlock bool

var executeCmd = &cobra.Command{
	Use:   "execute <custom-command>",
	Short: "Run a custom command defined in .loom.config",
	Long: `Look up <custom-command> in the [custom] section of .loom.config and run it.

With --unlock, if a loomlocker server is running and secrets are locked,
loom will prompt for the session password, unlock the secrets, run the command,
and let loomlocker's auto-relock timer re-lock them automatically.

Examples:
  loom execute runproject
  loom execute testproject --unlock`,
	Args: cobra.ExactArgs(1),
	RunE: runExecute,
}

func init() {
	executeCmd.Flags().BoolVar(&executeUnlock, "unlock", false,
		"unlock secrets via loomlocker before running (if server is active)")
}

func runExecute(cmd *cobra.Command, args []string) error {
	cmdKey := args[0]
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Load .loom.config for the custom command map and locker URL.
	cfg, err := loadLoomConfig(cwd)
	if err != nil {
		return fmt.Errorf(".loom.config: %w", err)
	}

	shellCmd, ok := cfg.Custom[cmdKey]
	if !ok {
		available := make([]string, 0, len(cfg.Custom))
		for k := range cfg.Custom {
			available = append(available, k)
		}
		return fmt.Errorf("unknown command %q — available: %s",
			cmdKey, strings.Join(available, ", "))
	}

	// Handle --unlock: talk to loomlocker if running and locked.
	if executeUnlock {
		lockerURL := cfg.Locker.LockHost + ":" + cfg.Locker.Port
		client := lockerclient.New(lockerURL)
		if client.IsRunning() {
			if client.IsLocked() {
				password, err := readMaskedPassword("  LoomLocker password")
				if err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "\n")
				if err := client.Unlock(password); err != nil {
					return fmt.Errorf("unlock failed: %w", err)
				}
				fmt.Printf("%s  secrets unlocked (auto-relock in %ds)\n",
					tui.SuccessStyle.Render("✓"),
					cfg.Locker.UnlockDurationSec)
			} else {
				fmt.Printf("%s  secrets already unlocked\n",
					tui.MutedStyle.Render("→"))
			}
		} else {
			fmt.Printf("%s  loomlocker not running — continuing without unlock\n",
				tui.MutedStyle.Render("→"))
		}
	}

	fmt.Printf("%s  %s\n",
		tui.MutedStyle.Render("→"),
		tui.BrightStyle.Render(shellCmd))

	return runShell(shellCmd)
}

// runShell executes a shell command string, connecting stdin/stdout/stderr directly.
func runShell(shellCmd string) error {
	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.Command("cmd", "/C", shellCmd)
	} else {
		c = exec.Command("sh", "-c", shellCmd)
	}
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		return err
	}
	return nil
}

// readMaskedPassword prompts and reads a password with asterisk masking.
// Falls back to plain read if stdin is not a terminal.
func readMaskedPassword(label string) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	fd := int(syscall.Stdin)
	if term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		if err != nil {
			return "", fmt.Errorf("read password: %w", err)
		}
		return string(b), nil
	}
	var pwd string
	_, err := fmt.Scanln(&pwd)
	return pwd, err
}

// loomConfigPartial is the subset of .loom.config used by loom execute.
type loomConfigPartial struct {
	Custom map[string]string `json:"custom"`
	Locker struct {
		LockHost          string `json:"lockhost"`
		Port              string `json:"port"`
		UnlockDurationSec int    `json:"unlock_duration_seconds"`
	} `json:"loomlocker"`
}

// loadLoomConfig reads .loom.config starting from dir and walking up.
func loadLoomConfig(dir string) (*loomConfigPartial, error) {
	path, err := findLoomConfig(dir)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg loomConfigPartial
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Locker.LockHost == "" {
		cfg.Locker.LockHost = "http://localhost"
	}
	if cfg.Locker.Port == "" {
		cfg.Locker.Port = "8053"
	}
	if cfg.Custom == nil {
		cfg.Custom = map[string]string{}
	}
	return &cfg, nil
}

func findLoomConfig(dir string) (string, error) {
	d := dir
	for {
		candidate := filepath.Join(d, ".loom.config")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf(".loom.config not found (searched from %s upward)", dir)
}
