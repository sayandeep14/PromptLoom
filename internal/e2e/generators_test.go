package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/loader"
	"github.com/sayandeep14/PromptLoom/internal/recipe"
	"github.com/sayandeep14/PromptLoom/internal/render"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
	"github.com/sayandeep14/PromptLoom/internal/starter"
	"github.com/sayandeep14/PromptLoom/internal/validate"
	"github.com/sayandeep14/PromptLoom/internal/workspace"
)

// Everything PromptLoom GENERATES for users (recipes, starter libraries, scaffolds)
// must itself be clean v2: no validation errors, no legacy-operator warnings, and
// every prompt must resolve and render. Otherwise a fresh `loom recipe apply` would
// hand the user a project that fails `loom inspect`.

func quietProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "loom.toml"), []byte(quietConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// requireCleanV2 loads dir and fails on any error, any legacy-syntax warning, and
// any prompt that does not resolve. It returns the number of prompts checked.
func requireCleanV2(t *testing.T, dir string) int {
	t.Helper()
	reg, cfg, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("generated project does not load: %v", err)
	}
	for _, d := range validate.Validate(reg, cfg) {
		msg := d.String()
		switch {
		case d.Sev == validate.Error:
			t.Errorf("generated project has a validation error: %s", msg)
		case strings.Contains(d.Message, "uses ':'") || strings.Contains(d.Message, "instead of ':='") ||
			strings.Contains(d.Message, "deprecated"):
			t.Errorf("generated project uses legacy syntax: %s", msg)
		}
	}
	n := 0
	for _, p := range reg.Prompts() {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			t.Errorf("prompt %s does not resolve: %v", p.Name, err)
			continue
		}
		if out := render.Render(rp, cfg); strings.TrimSpace(out) == "" {
			t.Errorf("prompt %s renders empty", p.Name)
		}
		n++
	}
	return n
}

func TestRecipesGenerateCleanV2(t *testing.T) {
	options := []recipe.Options{
		{}, // defaults
		{Language: "Go", Framework: "gin"},
		{Language: "Python", Framework: "fastapi"},
		{Language: "TypeScript", Framework: "express", Style: "graphql"},
		{Language: "Java", Framework: "spring", Style: "rest"},
	}
	for _, r := range recipe.List() {
		for i, opts := range options {
			r, opts := r, opts
			t.Run(fmt.Sprintf("%s/%d", r.Name, i), func(t *testing.T) {
				dir := quietProject(t)
				res, err := recipe.Apply(r.Name, opts, dir, true)
				if err != nil {
					t.Fatal(err)
				}
				if len(res.Written) == 0 {
					t.Fatal("recipe wrote nothing")
				}
				if n := requireCleanV2(t, dir); n == 0 {
					t.Error("recipe produced no prompts")
				}
			})
		}
	}
}

func TestStarterTemplatesGenerateCleanV2(t *testing.T) {
	stacks := []workspace.Stack{
		workspace.StackGo, workspace.StackPython, workspace.StackTypeScript, workspace.StackJavaScript,
		workspace.StackRust, workspace.StackJavaSpring, workspace.StackJava, workspace.StackUnknown,
	}
	tiers := map[string]starter.Tier{"minimal": starter.TierMinimal, "default": starter.TierDefault, "best": starter.TierBest}
	for _, st := range stacks {
		for tierName, tier := range tiers {
			st, tier := st, tier
			t.Run(fmt.Sprintf("%s/%s", st, tierName), func(t *testing.T) {
				dir := quietProject(t)
				info := &workspace.Info{Stack: st, Language: string(st), Framework: "web", BuildTool: "make", TestFramework: "unit", Dir: dir}
				plan := starter.TemplatesForStack(info, tier)
				if len(plan.Files) == 0 {
					t.Fatal("no files planned")
				}
				cfg, err := config.Load(dir)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := starter.Write(plan, cfg, dir); err != nil {
					t.Fatal(err)
				}
				requireCleanV2(t, dir)
			})
		}
	}
}

func TestInitSampleAndScaffoldsAreCleanV2(t *testing.T) {
	bin := buildLoom(t)
	dir := t.TempDir()

	if out, code := runLoom(t, bin, dir, "init", "--sample"); code != 0 {
		t.Fatalf("init --sample exit %d\n%s", code, out)
	}
	for _, args := range [][]string{
		{"thread", "prompt", "Extra"},
		{"thread", "block", "ExtraRules"},
		{"thread", "overlay", "ExtraOverlay"},
	} {
		if out, code := runLoom(t, bin, dir, args...); code != 0 {
			t.Fatalf("loom %v exit %d\n%s", args, code, out)
		}
	}

	if out, code := runLoom(t, bin, dir, "inspect"); code != 0 {
		t.Fatalf("inspect of a freshly generated project failed (exit %d):\n%s", code, out)
	} else if strings.Contains(out, "deprecated") || strings.Contains(out, "uses ':'") {
		t.Errorf("fresh project mentions legacy syntax:\n%s", out)
	}
	requireCleanV2(t, dir)
}
