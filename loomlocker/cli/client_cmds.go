package cli

// client_cmds.go — sub-commands that talk to a running loomlocker server over HTTP.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/sayandeep14/PromptLoom/loomlocker/internal/config"
	"github.com/sayandeep14/PromptLoom/loomlocker/internal/server"
	"github.com/spf13/cobra"
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Lock secrets (no password required)",
	RunE:  runClientLock,
}

var unlockCmd = &cobra.Command{
	Use:   "unlock",
	Short: "Unlock secrets (requires password)",
	RunE:  runClientUnlock,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the loomlocker server (unlocks first if locked)",
	RunE:  runClientStop,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current lock state",
	RunE:  runClientStatus,
}

// rootCmd is the top-level command. Registered in main.go.
var RootCmd = buildRoot()

func buildRoot() *cobra.Command {
	root := &cobra.Command{
		Use:     "loomlocker",
		Short:   "LoomLocker — session-scoped secret protection for loom projects",
		Version: version,
	}
	root.AddCommand(startCmd, lockCmd, unlockCmd, stopCmd, statusCmd, recoverCmd)
	return root
}

// ---- helpers ----

func loadClientConfig() (*config.Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	cfg, _, err := config.Load(cwd)
	return cfg, err
}

func baseURL(cfg *config.Config) string {
	return cfg.Locker.BaseURL() + "/api"
}

func postJSON(url string, body any) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	return client.Post(url, "application/json", bytes.NewReader(data))
}

func getJSON(url string, out any) error {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("server not reachable — is loomlocker running?")
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

func checkRunning(cfg *config.Config) error {
	var ping map[string]any
	if err := getJSON(baseURL(cfg)+"/ping", &ping); err != nil {
		return fmt.Errorf("loomlocker not running on port %s", cfg.Locker.Port)
	}
	return nil
}

// ---- lock ----

func runClientLock(_ *cobra.Command, _ []string) error {
	cfg, err := loadClientConfig()
	if err != nil {
		return err
	}
	if err := checkRunning(cfg); err != nil {
		return err
	}
	resp, err := postJSON(baseURL(cfg)+"/lock", map[string]string{})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	fmt.Printf("✓  locked — %v secret(s)\n", result["count"])
	return nil
}

// ---- unlock ----

func runClientUnlock(_ *cobra.Command, _ []string) error {
	cfg, err := loadClientConfig()
	if err != nil {
		return err
	}
	if err := checkRunning(cfg); err != nil {
		return err
	}
	password, err := ReadPassword("Password")
	if err != nil {
		return err
	}
	resp, err := postJSON(baseURL(cfg)+"/unlock", map[string]string{"password": password})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if errMsg, ok := result["error"].(string); ok {
		return fmt.Errorf("%s", errMsg)
	}
	fmt.Printf("✓  unlocked — auto-relock in %vs\n", result["auto_relock_sec"])
	return nil
}

// ---- stop ----

func runClientStop(_ *cobra.Command, _ []string) error {
	cfg, err := loadClientConfig()
	if err != nil {
		return err
	}
	if err := checkRunning(cfg); err != nil {
		return err
	}
	// Peek at status to see if locked.
	var st server.StatusInfo
	if err := getJSON(baseURL(cfg)+"/status", &st); err != nil {
		return err
	}
	body := map[string]string{}
	if st.Locked {
		password, err := ReadPassword("Password to unlock before stopping")
		if err != nil {
			return err
		}
		body["password"] = password
	}
	resp, err := postJSON(baseURL(cfg)+"/stop", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if errMsg, ok := result["error"].(string); ok {
		return fmt.Errorf("%s", errMsg)
	}
	fmt.Println("✓  loomlocker stopped")
	return nil
}

// ---- status ----

func runClientStatus(_ *cobra.Command, _ []string) error {
	cfg, err := loadClientConfig()
	if err != nil {
		return err
	}
	var st server.StatusInfo
	if err := getJSON(baseURL(cfg)+"/status", &st); err != nil {
		fmt.Printf("  loomlocker not running on port %s\n", cfg.Locker.Port)
		return nil
	}
	if st.Locked {
		fmt.Printf("  state    LOCKED (since %s)\n", st.LockedAt.Format("15:04:05"))
	} else {
		fmt.Println("  state    UNLOCKED")
	}
	fmt.Printf("  secrets  %d key(s)\n", st.SecretCount)
	for _, f := range st.Files {
		fmt.Printf("           • %s\n", f)
	}
	fmt.Printf("  port     %s\n", st.Port)
	return nil
}
