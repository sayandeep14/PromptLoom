package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
	"github.com/sayandeep14/PromptLoom/internal/validate"
)

const loomToml = `[project]
name = "t"
version = "0.0.0"

[paths]
prompts  = "prompts"
blocks   = "blocks"
overlays = "overlays"
out      = "dist"

[validation]
require_objective        = false
require_format           = false
require_contract         = false
warn_on_empty_context    = false
warn_on_deep_inheritance = false
`

func project(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(loomToml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

var optionSets = []Options{
	{},
	{Language: "Go"},
	{Language: "go", Framework: "gin"},
	{Language: "TypeScript", Framework: "next-js"},
	{Language: "C++"},
	{Language: "C#", Framework: ".NET 8"},
	{Language: "my lang_v2", Framework: "spring boot", Style: "graphql"},
	{Language: "日本語"},
	{Language: "1st language"},
}

// Every recipe, with every kind of option value, must produce a library that loads, validates
// without errors, and resolves and renders every prompt.
func TestEveryRecipeProducesAValidLibrary(t *testing.T) {
	for _, r := range List() {
		for _, opts := range optionSets {
			dir := project(t)
			res, err := Apply(r.Name, opts, dir, false)
			if err != nil {
				t.Fatalf("%s %+v: %v", r.Name, opts, err)
			}
			if len(res.Written) != len(r.Files) || len(res.Skipped) != 0 {
				t.Errorf("%s: %+v", r.Name, res)
			}
			reg, cfg, err := loader.Load(dir)
			if err != nil {
				t.Errorf("%s %+v does not load: %v", r.Name, opts, err)
				continue
			}
			for _, d := range validate.Validate(reg, cfg) {
				if d.Sev == validate.Error {
					t.Errorf("%s %+v: %s", r.Name, opts, d.Message)
				}
			}
			for _, p := range reg.Prompts() {
				rp, err := resolve.ResolveWithOptions(p.Name, reg, resolve.Options{Variables: map[string]string{"repo_name": "demo"}})
				if err != nil {
					t.Errorf("%s %+v: resolve %s: %v", r.Name, opts, p.Name, err)
					continue
				}
				if strings.TrimSpace(render.Render(rp, cfg)) == "" {
					t.Errorf("%s %s renders empty", r.Name, p.Name)
				}
			}
		}
	}
}

// The language and framework end up in file names and DSL source: hostile values must not be
// able to escape the project, add files elsewhere or inject prompts.
func TestHostileOptionsCannotEscapeOrInject(t *testing.T) {
	dir := project(t)
	parent := filepath.Dir(dir)
	before, _ := os.ReadDir(parent)
	hostile := Options{
		Language:  "../../evil\n}\nprompt Injected {\n  persona :=\n    pwned",
		Framework: "..\\..\\x/../../y",
	}
	if _, err := Apply("reviewer", hostile, dir, false); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadDir(parent); len(after) != len(before) {
		t.Errorf("files were written outside the project")
	}
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if rel, _ := filepath.Rel(dir, p); err == nil && !info.IsDir() && rel != "loom.toml" {
			if !(strings.HasPrefix(rel, "prompts"+string(filepath.Separator)) || strings.HasPrefix(rel, "blocks"+string(filepath.Separator))) || strings.Contains(rel, "..") {
				t.Errorf("unexpected file %s", rel)
			}
		}
		return nil
	})
	reg, _, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("hostile input must still yield a loadable library: %v", err)
	}
	for _, p := range reg.Prompts() {
		if p.Name == "Injected" {
			t.Error("a prompt was injected through --language")
		}
	}
}

func TestSubstitutionIsDeterministicAndDoesNotRecurse(t *testing.T) {
	// a value that itself looks like a placeholder must be inserted literally, in every run
	opts := Options{Language: "{{Style}}", Style: "graphql"}
	first := substitute("L={{Language}} S={{Style}}", buildVars(opts))
	for i := 0; i < 50; i++ {
		if got := substitute("L={{Language}} S={{Style}}", buildVars(opts)); got != first {
			t.Fatalf("substitution depends on map order: %q vs %q", got, first)
		}
	}
	if first != "L={{Style}} S=graphql" {
		t.Errorf("%q", first)
	}
	// unknown placeholders (slots such as {{repo_name}}) are left alone
	if got := substitute("{{repo_name}}", buildVars(Options{})); got != "{{repo_name}}" {
		t.Errorf("%q", got)
	}
}

func TestApplySkipsExistingFilesUnlessForced(t *testing.T) {
	dir := project(t)
	if _, err := Apply("reviewer", Options{Language: "Go"}, dir, false); err != nil {
		t.Fatal(err)
	}
	mine := filepath.Join(dir, "prompts", "BaseEngineer.prompt.loom")
	os.WriteFile(mine, []byte("my edits"), 0o644)

	res, err := Apply("reviewer", Options{Language: "Go"}, dir, false)
	if err != nil || len(res.Written) != 0 || len(res.Skipped) == 0 {
		t.Errorf("%+v %v", res, err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "my edits" {
		t.Error("an existing file was overwritten without --force")
	}
	res, _ = Apply("reviewer", Options{Language: "Go"}, dir, true)
	if len(res.Skipped) != 0 || len(res.Written) == 0 {
		t.Errorf("%+v", res)
	}
	if b, _ := os.ReadFile(mine); string(b) == "my edits" {
		t.Error("--force must overwrite")
	}
}

func TestGetAndList(t *testing.T) {
	if len(List()) < 5 {
		t.Fatal("expected the built-in recipes")
	}
	seen := map[string]bool{}
	for _, r := range List() {
		if r.Name == "" || r.Description == "" || len(r.Files) == 0 || seen[r.Name] {
			t.Errorf("bad recipe %+v", r.Name)
		}
		seen[r.Name] = true
		if got, ok := Get(strings.ToUpper(r.Name)); !ok || got.Name != r.Name {
			t.Errorf("Get is case-insensitive: %s", r.Name)
		}
		paths := map[string]bool{}
		for _, f := range r.Files {
			if paths[f.RelPath] {
				t.Errorf("%s lists %s twice", r.Name, f.RelPath)
			}
			paths[f.RelPath] = true
		}
	}
	if _, ok := Get("nope"); ok {
		t.Error("unknown recipe")
	}
	if _, err := Apply("nope", Options{}, t.TempDir(), false); err == nil || !strings.Contains(err.Error(), "loom recipe list") {
		t.Errorf("%v", err)
	}
}

func TestNameHelpers(t *testing.T) {
	for in, want := range map[string]string{"go": "Go", "next-js": "NextJs", "my_lang v2": "MyLangV2", "": "", "C++": "C", "日本語": "日本語", "../x": "X"} {
		if got := toPascal(in); got != want {
			t.Errorf("toPascal(%q) = %q, want %q", in, got, want)
		}
	}
	if titleCase("spring boot") != "Spring Boot" || titleCase("") != "" {
		t.Error("titleCase")
	}
}
