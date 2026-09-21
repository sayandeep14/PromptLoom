package diff

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func find(diffs []FieldDiff, field string) FieldDiff {
	for _, d := range diffs {
		if d.Field == field {
			return d
		}
	}
	return FieldDiff{}
}

func TestIdenticalPromptsHaveNoChanges(t *testing.T) {
	rp := &ast.ResolvedPrompt{Persona: "p", Instructions: []string{"a"}, InheritsChain: []string{"X", "Y"}}
	for _, d := range DiffPrompts(rp, rp) {
		if d.Changed {
			t.Errorf("%s reported as changed", d.Field)
		}
	}
}

func TestScalarChange(t *testing.T) {
	a := &ast.ResolvedPrompt{Persona: "old", Objective: "same"}
	b := &ast.ResolvedPrompt{Persona: "new", Objective: "same"}
	d := DiffPrompts(a, b)
	if p := find(d, "persona"); !p.Changed || p.Before != "old" || p.After != "new" || p.IsList {
		t.Errorf("%+v", p)
	}
	if find(d, "objective").Changed {
		t.Error("objective did not change")
	}
}

func TestListChangeIsAnAddedRemovedSet(t *testing.T) {
	a := &ast.ResolvedPrompt{Constraints: []string{"keep", "drop"}}
	b := &ast.ResolvedPrompt{Constraints: []string{"keep", "new1", "new2"}}
	c := find(DiffPrompts(a, b), "constraints")
	if !c.Changed || !c.IsList || strings.Join(c.Added, ",") != "new1,new2" || strings.Join(c.Removed, ",") != "drop" {
		t.Errorf("%+v", c)
	}
}

// Pinned: lists are compared as SETS. Reordering, or changing how many times an item
// appears, is not a difference.
func TestReorderingIsNotAChange(t *testing.T) {
	a := &ast.ResolvedPrompt{Instructions: []string{"one", "two", "two"}}
	b := &ast.ResolvedPrompt{Instructions: []string{"two", "one"}}
	if find(DiffPrompts(a, b), "instructions").Changed {
		t.Error("a pure reordering must not count as a change (set comparison)")
	}
}

func TestInheritanceChain(t *testing.T) {
	a := &ast.ResolvedPrompt{InheritsChain: []string{"A", "Base"}}
	b := &ast.ResolvedPrompt{InheritsChain: []string{"A", "Other", "Base"}}
	i := find(DiffPrompts(a, b), "inheritance")
	if !i.Changed || i.Before != "A → Base" || i.After != "A → Other → Base" {
		t.Errorf("%+v", i)
	}
}

func TestEveryFieldIsReportedInACanonicalOrder(t *testing.T) {
	d := DiffPrompts(&ast.ResolvedPrompt{}, &ast.ResolvedPrompt{})
	if len(d) < 9 {
		t.Fatalf("expected an entry for every field, got %d", len(d))
	}
	seen := map[string]bool{}
	for _, x := range d {
		if seen[x.Field] {
			t.Errorf("duplicate field %s", x.Field)
		}
		seen[x.Field] = true
		if x.Heading == "" {
			t.Errorf("%s has no heading", x.Field)
		}
	}
	d2 := DiffPrompts(&ast.ResolvedPrompt{}, &ast.ResolvedPrompt{})
	for i := range d {
		if d[i].Field != d2[i].Field {
			t.Fatal("the order must be stable")
		}
	}
}

func TestSetDiffAndJoinChain(t *testing.T) {
	added, removed := setDiff([]string{"a", "b"}, []string{"b", "c"})
	if strings.Join(added, ",") != "c" || strings.Join(removed, ",") != "a" {
		t.Errorf("%v %v", added, removed)
	}
	if a, r := setDiff(nil, nil); a != nil || r != nil {
		t.Error("empty inputs")
	}
	if joinChain(nil) != "" || joinChain([]string{"A"}) != "A" || joinChain([]string{"A", "B"}) != "A → B" {
		t.Error("joinChain")
	}
}
