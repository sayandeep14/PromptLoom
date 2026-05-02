package sourcemap_test

import (
	"strings"
	"testing"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/sourcemap"
)

func buildReg(t *testing.T, sources map[string]string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for filename, src := range sources {
		nodes, err := parser.Parse(filename, src)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	return reg
}

func mustResolve(t *testing.T, name string, reg *registry.Registry) *ast.ResolvedPrompt {
	t.Helper()
	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return rp
}

func TestBuildSourceMap(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"base.prompt": `
prompt Base {
  persona:
    You are careful.
  constraints:
    - Do not hallucinate APIs.
  format:
    - Summary
}`,
		"rules.prompt": `
block Rules {
  constraints:
    - Check transaction boundaries.
}`,
		"child.prompt": `
prompt Child inherits Base {
  use Rules
  objective:
    Review the code.
}`,
	})
	rp := mustResolve(t, "Child", reg)
	body, err := sourcemap.Build(rp, time.Date(2026, 5, 1, 18, 30, 0, 0, time.UTC), "/repo")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	out := string(body)
	for _, want := range []string{
		`"prompt": "Child"`,
		`"fingerprint": "sha256:`,
		`"rendered_at": "2026-05-01T18:30:00Z"`,
		`"source": "base.prompt"`,
		`"source": "rules.prompt"`,
		`"op": "block"`,
		`"value": "Check transaction boundaries."`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected sourcemap to contain %q, got:\n%s", want, out)
		}
	}
}
