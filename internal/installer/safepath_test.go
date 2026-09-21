package installer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRelPath(t *testing.T) {
	for _, p := range []string{"prompts/A.prompt.loom", ".dependency.loom", "loom.toml", "blocks/x.block.loom"} {
		if err := safeRelPath(p); err != nil {
			t.Errorf("%q should be safe: %v", p, err)
		}
	}
	for _, p := range []string{"", "../evil", "a/../../evil", "/etc/passwd", "./a", "a//b", `a\b`, "C:/x", "a\x00b", "a b", "dir/"} {
		if err := safeRelPath(p); err == nil {
			t.Errorf("%q should be rejected", p)
		}
	}
}

func TestValidateBundle(t *testing.T) {
	ok := &Bundle{Slug: "go-backend", Files: []BundleFile{{Path: "prompts/a.prompt.loom"}}}
	if err := validateBundle(ok); err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string]*Bundle{
		"slug traversal": {Slug: "../../x", Files: ok.Files},
		"slug uppercase": {Slug: "Go", Files: ok.Files},
		"path traversal": {Slug: "x", Files: []BundleFile{{Path: "../../.bashrc"}}},
		"duplicate":      {Slug: "x", Files: []BundleFile{{Path: "a.loom"}, {Path: "a.loom"}}},
	} {
		if err := validateBundle(b); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

// A malicious registry must not be able to write outside the pack directory.
func TestInstallRefusesMaliciousBundle(t *testing.T) {
	evil := Bundle{
		Slug: "pwn", Version: "1.0.0", Name: "pwn",
		Files: []BundleFile{{Path: "../../../escaped.txt", FileType: "prompt", Content: "owned"}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(evil)
	}))
	defer srv.Close()

	base := t.TempDir()
	cwd := filepath.Join(base, "a", "b", "c")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOOM_REGISTRY_URL", srv.URL)

	if _, err := Install("pwn", cwd); err == nil {
		t.Fatal("Install must refuse a bundle containing a traversal path")
	}
	if _, err := os.Stat(filepath.Join(base, "escaped.txt")); err == nil {
		t.Fatal("file escaped the pack directory")
	}
	if _, err := os.Stat(filepath.Join(cwd, "loompack", "pwn")); err == nil {
		t.Fatal("nothing should be written for a rejected bundle")
	}
}
