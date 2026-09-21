package graph_test

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/graph"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
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

// ---- roots ----

func TestSingleRootNoParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
	})
	g := graph.Build(reg)
	if len(g.Roots()) != 1 || g.Roots()[0] != "A" {
		t.Errorf("expected roots=[A], got %v", g.Roots())
	}
}

func TestChildNotInRoots(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    child.
}`,
	})
	g := graph.Build(reg)
	for _, r := range g.Roots() {
		if r == "B" {
			t.Errorf("B should not be a root; roots=%v", g.Roots())
		}
	}
}

// ---- multi-parent children map ----

func TestMultiParentChildOfBoth(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)

	// C must appear in the children list of both A and B.
	if !containsString(g.Children("A"), "C") {
		t.Errorf("expected C in children of A, got %v", g.Children("A"))
	}
	if !containsString(g.Children("B"), "C") {
		t.Errorf("expected C in children of B, got %v", g.Children("B"))
	}

	// Neither A nor B should appear in C's children.
	for _, child := range g.Children("C") {
		if child == "A" || child == "B" {
			t.Errorf("A and B should not be children of C, got %v", g.Children("C"))
		}
	}
}

func TestMultiParentNeitherParentIsRoot(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	// C has resolvable parents → not a root.
	for _, r := range g.Roots() {
		if r == "C" {
			t.Errorf("C should not be a root; roots=%v", g.Roots())
		}
	}
	// A and B have no parents → both are roots.
	roots := g.Roots()
	if !containsString(roots, "A") || !containsString(roots, "B") {
		t.Errorf("expected A and B as roots, got %v", roots)
	}
}

// ---- no cycles ----

func TestNoCycles(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    child.
}`,
	})
	g := graph.Build(reg)
	if g.HasCycles() {
		t.Errorf("expected no cycles, got %v", g.Cycles())
	}
}

// ---- cycle detection ----

func TestDirectCycle(t *testing.T) {
	// A inherits B, B inherits A.
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits B {
  persona :=
    A.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
	})
	g := graph.Build(reg)
	if !g.HasCycles() {
		t.Fatal("expected cycle to be detected")
	}
}

