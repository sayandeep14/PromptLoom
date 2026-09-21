package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeToml(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.Paths.Prompts == "" || c.Paths.Blocks == "" || c.Paths.Overlays == "" || c.Paths.Out == "" {
		t.Errorf("paths: %+v", c.Paths)
	}
	if c.Render.DefaultFormat != "markdown" || c.Validation.MaxInheritanceDepth != 3 {
		t.Errorf("%+v %+v", c.Render, c.Validation)
	}
	// defaults must be independent copies
	c.Paths.Prompts = "changed"
	if Defaults().Paths.Prompts == "changed" {
		t.Error("Defaults must return a fresh value each time")
	}
}

func TestLoadOverridesOnlyWhatIsSet(t *testing.T) {
	dir := t.TempDir()
	writeToml(t, dir, `
[project]
name = "x"

[paths]
prompts = "src/p"

[validation]
max_inheritance_depth = 7

[[targets]]
prompt = "P"
format = "cursor-rule"
dest = ".cursorrules"

[profile.prod]
language = "Go"
`)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	d := Defaults()
	if c.Project.Name != "x" || c.Paths.Prompts != "src/p" || c.Validation.MaxInheritanceDepth != 7 {
		t.Errorf("overrides: %+v", c)
	}
	if c.Paths.Blocks != d.Paths.Blocks || c.Render.DefaultFormat != "markdown" {
		t.Errorf("unset keys must keep their defaults: %+v", c)
	}
	if len(c.Targets) != 1 || c.Targets[0].Format != "cursor-rule" || c.Profiles["prod"]["language"] != "Go" {
		t.Errorf("targets/profiles: %+v %+v", c.Targets, c.Profiles)
	}
}

func TestLoadDerivedSettings(t *testing.T) {
	dir := t.TempDir()
	writeToml(t, dir, "[render]\ninclude_sourcemap = true\ndefault_format = \"\"\n")
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Render.IncludeSourceMap {
		t.Error("include_sourcemap (v2 key) must switch on the source map")
	}
	if c.Render.DefaultFormat != "markdown" {
		t.Errorf("an empty default_format falls back to markdown: %q", c.Render.DefaultFormat)
	}
}

func TestLoadRegistryURL(t *testing.T) {
	dir := t.TempDir()
	writeToml(t, dir, "[registry]\nurl = \"https://r.example\"\n")
	c, err := Load(dir)
	if err != nil || c.Registry.URL != "https://r.example" {
		t.Errorf("%+v %v", c, err)
	}
}

func TestLoadErrors(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil || !strings.Contains(err.Error(), "loom.toml") {
		t.Errorf("missing: %v", err)
	}
	dir := t.TempDir()
	writeToml(t, dir, "[paths\nbroken")
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Errorf("syntax error: %v", err)
	}
}

func TestFindProjectRoot(t *testing.T) {
	root := t.TempDir()
	writeToml(t, root, "")
	deep := filepath.Join(root, "a", "b", "c")
	os.MkdirAll(deep, 0o755)
	got, ok := FindProjectRoot(deep)
	want, _ := filepath.EvalSymlinks(root)
	gotReal, _ := filepath.EvalSymlinks(got)
	if !ok || gotReal != want {
		t.Errorf("walks upward: %s %v", got, ok)
	}
	if got, ok := FindProjectRoot(root); !ok || filepath.Clean(got) != filepath.Clean(root) {
		t.Errorf("at the root: %s %v", got, ok)
	}
}

func TestFindProjectRootFallsBackToASingleExample(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "examples", "demo"), 0o755)
	writeToml(t, filepath.Join(root, "examples", "demo"), "")
	if got, ok := FindProjectRoot(root); !ok || filepath.Base(got) != "demo" {
		t.Errorf("single example: %s %v", got, ok)
	}
	// two candidates are ambiguous: do not guess
	os.MkdirAll(filepath.Join(root, "examples", "other"), 0o755)
	writeToml(t, filepath.Join(root, "examples", "other"), "")
	if _, ok := FindProjectRoot(root); ok {
		t.Error("with several example projects the choice is ambiguous")
	}
	if _, ok := FindProjectRoot(t.TempDir()); ok {
		t.Error("no project anywhere")
	}
}
