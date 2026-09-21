package cli

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/sayandeep14/PromptLoom/loomlocker/internal/config"
	"github.com/sayandeep14/PromptLoom/loomlocker/internal/repl"
	"github.com/sayandeep14/PromptLoom/loomlocker/internal/server"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the loomlocker server (interactive)",
	Long: `Start the loomlocker server. Asks for a session password, then runs an
interactive REPL while an HTTP server listens for requests from 'loom execute'.

Commands available in the REPL:
  lock    — lock all secrets
  unlock  — restore secrets (re-enter password)
  status  — show current state
  stop    — unlock and exit
  help    — list commands`,
	RunE: runStart,
}

func runStart(_ *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg, workDir, err := config.Load(cwd)
	if err != nil {
		return err
	}

	// Check if a server is already running on this port.
	if isPortOpen(cfg.Locker.Port) {
		return fmt.Errorf("loomlocker is already running on port %s", cfg.Locker.Port)
	}

	// Ask for session password (masked).
	password, err := ReadPassword("Enter session password")
	if err != nil {
		return err
	}
	if password == "" {
		return fmt.Errorf("password cannot be empty")
	}
	confirm, err := ReadPassword("Confirm password")
	if err != nil {
		return err
	}
	if password != confirm {
		return fmt.Errorf("passwords do not match")
	}

	srv, err := server.New(cfg, workDir, password)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}
	if err := srv.Start(); err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	fmt.Printf("✓  loomlocker started on %s (port %s)\n",
		cfg.Locker.LockHost, cfg.Locker.Port)
	fmt.Printf("   unlock window: %ds | recoverable: %v\n",
		cfg.Locker.UnlockDurationSeconds, cfg.Locker.Recoverable)
	fmt.Println()

	// Run the interactive REPL. When stdin closes without "stop" being typed
	// (e.g., terminal closed), the server keeps running until POST /api/stop.
	repl.Run(srv, ReadPassword)
	// Block until the server shuts down (via "stop" command or POST /api/stop).
	<-srv.Done()
	return nil
}

// isPortOpen returns true if something is already listening on the local port.
func isPortOpen(port string) bool {
	conn, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
