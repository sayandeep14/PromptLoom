package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const toml = `[project]
name = "t"
version = "0.0.0"
[paths]
prompts  = "prompts"
blocks   = "blocks"
overlays = "overlays"
out      = "dist"
`

func project(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if _, ok := files["loom.toml"]; !ok {
		files["loom.toml"] = toml
	}
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const promptA = "prompt A {\n  persona :=\n    a.\n}\n"

func TestLoadNeedsLoomToml(t *testing.T) {
	_, _, err := Load(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "loom.toml") {
		t.Errorf("got %v", err)
	}
}

func TestLoadEmptyProjectIsFine(t *testing.T) {
	reg, cfg, err := Load(project(t, map[string]string{}))
	if err != nil {
		t.Fatal(err)
	}
	if reg.PromptCount() != 0 || reg.BlockCount() != 0 || cfg.Project.Name != "t" {
		t.Errorf("unexpected: %d prompts, %d blocks", reg.PromptCount(), reg.BlockCount())
	}
}

func TestLoadScansEachDirectoryByKind(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/A.prompt.loom":   promptA,
		"blocks/B.block.loom":     "block B {\n  constraints :=\n    - x\n}\n",
		"overlays/O.overlay.loom": "overlay O {\n  constraints :=\n    - y\n}\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reg.PromptCount() != 1 || reg.BlockCount() != 1 || reg.OverlayCount() != 1 {
		t.Errorf("counts: %d/%d/%d", reg.PromptCount(), reg.BlockCount(), reg.OverlayCount())
	}
	if _, ok := reg.LookupPrompt("A"); !ok {
		t.Error("prompt A missing")
	}
}

func TestLoadIgnoresUnrelatedFiles(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/A.prompt.loom": promptA,
		"prompts/notes.txt":     "not a loom file {{{",
		"prompts/README.md":     "# hello",
		"prompts/.hidden":       "junk",
		"prompts/X.block.loom":  "block X {\n  constraints :=\n    - x\n}\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Non-loom files are ignored. Any *.loom file is "mixed" and may declare any kind,
	// so a .block.loom under prompts/ is still loaded, as a block.
	if reg.PromptCount() != 1 || reg.BlockCount() != 1 {
		t.Errorf("counts: %d prompts, %d blocks", reg.PromptCount(), reg.BlockCount())
	}
}

func TestLoadMixedExtensionFile(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/all.loom": "block B {\n  constraints :=\n    - x\n}\n\nprompt P {\n  use B\n}\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reg.PromptCount() != 1 || reg.BlockCount() != 1 {
		t.Errorf("a .loom file may declare several kinds: %d/%d", reg.PromptCount(), reg.BlockCount())
	}
}

func TestLoadNamespacesSubdirectories(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/team/A.prompt.loom": promptA,
		"prompts/Top.prompt.loom":    "prompt Top {\n  persona :=\n    t.\n}\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.LookupPrompt("team/A"); !ok {
		t.Error(`a prompt in a subdirectory is registered as "team/A"`)
	}
	if _, ok := reg.LookupPrompt("Top"); !ok {
		t.Error("top-level prompt missing")
	}
	// The same name in two different directories does not collide.
	dir = project(t, map[string]string{
		"prompts/one/A.prompt.loom": promptA,
		"prompts/two/A.prompt.loom": promptA,
	})
	if reg, _, err = Load(dir); err != nil || reg.PromptCount() != 2 {
		t.Errorf("namespaced duplicates should coexist: %v", err)
	}
}

func TestLoadDuplicateNamesAreReportedWithBothLocations(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/one.prompt.loom": promptA,
		"prompts/two.prompt.loom": promptA,
	})
	_, _, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), `duplicate prompt name "A"`) ||
		!strings.Contains(err.Error(), "first defined at") {
		t.Errorf("got %v", err)
	}
}

func TestLoadParseErrorNamesFileAndLine(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/Bad.prompt.loom": "prompt Bad {\n  persona :=\n    ok.\n\n  what is this\n}\n",
	})
	_, _, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "Bad.prompt.loom:5") {
		t.Errorf("error must include file:line, got %v", err)
	}
}

func TestLoadGlobalVarsFile(t *testing.T) {
	dir := project(t, map[string]string{
		"prompts/A.prompt.loom": promptA,
		"shared.vars.loom":      "var language = \"Go\"\nslot repo { required: true }\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reg.GlobalVars()); got != 2 {
		t.Errorf("expected 2 global vars, got %d", got)
	}
	// A malformed vars file is an error, with its location.
	dir = project(t, map[string]string{"bad.vars.loom": "this is not a declaration\n"})
	if _, _, err = Load(dir); err == nil || !strings.Contains(err.Error(), "bad.vars.loom:1") {
		t.Errorf("got %v", err)
	}
}

func TestLoadCustomPaths(t *testing.T) {
	dir := project(t, map[string]string{
		"loom.toml":           "[project]\nname=\"x\"\n[paths]\nprompts=\"src/p\"\nblocks=\"src/b\"\noverlays=\"src/o\"\nout=\"o\"\n",
		"src/p/A.prompt.loom": promptA,
	})
	reg, _, err := Load(dir)
	if err != nil || reg.PromptCount() != 1 {
		t.Errorf("custom prompt path: %v", err)
	}
}

func TestLoadSeesInstalledPacks(t *testing.T) {
	dir := project(t, map[string]string{
		"loompack/kit/source/prompts/Base.prompt.loom": "prompt Base {\n  persona :=\n    b.\n}\n",
		"prompts/Mine.prompt.loom":                     "prompt Mine inherits kit.Base {\n  objective :=\n    o.\n}\n",
	})
	reg, _, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := reg.LookupPromptFull("kit.Base", ""); !ok {
		t.Error("an installed pack's prompt must be resolvable as slug.Name")
	}
}

func TestIsLoomSource(t *testing.T) {
	yes := []string{"a.prompt.loom", "dir/b.block.loom", "c.overlay.loom", "d.vars.loom", "e.loom"}
	no := []string{"a.prompt", "b.md", "loom.toml", "c.loom.bak", "prompt.txt", ""}
	for _, p := range yes {
		if !IsLoomSource(p) {
			t.Errorf("%q should be a loom source", p)
		}
	}
	for _, p := range no {
		if IsLoomSource(p) {
			t.Errorf("%q should not be a loom source", p)
		}
	}
}
