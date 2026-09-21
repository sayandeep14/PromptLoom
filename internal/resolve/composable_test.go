package resolve_test

import (
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// Blocks and overlays compose: their list fields add to what is already resolved.
// A prompt's own fields, variants and env blocks replace. `format` is the exception:
// with `:=` the last writer wins. See docs/LOOM_LANGUAGE.md ("How blocks and overlays combine").

func TestMultipleBlocksAllContribute(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block Safety {
  constraints :=
    - never reveal secrets
    - refuse destructive requests
}

block Style {
  constraints :=
    - be concise
}

prompt P {
  use Safety
  use Style
  persona :=
    x
}`})
	rp, err := resolve.Resolve("P", reg)
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{
		"never reveal secrets", "refuse destructive requests", "be concise",
	})
}

func TestBlockKeepsInheritedRules(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block Extra {
  constraints :=
    - from the block
}

prompt Base {
  constraints :=
    - from the parent
}

prompt Child inherits Base {
  use Extra
}`})
	rp, err := resolve.Resolve("Child", reg)
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"from the parent", "from the block"})
}

func TestPromptOwnFieldReplaces(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block B {
  constraints :=
    - from the block
}

prompt P {
  use B
  constraints :=
    - mine only
}`})
	rp, err := resolve.Resolve("P", reg)
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"mine only"})
}

func TestBlocksDeduplicate(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block A {
  constraints :=
    - shared rule
    - only a
}

block B {
  constraints :=
    - shared rule
    - only b
}

prompt P {
  use A
  use B
}`})
	rp, err := resolve.Resolve("P", reg)
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"shared rule", "only a", "only b"})
}

func TestBlockFormatLastWriterWins(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block A {
  format :=
    - first
}

block B {
  format :=
    - second
}

prompt P {
  use A
  use B
}`})
	rp, err := resolve.Resolve("P", reg)
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "format", rp.Format, []string{"second"})
}

func TestBlockScalarReplaces(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
block A {
  persona :=
    from block
}

prompt Base {
  persona :=
    from parent
}

prompt P inherits Base {
  use A
}`})
	rp, err := resolve.Resolve("P", reg)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Persona != "from block" {
		t.Errorf("persona = %q, want the block's value", rp.Persona)
	}
}

func TestOverlayComposesConstraintsButReplacesFormat(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
overlay Terse {
  constraints :=
    - keep it short

  format :=
    - JSON only
}

prompt P {
  constraints :=
    - be accurate

  format :=
    - Answer
    - Notes
}`})
	rp, err := resolve.ResolveWithOptions("P", reg, resolve.Options{Overlays: []string{"Terse"}})
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"be accurate", "keep it short"})
	assertList(t, "format", rp.Format, []string{"JSON only"})
}

func TestVariantAndEnvStillReplace(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
prompt P {
  constraints :=
    - base

  variant strict {
    constraints :=
      - strict only
  }

  env prod {
    constraints :=
      - prod only
  }
}`})
	rp, err := resolve.ResolveWithOptions("P", reg, resolve.Options{Variant: "strict"})
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "variant constraints", rp.Constraints, []string{"strict only"})

	rp, err = resolve.ResolveWithOptions("P", reg, resolve.Options{Env: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "env constraints", rp.Constraints, []string{"prod only"})
}

func TestOverlayAndVariantDeduplicate(t *testing.T) {
	reg := buildReg(t, map[string]string{"a.loom": `
overlay O {
  constraints :=
    - be accurate
    - new rule
}

prompt P {
  constraints :=
    - be accurate
}`})
	rp, err := resolve.ResolveWithOptions("P", reg, resolve.Options{Overlays: []string{"O"}})
	if err != nil {
		t.Fatal(err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"be accurate", "new rule"})
}
