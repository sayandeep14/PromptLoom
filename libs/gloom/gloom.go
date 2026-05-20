// Package gloom is the Go client library for LoomLocker.
//
// It provides the Safe type for safely loading secrets in applications
// protected by loomlocker.
//
// Basic usage:
//
//	import "github.com/sayandeepgiri/promptloom/libs/gloom"
//
//	func main() {
//	    gloom.NewSafe().Unlock().Execute(func() {
//	        godotenv.Load()
//	    }).Autolock()
//
//	    server.Start()
//	}
//
// If loomlocker is not running, all operations are transparent no-ops —
// Execute always runs.
package gloom

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Safe is the fluent API for safely loading secrets.
type Safe struct {
	cfg      *Config
	client   *Client
	unlocked bool
	logger   *log.Logger
}

// NewSafe creates a Safe with config auto-detected from .loom.config or env vars.
func NewSafe() *Safe {
	cfg := ConfigFromEnv()
	return &Safe{
		cfg:    cfg,
		client: NewClient(cfg),
		logger: log.New(log.Writer(), "[gloom] ", 0),
	}
}

// WithConfig creates a Safe with explicit config.
func WithConfig(cfg *Config) *Safe {
	return &Safe{
		cfg:    cfg,
		client: NewClient(cfg),
		logger: log.New(log.Writer(), "[gloom] ", 0),
	}
}

// Silent disables all log output from gloom.
func (s *Safe) Silent() *Safe {
	s.logger = log.New(nopWriter{}, "", 0)
	return s
}

// ── Fluent API ────────────────────────────────────────────────────────────────

// Unlock unlocks secrets if loomlocker is running and they are locked.
// Password precedence: argument → LOOM_SESSION_PASSWORD env var → skip.
func (s *Safe) Unlock(password ...string) *Safe {
	if !s.client.IsRunning() {
		return s
	}
	if !s.client.IsLocked() {
		s.unlocked = true
		return s
	}
	pwd := ""
	if len(password) > 0 {
		pwd = password[0]
	}
	if pwd == "" {
		pwd = os.Getenv("LOOM_SESSION_PASSWORD")
	}
	if pwd == "" {
		s.logger.Println("secrets are locked but no password provided — set LOOM_SESSION_PASSWORD")
		return s
	}
	if err := s.client.Unlock(pwd); err != nil {
		s.logger.Printf("unlock failed: %v", err)
		return s
	}
	s.unlocked = true
	return s
}

// Execute runs fn and returns Safe for chaining. Always called regardless of lock state.
func (s *Safe) Execute(fn func()) *Safe {
	fn()
	return s
}

// Autolock signals loomlocker that startup is complete → immediate relock.
// No-op if loomlocker is not running.
func (s *Safe) Autolock() *Safe {
	if s.unlocked && s.client.IsRunning() {
		if err := s.client.Autolock(); err != nil {
			s.logger.Printf("autolock failed: %v", err)
		}
		s.unlocked = false
	}
	return s
}

// ── Config ────────────────────────────────────────────────────────────────────

// Config holds connection settings for the loomlocker server.
type Config struct {
	Host string
	Port string
}

// BaseURL returns the full API base URL.
func (c *Config) BaseURL() string {
	return fmt.Sprintf("%s:%s/api", c.Host, c.Port)
}

// ConfigFromEnv loads config from env vars, falling back to .loom.config file.
func ConfigFromEnv() *Config {
	cfg := configFromFile()
	if cfg == nil {
		cfg = &Config{Host: "http://localhost", Port: "8053"}
	}
	if h := os.Getenv("LOOM_HOST"); h != "" {
		cfg.Host = h
	}
	if p := os.Getenv("LOOM_PORT"); p != "" {
		cfg.Port = p
	}
	return cfg
}

func configFromFile() *Config {
	path := findLoomConfig()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw struct {
		Locker struct {
			LockHost string `json:"lockhost"`
			Port     string `json:"port"`
		} `json:"loomlocker"`
	}
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	cfg := &Config{
		Host: raw.Locker.LockHost,
		Port: raw.Locker.Port,
	}
	if cfg.Host == "" {
		cfg.Host = "http://localhost"
	}
	if cfg.Port == "" {
		cfg.Port = "8053"
	}
	return cfg
}

func findLoomConfig() string {
	d, _ := os.Getwd()
	for {
		candidate := filepath.Join(d, ".loom.config")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// ── Client ────────────────────────────────────────────────────────────────────

// Client is the HTTP client for the loomlocker server.
type Client struct {
	cfg  *Config
	http *http.Client
}

// NewClient creates a Client. All operations silently fail if the server is unreachable.
func NewClient(cfg *Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 4 * time.Second},
	}
}

// IsRunning returns true if the loomlocker server responds.
func (c *Client) IsRunning() bool {
	resp, err := c.http.Get(c.cfg.BaseURL() + "/ping")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsLocked returns the current lock state.
func (c *Client) IsLocked() bool {
	resp, err := c.http.Get(c.cfg.BaseURL() + "/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct {
		Locked bool `json:"locked"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return body.Locked
}

// Unlock sends a password to unlock secrets.
func (c *Client) Unlock(password string) error {
	body, _ := json.Marshal(map[string]string{"password": password})
	resp, err := c.http.Post(c.cfg.BaseURL()+"/unlock", "application/json",
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("invalid password")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return nil
}

// Autolock triggers an immediate relock on the server.
func (c *Client) Autolock() error {
	resp, err := c.http.Post(c.cfg.BaseURL()+"/autolock", "application/json",
		bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ── internal ──────────────────────────────────────────────────────────────────

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
