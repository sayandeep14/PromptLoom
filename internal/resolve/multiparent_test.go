package resolve_test

import (
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

func buildReg(t *testing.T, srcs map[string]string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for filename, src := range srcs {
		nodes, err := parser.Parse(filename, src)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatalf("register %s: %v", filename, err)
		}
	}
	return reg
}

// ---- from(parent[*]) — list merge across all parents ----

func TestFromParentAllMergesList(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  instructions:
    - step A1
    - step A2
}`,
		"b.loom": `
prompt B {
  instructions:
    - step B1
    - step B2
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions := from(parent[*])
}`,
	})

	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertList(t, "instructions", rp.Instructions, []string{
		"step A1", "step A2", "step B1", "step B2",
	})
}

// ---- from(parent[0]) — scalar copy from first parent ----

func TestFromParentIndexScalar(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona:
    You are an expert in Go.
}`,
		"b.loom": `
prompt B {
  persona:
    You are an expert in Python.
}`,
		"c.loom": `
prompt C inherits A, B {
  persona := from(parent[0])
}`,
	})

	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rp.Persona != "You are an expert in Go." {
		t.Errorf("expected Go persona, got %q", rp.Persona)
	}
}

// ---- from(parent[*]) and { literal items } ----

func TestFromParentAllAndLiteral(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"base.loom": `
prompt Base {
  instructions:
    - inherited step
}`,
		"child.loom": `
prompt Child inherits Base {
  instructions := from(parent[*]) and {
    - extra step
  }
}`,
	})

	rp, err := resolve.Resolve("Child", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertList(t, "instructions", rp.Instructions, []string{
		"inherited step",
		"extra step",
	})
}

// ---- parent[N].field[sub] — explicit field subscript ----

func TestFromFieldRefSlice(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  instructions:
    - step 0
    - step 1
    - step 2
    - step 3
    - step 4
}`,
		"b.loom": `
prompt B {
  instructions:
    - step B0
    - step B1
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions := parent[0].instructions[1..3] and parent[1].instructions[*]
}`,
	})

	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// parent[0].instructions[1..3] = ["step 1", "step 2"]
	// parent[1].instructions[*] = ["step B0", "step B1"]
	assertList(t, "instructions", rp.Instructions, []string{
		"step 1", "step 2", "step B0", "step B1",
	})
}

// ---- first-parent-wins with warning when both parents define a field ----

func TestMultiParentConflictFirstWins(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  objective:
    Objective from A.
}`,
		"b.loom": `
prompt B {
  objective:
    Objective from B.
}`,
		"c.loom": `
prompt C inherits A, B {
}`,
	})

	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A is parent[0], so A's objective wins.
	if rp.Objective != "Objective from A." {
		t.Errorf("expected objective from A, got %q", rp.Objective)
	}
	// A warning should be recorded.
	found := false
	for _, w := range rp.Warnings {
		if strings.Contains(w, "objective") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected conflict warning for 'objective', got: %v", rp.Warnings)
	}
}

// ---- deduplication of list items ----

func TestDeduplicateListItems(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  constraints:
    - No hallucination.
    - Be concise.
}`,
		"b.loom": `
prompt B {
  constraints:
    - No hallucination.
    - Be precise.
}`,
		"c.loom": `
prompt C inherits A, B {
  constraints := from(parent[0]) and from(parent[1])
}`,
	})

	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// "No hallucination." appears in both parents — should be deduplicated.
	assertList(t, "constraints", rp.Constraints, []string{
		"No hallucination.", "Be concise.", "Be precise.",
	})
}

// ---- InheritsChain for multi-parent ----

func TestMultiParentInheritsChain(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  objective:
    obj.
}`,
		"b.loom": `
prompt B {
  objective:
    obj.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions := from(parent[*])
}`,
	})
	rp, err := resolve.Resolve("C", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Chain should include A, B, and C.
	if len(rp.InheritsChain) != 3 {
		t.Fatalf("expected chain length 3, got %d: %v", len(rp.InheritsChain), rp.InheritsChain)
	}
	if rp.InheritsChain[2] != "C" {
		t.Errorf("last in chain should be C, got %q", rp.InheritsChain[2])
	}
}

// ---- type error: from(parent[*]) on scalar ----

func TestFromParentAllOnScalarIsTypeError(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona:
    some persona.
}`,
		"b.loom": `
prompt B inherits A {
  persona := from(parent[*])
}`,
	})
	_, err := resolve.Resolve("B", reg)
	if err == nil {
		t.Fatal("expected type error for from(parent[*]) on scalar field")
	}
	if !strings.Contains(err.Error(), "scalar") {
		t.Errorf("expected 'scalar' in error message, got: %v", err)
	}
}

// ---- cycle detection with multi-parent ----

func TestMultiParentCycleDetection(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits C {
  objective:
    o.
}`,
		"b.loom": `
prompt B {
  objective:
    o.
}`,
		"c.loom": `
prompt C inherits A, B {
  objective:
    o.
}`,
	})
	_, err := resolve.Resolve("C", reg)
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("expected 'cycle' in error, got: %v", err)
	}
}

// ---- deep single-parent chain still works ----

func TestDeepSingleParentChain(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"root.loom": `
prompt Root {
  instructions:
    - root step
}`,
		"mid.loom": `
prompt Mid inherits Root {
  instructions +=
    - mid step
}`,
		"leaf.loom": `
prompt Leaf inherits Mid {
  instructions +=
    - leaf step
}`,
	})
	rp, err := resolve.Resolve("Leaf", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertList(t, "instructions", rp.Instructions, []string{
		"root step", "mid step", "leaf step",
	})
	assertChain(t, rp, []string{"Root", "Mid", "Leaf"})
}