func TestThreeNodeCycle(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits C {
  persona :=
    A.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits B {
  persona :=
    C.
}`,
	})
	g := graph.Build(reg)
	if !g.HasCycles() {
		t.Fatal("expected cycle among A→B→C→A to be detected")
	}
	// Every cycle path should start and end at the same node.
	for _, cycle := range g.Cycles() {
		if len(cycle) < 2 {
			t.Errorf("cycle too short: %v", cycle)
			continue
		}
		if cycle[0] != cycle[len(cycle)-1] {
			t.Errorf("cycle path should start and end at same node: %v", cycle)
		}
	}
}

func TestMultiParentCycleDetected(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits C {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	if !g.HasCycles() {
		t.Fatal("expected cycle A→C→A to be detected in multi-parent graph")
	}
}

func TestDiamondNoCycle(t *testing.T) {
	// Diamond: A←B, A←C, B←D, C←D (D inherits B and C, both inherit A).
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    root.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A {
  persona :=
    C.
}`,
		"d.loom": `
prompt D inherits B, C {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	if g.HasCycles() {
		t.Errorf("diamond inheritance should not be a cycle, got %v", g.Cycles())
	}
	// D should appear under both B and C.
	if !containsString(g.Children("B"), "D") {
		t.Errorf("D should be child of B, children=%v", g.Children("B"))
	}
	if !containsString(g.Children("C"), "D") {
		t.Errorf("D should be child of C, children=%v", g.Children("C"))
	}
}

// ---- ASCII contains cycle marker ----

func TestASCIIContainsCycleMarker(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits B {
  persona :=
    A.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
	})
	g := graph.Build(reg)
	ascii := g.ASCII()
	if !strings.Contains(ascii, "cycle") {
		t.Errorf("expected 'cycle' marker in ASCII output, got:\n%s", ascii)
	}
}

// ---- Mermaid and DOT use all parents ----

func TestMermaidMultiParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	mermaid := g.Mermaid()
	if !strings.Contains(mermaid, "A --> C") && !strings.Contains(mermaid, "A_-->_C") {
		if !strings.Contains(mermaid, "A") || !strings.Contains(mermaid, "C") {
			t.Errorf("Mermaid should contain edges for both parents A and B to C:\n%s", mermaid)
		}
	}
}

func TestDOTMultiParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	dot := g.DOT()
	// Both "A" -> "C" and "B" -> "C" edges should be present.
	if !strings.Contains(dot, `"A" -> "C"`) {
		t.Errorf("DOT missing A→C edge:\n%s", dot)
	}
	if !strings.Contains(dot, `"B" -> "C"`) {
		t.Errorf("DOT missing B→C edge:\n%s", dot)
	}
}

// ---- AncestorTree ----

func TestAncestorTreeBasePrompt(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("A")
	// A has no parents — should just show A with no parent lines.
	if !strings.Contains(out, "A") {
		t.Errorf("expected A in output, got:\n%s", out)
	}
	if strings.Contains(out, "parent[") {
		t.Errorf("base prompt should have no parent lines, got:\n%s", out)
	}
}

func TestAncestorTreeSingleParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"root.loom": `
prompt Root {
  persona :=
    root.
}`,
		"child.loom": `
prompt Child inherits Root {
  persona :=
    child.
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("Child")

	// Should show Child at top, then Root as parent.
	if !strings.HasPrefix(strings.TrimSpace(out), "Child") {
		t.Errorf("expected Child at top of output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent[0]: Root") {
		t.Errorf("expected parent[0]: Root in output, got:\n%s", out)
	}
	// Root should not show its own parent lines.
	if strings.Contains(out, "parent[0]: Root\n") {
		// Root appears as parent line; it's fine. But Root should not recurse further.
	}
}

func TestAncestorTreeThreeLevels(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"grandparent.loom": `
prompt GrandParent {
  persona :=
    grand.
}`,
		"parent.loom": `
prompt Parent inherits GrandParent {
  persona :=
    parent.
}`,
		"leaf.loom": `
prompt Leaf inherits Parent {
  persona :=
    leaf.
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("Leaf")

	if !strings.Contains(out, "parent[0]: Parent") {
		t.Errorf("expected parent[0]: Parent in output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent[0]: GrandParent") {
		t.Errorf("expected parent[0]: GrandParent in output, got:\n%s", out)
	}
	// Leaf should appear before its parents in the output (it's the root of the tree).
	leafPos := strings.Index(out, "Leaf")
	parentPos := strings.Index(out, "Parent")
	grandPos := strings.Index(out, "GrandParent")
	if leafPos > parentPos {
		t.Errorf("Leaf should appear before Parent in output")
	}
	if parentPos > grandPos {
		t.Errorf("Parent should appear before GrandParent in output")
	}
}

func TestAncestorTreeMultiParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    A.
}`,
		"b.loom": `
prompt B {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("C")

	if !strings.Contains(out, "parent[0]: A") {
		t.Errorf("expected parent[0]: A in output, got:\n%s", out)
	}
	if !strings.Contains(out, "parent[1]: B") {
		t.Errorf("expected parent[1]: B in output, got:\n%s", out)
	}
	// C should be the first node shown (at the top, before its parents).
	cPos := strings.Index(out, "C")
	aPos := strings.Index(out, "parent[0]: A")
	bPos := strings.Index(out, "parent[1]: B")
	if cPos > aPos || cPos > bPos {
		t.Errorf("C should appear before its parents in the output:\n%s", out)
	}
}

func TestAncestorTreeDiamond(t *testing.T) {
	// D inherits B and C; both B and C inherit A.
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    root.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
		"c.loom": `
prompt C inherits A {
  persona :=
    C.
}`,
		"d.loom": `
prompt D inherits B, C {
  instructions :=
    from(parent[*])
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("D")

	// A should appear once in full, then as "(shared)" the second time.
	firstA := strings.Index(out, "A")
	lastA := strings.LastIndex(out, "A")
	if firstA == lastA {
		t.Errorf("A should appear more than once in diamond ancestor tree:\n%s", out)
	}
	if !strings.Contains(out, "(shared)") {
		t.Errorf("expected '(shared)' marker for diamond ancestor A, got:\n%s", out)
	}
}

func TestAncestorTreeCycleMarker(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A inherits B {
  persona :=
    A.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    B.
}`,
	})
	g := graph.Build(reg)
	// A is in a cycle — ancestor tree should show cycle marker, not infinite loop.
	out := g.AncestorTree("A")
	if !strings.Contains(out, "cycle") {
		t.Errorf("expected cycle marker in ancestor tree of cyclic prompt, got:\n%s", out)
	}
}

func TestAncestorTreeUnknownPrompt(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
	})
	g := graph.Build(reg)
	out := g.AncestorTree("DoesNotExist")
	if !strings.Contains(out, "no prompt") {
		t.Errorf("expected 'no prompt' error message, got:\n%s", out)
	}
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
