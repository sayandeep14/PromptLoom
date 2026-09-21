package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
)

const Filename = ".loom.config"

// Config is the parsed .loom.config file.
type Config struct {
	Secret     []string          `json:"secret"`
	Ignore     []string          `json:"ignore"`
	Permission Permission        `json:"permission"`
	Locker     LockerConfig      `json:"loomlocker"`
	Custom     map[string]string `json:"custom"`
}

type Permission struct {
	Read  []string `json:"read"`
	Write []string `json:"write"`
}

type LockerConfig struct {
	Active                bool   `json:"active"`
	LockHost              string `json:"lockhost"`
	Port                  string `json:"port"`
	Recoverable           bool   `json:"recoverable"`
	UnlockDurationSeconds int    `json:"unlock_duration_seconds"`
}

// Defaults returns sensible defaults for a LockerConfig.
func (c *LockerConfig) applyDefaults() {
	if c.LockHost == "" {
		c.LockHost = "http://localhost"
	}
	if c.Port == "" {
		c.Port = "8053"
	}
	if c.UnlockDurationSeconds == 0 {
		c.UnlockDurationSeconds = 10
	}
}

// Validate rejects settings that would expose the password or the unlock API:
// the locker is a local service, so lockhost must be a loopback address (a remote
// host would receive the session password in clear text) and the port must be numeric.
func (c *LockerConfig) Validate() error {
	u, err := url.Parse(c.LockHost)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return fmt.Errorf("loomlocker.lockhost %q must look like http://localhost", c.LockHost)
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
	default:
		return fmt.Errorf("loomlocker.lockhost %q is not a loopback address: the session password would be sent over the network. Use http://localhost", c.LockHost)
	}
	if n, err := strconv.Atoi(c.Port); err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("loomlocker.port %q is not a valid port number", c.Port)
	}
	if c.UnlockDurationSeconds < 0 {
		return fmt.Errorf("loomlocker.unlock_duration_seconds must not be negative")
	}
	return nil
}

// BaseURL returns the full base URL for the loomlocker HTTP server.
func (c *LockerConfig) BaseURL() string {
	return fmt.Sprintf("%s:%s", c.LockHost, c.Port)
}

// Load reads .loom.config starting from dir and walking up to find it.
func Load(dir string) (*Config, string, error) {
	path, err := findConfig(dir)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", path, err)
	}
	cfg.Locker.applyDefaults()
	if err := cfg.Locker.Validate(); err != nil {
		return nil, "", fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Custom == nil {
		cfg.Custom = map[string]string{}
	}
	return &cfg, filepath.Dir(path), nil
}

// findConfig walks up from dir to locate .loom.config.
func findConfig(dir string) (string, error) {
	d := dir
	for {
		candidate := filepath.Join(d, Filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("%s not found (searched from %s upward)", Filename, dir)
}

// WriteDefault creates a skeleton .loom.config in dir.
func WriteDefault(dir string) error {
	cfg := Config{
		Secret: []string{".loom.secret"},
		Ignore: []string{},
		Permission: Permission{
			Read:  []string{"*"},
			Write: []string{"*"},
		},
		Locker: LockerConfig{
			Active:                true,
			LockHost:              "http://localhost",
			Port:                  "8053",
			Recoverable:           false,
			UnlockDurationSeconds: 10,
		},
		Custom: map[string]string{},
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, Filename), data, 0o644)
}
