package parser_test

import (
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// ---- multiple inheritance ----

func TestMultipleInherits(t *testing.T) {
	src := `
prompt Combined inherits ReviewerA, ReviewerB {
  instructions :=
    - Shared step.
}
`
	nodes := mustParse(t, "combined.loom", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if len(n.Parents) != 2 {
		t.Fatalf("expected 2 parents, got %d: %v", len(n.Parents), n.Parents)
	}
	if n.Parents[0] != "ReviewerA" || n.Parents[1] != "ReviewerB" {
		t.Errorf("unexpected parents: %v", n.Parents)
	}
	if n.Parent != "ReviewerA" {
		t.Errorf("Parent backward-compat field should be %q, got %q", "ReviewerA", n.Parent)
	}
}

func TestThreeParents(t *testing.T) {
	src := `
prompt Triple inherits A, B, C {
}
`
	nodes := mustParse(t, "triple.loom", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if len(n.Parents) != 3 || n.Parents[0] != "A" || n.Parents[1] != "B" || n.Parents[2] != "C" {
		t.Errorf("unexpected parents: %v", n.Parents)
	}
}

func TestNamespacedInherits(t *testing.T) {
	src := `
prompt MyPrompt inherits go-backend.GoCodeReviewer {
}
`
	nodes := mustParse(t, "ns.loom", src)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if len(n.Parents) != 1 || n.Parents[0] != "go-backend.GoCodeReviewer" {
		t.Errorf("unexpected parents: %v", n.Parents)
	}
	if n.Parent != "go-backend.GoCodeReviewer" {
		t.Errorf("backward compat Parent: got %q", n.Parent)
	}
}

// ---- from() expression parsing ----

func TestFromParentAll(t *testing.T) {
	src := `
prompt Child inherits Base {
  instructions := from(parent[*])
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["instructions"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr to be set")
	}
	if len(field.FromExpr.Units) != 1 {
		t.Fatalf("expected 1 unit, got %d", len(field.FromExpr.Units))
	}
	u := field.FromExpr.Units[0]
	if u.Kind != ast.FromParentRef {
		t.Errorf("expected FromParentRef, got %v", u.Kind)
	}
	if u.ParentSub.Kind != ast.SubAll {
		t.Errorf("expected SubAll subscript, got %v", u.ParentSub.Kind)
	}
}

func TestFromParentIndex(t *testing.T) {
	src := `
prompt Child inherits A, B {
  persona := from(parent[0])
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["persona"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr")
	}
	u := field.FromExpr.Units[0]
	if u.Kind != ast.FromParentRef {
		t.Errorf("expected FromParentRef, got %v", u.Kind)
	}
	if u.ParentSub.Kind != ast.SubIndex || u.ParentSub.N != 0 {
		t.Errorf("expected SubIndex{0}, got %+v", u.ParentSub)
	}
}

func TestFromNamedRef(t *testing.T) {
	src := `
prompt Child inherits python.develop_python {
  persona := from(python.develop_python)
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["persona"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr")
	}
	u := field.FromExpr.Units[0]
	if u.Kind != ast.FromNamedRef || u.ParentName != "python.develop_python" {
		t.Errorf("expected FromNamedRef{python.develop_python}, got %+v", u)
	}
}

func TestFromParentAndLiteral(t *testing.T) {
	src := `
prompt Child inherits Base {
  instructions := from(parent[*]) and {
    - new instruction 1
    - new instruction 2
  }
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["instructions"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr")
	}
	if len(field.FromExpr.Units) != 2 {
		t.Fatalf("expected 2 units, got %d: %+v", len(field.FromExpr.Units), field.FromExpr.Units)
	}
	if field.FromExpr.Units[0].Kind != ast.FromParentRef {
		t.Errorf("unit[0] should be FromParentRef")
	}
	lit := field.FromExpr.Units[1]
	if lit.Kind != ast.FromLiteral {
		t.Errorf("unit[1] should be FromLiteral, got %v", lit.Kind)
	}
	if len(lit.Items) != 2 || lit.Items[0] != "new instruction 1" || lit.Items[1] != "new instruction 2" {
		t.Errorf("unexpected literal items: %v", lit.Items)
	}
}

func TestFromFieldRef(t *testing.T) {
	src := `
prompt Child inherits A, B {
  instructions := parent[0].instructions[*] and parent[1].instructions[0..5]
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["instructions"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr")
	}
	if len(field.FromExpr.Units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(field.FromExpr.Units))
	}

	u0 := field.FromExpr.Units[0]
	if u0.Kind != ast.FromFieldRef || u0.FieldName != "instructions" {
		t.Errorf("unit[0]: expected FromFieldRef{instructions}, got %+v", u0)
	}
	if u0.SourceSub.Kind != ast.SubIndex || u0.SourceSub.N != 0 {
		t.Errorf("unit[0] SourceSub: expected SubIndex{0}, got %+v", u0.SourceSub)
	}
	if u0.FieldSub.Kind != ast.SubAll {
		t.Errorf("unit[0] FieldSub: expected SubAll, got %+v", u0.FieldSub)
	}

	u1 := field.FromExpr.Units[1]
	if u1.Kind != ast.FromFieldRef || u1.FieldName != "instructions" {
		t.Errorf("unit[1]: expected FromFieldRef{instructions}, got %+v", u1)
	}
	if u1.FieldSub.Kind != ast.SubRange || u1.FieldSub.N != 0 || u1.FieldSub.M != 5 {
		t.Errorf("unit[1] FieldSub: expected SubRange{0,5}, got %+v", u1.FieldSub)
	}
}

func TestFromTwoParents(t *testing.T) {
	src := `
prompt Child inherits A, B {
  instructions := from(parent[0]) and from(parent[1])
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	field := fieldsByName(n)["instructions"]
	if field.FromExpr == nil {
		t.Fatal("expected FromExpr")
	}
	if len(field.FromExpr.Units) != 2 {
		t.Fatalf("expected 2 units, got %d", len(field.FromExpr.Units))
	}
	for i, u := range field.FromExpr.Units {
		if u.Kind != ast.FromParentRef || u.ParentSub.Kind != ast.SubIndex || u.ParentSub.N != i {
			t.Errorf("unit[%d]: expected FromParentRef{SubIndex{%d}}, got %+v", i, i, u)
		}
	}
}

// ---- namespaced use ----

func TestNamespacedUse(t *testing.T) {
	src := `
prompt Foo {
  use go-backend.GoConventions
}
`
	nodes := mustParse(t, "foo.loom", src)
	n := nodes[0]
	if len(n.Uses) != 1 || n.Uses[0] != "go-backend.GoConventions" {
		t.Errorf("expected namespaced use, got %v", n.Uses)
	}
}

// ---- inline scalar from() ----

func TestInlineScalarFrom(t *testing.T) {
	src := `
prompt Child inherits A, B {
  persona := from(parent[0])
  summary := from(parent[1])
}
`
	nodes := mustParse(t, "child.loom", src)
	n := nodes[0]
	fm := fieldsByName(n)
	for _, fieldName := range []string{"persona", "summary"} {
		if fm[fieldName].FromExpr == nil {
			t.Errorf("field %q: expected FromExpr", fieldName)
		}
	}
}

// ---- single-parent backward compat (Parents slice populated) ----

func TestSingleParentPopulatesParentsSlice(t *testing.T) {
	nodes := mustParse(t, "cr.loom", srcCodeReviewer)
	n := nodes[0]
	if len(n.Parents) != 1 || n.Parents[0] != "BaseEngineer" {
		t.Errorf("expected Parents=[BaseEngineer], got %v", n.Parents)
	}
	if n.Parent != "BaseEngineer" {
		t.Errorf("Parent compat: expected BaseEngineer, got %q", n.Parent)
	}
}

// ---- error: invalid from() syntax ----

func TestFromExprInvalidSubscript(t *testing.T) {
	_, err := parser.Parse("bad.loom", `
prompt Child inherits Base {
  instructions := from(parent[bad])
}
`)
	if err == nil {
		t.Fatal("expected error for invalid subscript")
	}
}
