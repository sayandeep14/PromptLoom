package namespacereg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const promptA = "prompt A {\n  persona :=\n    a.\n}\n"

func TestScanV2Layout(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go-kit/source/prompts/A.prompt.loom", promptA)
	write(t, root, "go-kit/source/blocks/B.block.loom", "block B {\n  constraints :=\n    - x\n}\n")
	write(t, root, "go-kit/source/overlays/O.overlay.loom", "overlay O {\n  constraints :=\n    - y\n}\n")
	nr := Scan(root)

	if !nr.HasPack("go-kit") || nr.HasPack("nope") {
		t.Error("HasPack")
	}
	if _, ok := nr.LookupPrompt("go-kit", "A"); !ok {
		t.Error("prompt")
	}
	if _, ok := nr.LookupBlock("go-kit", "B"); !ok {
		t.Error("block")
	}
	if _, ok := nr.LookupOverlay("go-kit", "O"); !ok {
		t.Error("overlay")
	}
	if _, ok := nr.LookupPrompt("other-pack", "A"); ok {
		t.Error("lookups are per pack")
	}
	if _, ok := nr.LookupPrompt("go-kit", "Missing"); ok {
		t.Error("unknown name")
	}
}

func TestScanLegacyFlatLayout(t *testing.T) {
	root := t.TempDir()
	write(t, root, "old/source/A.prompt.loom", promptA)
	write(t, root, "old/source/B.block.loom", "block B {\n  constraints :=\n    - x\n}\n")
	nr := Scan(root)
	if _, ok := nr.LookupPrompt("old", "A"); !ok {
		t.Error("flat layout prompt")
	}
	if _, ok := nr.LookupBlock("old", "B"); !ok {
		t.Error("flat layout block")
	}
}

func TestScanIsTolerant(t *testing.T) {
	root := t.TempDir()
	write(t, root, "good/source/prompts/A.prompt.loom", promptA)
	write(t, root, "broken/source/prompts/Bad.prompt.loom", "prompt Bad {{{")
	write(t, root, "empty/source/prompts/.keep", "")
	write(t, root, "nosource/readme.txt", "no source dir")
	write(t, root, "stray-file.txt", "not a pack")
	write(t, root, "mixed/source/prompts/Good.prompt.loom", "prompt Good {\n  persona :=\n    g.\n}\n")
	write(t, root, "mixed/source/prompts/Bad.prompt.loom", "prompt Bad {{{")

	nr := Scan(root)
	if got := strings.Join(nr.Slugs(), ","); got != "good,mixed" {
		t.Errorf("slugs = %s (packs that fail to parse or are empty are skipped)", got)
	}
	if _, ok := nr.LookupPrompt("mixed", "Good"); !ok {
		t.Error("one broken file must not hide the pack's good prompts")
	}
	if got := Scan(filepath.Join(root, "does-not-exist")); len(got.Slugs()) != 0 {
		t.Error("a missing loompack directory means no packs")
	}
}

func TestSlugsAreSorted(t *testing.T) {
	root := t.TempDir()
	for _, s := range []string{"zeta", "alpha", "mid", "beta", "omega"} {
		write(t, root, s+"/source/prompts/A.prompt.loom", promptA)
	}
	for i := 0; i < 20; i++ {
		if got := strings.Join(Scan(root).Slugs(), ","); got != "alpha,beta,mid,omega,zeta" {
			t.Fatalf("slugs must be sorted, got %s", got)
		}
	}
}

func TestScanProjectDirPrefersLoomLoompack(t *testing.T) {
	root := t.TempDir()
	write(t, root, "loom/loompack/inner/source/prompts/A.prompt.loom", promptA)
	write(t, root, "loompack/outer/source/prompts/A.prompt.loom", promptA)
	if got := strings.Join(ScanProjectDir(root).Slugs(), ","); got != "inner" {
		t.Errorf("loom/loompack wins: %s", got)
	}
	only := t.TempDir()
	write(t, only, "loompack/outer/source/prompts/A.prompt.loom", promptA)
	if got := strings.Join(ScanProjectDir(only).Slugs(), ","); got != "outer" {
		t.Errorf("fallback to loompack/: %s", got)
	}
	if got := ScanProjectDir(t.TempDir()); len(got.Slugs()) != 0 {
		t.Error("no packs installed")
	}
}
