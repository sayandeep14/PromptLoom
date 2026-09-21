package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/installer"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveRegistryHasNoDefault(t *testing.T) {
	t.Setenv("LOOM_REGISTRY_URL", "")
	url, src := resolveRegistryURL(t.TempDir())
	if url != "" || src != "" {
		t.Fatalf("expected no registry, got %q (%s)", url, src)
	}
}

func TestResolveRegistryPrecedence(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "loom.toml"), "[registry]\nurl = \"https://toml.example\"\n")

	t.Setenv("LOOM_REGISTRY_URL", "")
	if url, _ := resolveRegistryURL(dir); url != "https://toml.example" {
		t.Errorf("loom.toml: got %q", url)
	}

	write(t, filepath.Join(dir, "loom", ".loom.env"), "LOOM_REGISTRY_URL=https://envfile.example\n")
	if url, _ := resolveRegistryURL(dir); url != "https://envfile.example" {
		t.Errorf(".loom.env should beat loom.toml: got %q", url)
	}

	t.Setenv("LOOM_REGISTRY_URL", "https://shell.example")
	if url, src := resolveRegistryURL(dir); url != "https://shell.example" || src != "$LOOM_REGISTRY_URL" {
		t.Errorf("shell env should win: got %q (%s)", url, src)
	}
}

func TestCheckRegistryURL(t *testing.T) {
	for _, u := range []string{"https://r.example", "http://localhost:8080", "https://r.example/base"} {
		if err := checkRegistryURL(u); err != nil {
			t.Errorf("%q should be valid: %v", u, err)
		}
	}
	for _, u := range []string{"", "registry.example", "ftp://r.example", "file:///etc", "javascript:alert(1)", "http://"} {
		if err := checkRegistryURL(u); err == nil {
			t.Errorf("%q should be rejected", u)
		}
	}
}

func TestIsPlainHTTPRemote(t *testing.T) {
	cases := map[string]bool{
		"http://registry.example":  true,
		"http://10.0.0.5:8080":     true,
		"https://registry.example": false,
		"http://localhost:8080":    false,
		"http://127.0.0.1:8080":    false,
		"http://[::1]:8080":        false,
	}
	for u, want := range cases {
		if got := isPlainHTTPRemote(u); got != want {
			t.Errorf("%s: got %v, want %v", u, got, want)
		}
	}
}

func TestNoRegistryMessageIsActionable(t *testing.T) {
	msg := errNoRegistry().Error()
	for _, want := range []string{"--registry", "LOOM_REGISTRY_URL", "[registry]", "localhost:8080"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
}

func TestInstallerRefusesWithoutRegistry(t *testing.T) {
	t.Setenv("LOOM_REGISTRY_URL", "")
	if _, err := installer.Install("anything", t.TempDir()); !errors.Is(err, installer.ErrNoRegistry) {
		t.Fatalf("got %v, want ErrNoRegistry", err)
	}
}
