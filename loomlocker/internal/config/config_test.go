package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCfg(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"secret": [".env"]}`)
	cfg, workDir, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if workDir != dir {
		t.Errorf("workDir = %s", workDir)
	}
	l := cfg.Locker
	if l.LockHost != "http://localhost" || l.Port != "8053" || l.UnlockDurationSeconds != 10 || l.Recoverable {
		t.Errorf("defaults: %+v", l)
	}
	if cfg.Custom == nil {
		t.Error("Custom must never be nil")
	}
	if cfg.Locker.BaseURL() != "http://localhost:8053" {
		t.Errorf("BaseURL = %s", cfg.Locker.BaseURL())
	}
}

func TestLoadWalksUp(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"secret":[".env"],"loomlocker":{"port":"9999"}}`)
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, workDir, err := Load(sub)
	if err != nil {
		t.Fatal(err)
	}
	if workDir != dir || cfg.Locker.Port != "9999" {
		t.Errorf("workDir=%s port=%s", workDir, cfg.Locker.Port)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), Filename) {
		t.Errorf("missing config: %v", err)
	}
	dir := t.TempDir()
	writeCfg(t, dir, `{ not json`)
	if _, _, err := Load(dir); err == nil || !strings.Contains(err.Error(), Filename) {
		t.Errorf("bad JSON should name the file: %v", err)
	}
}

func TestLockHostMustBeLoopback(t *testing.T) {
	good := []string{"http://localhost", "http://127.0.0.1", "http://[::1]"}
	bad := []string{
		"http://example.com", "http://192.168.1.10", "http://0.0.0.0", "https://localhost",
		"localhost", "ftp://localhost", "http://localhost.evil.com", "http://127.0.0.1.evil.com", "",
	}
	for _, h := range good {
		l := LockerConfig{LockHost: h, Port: "8053"}
		if err := l.Validate(); err != nil {
			t.Errorf("%q should be accepted: %v", h, err)
		}
	}
	for _, h := range bad {
		l := LockerConfig{LockHost: h, Port: "8053"}
		if err := l.Validate(); err == nil {
			t.Errorf("%q must be rejected: the session password would leave the machine", h)
		}
	}
}

func TestLoadRejectsRemoteLockHostAndBadPort(t *testing.T) {
	dir := t.TempDir()
	writeCfg(t, dir, `{"loomlocker":{"lockhost":"http://10.0.0.5"}}`)
	if _, _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("remote lockhost: %v", err)
	}
	writeCfg(t, dir, `{"loomlocker":{"port":"http"}}`)
	if _, _, err := Load(dir); err == nil {
		t.Error("a non-numeric port must be rejected")
	}
	writeCfg(t, dir, `{"loomlocker":{"port":"99999"}}`)
	if _, _, err := Load(dir); err == nil {
		t.Error("an out-of-range port must be rejected")
	}
}

func TestWriteDefaultIsValidAndLoads(t *testing.T) {
	dir := t.TempDir()
	if err := WriteDefault(dir); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Secret) != 1 || cfg.Locker.Recoverable || !cfg.Locker.Active {
		t.Errorf("default config: %+v", cfg)
	}
}
