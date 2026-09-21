package installer

import (
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/deps"
)

// ---- splitConstraint ----

func TestSplitConstraint(t *testing.T) {
	cases := []struct {
		in  string
		op  string
		ver string
		ok  bool
	}{
		{"==1.0.0", "==", "1.0.0", true},
		{">=2.3.0", ">=", "2.3.0", true},
		{">0.1.0", ">", "0.1.0", true},
		{"<=3.0.0", "<=", "3.0.0", true},
		{"<4.0.0", "<", "4.0.0", true},
		{"~=1.2.3", "~=", "1.2.3", true},
		{"any", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		op, ver, ok := splitConstraint(c.in)
		if ok != c.ok {
			t.Errorf("splitConstraint(%q): ok=%v want %v", c.in, ok, c.ok)
			continue
		}
		if ok && (op != c.op || ver != c.ver) {
			t.Errorf("splitConstraint(%q): got (%q,%q) want (%q,%q)", c.in, op, ver, c.op, c.ver)
		}
	}
}

// ---- allSatisfied ----

func TestAllSatisfiedTrue(t *testing.T) {
	constraints := []ConflictEntry{
		{Constraint: ">=1.0.0", RequiredBy: "alpha"},
		{Constraint: "<=2.0.0", RequiredBy: "beta"},
	}
	if !allSatisfied("1.5.0", constraints) {
		t.Error("expected 1.5.0 to satisfy >=1.0.0 and <=2.0.0")
	}
}

func TestAllSatisfiedFalse(t *testing.T) {
	constraints := []ConflictEntry{
		{Constraint: ">=2.0.0", RequiredBy: "alpha"},
		{Constraint: "<2.0.0", RequiredBy: "beta"},
	}
	if allSatisfied("1.9.9", constraints) {
		t.Error("expected 1.9.9 to NOT satisfy >=2.0.0 and <2.0.0 simultaneously")
	}
}

func TestAllSatisfiedAnyVersionSkipped(t *testing.T) {
	constraints := []ConflictEntry{
		{Constraint: "any", RequiredBy: "(direct)"},
	}
	if !allSatisfied("0.0.1", constraints) {
		t.Error("expected 'any' constraint to always satisfy")
	}
}

// ---- detectConflicts ----

func TestDetectConflictsNoConflict(t *testing.T) {
	pl := &deps.PackLock{}
	pl.Upsert(deps.PackLockEntry{Slug: "shared", Version: "1.5.0"})

	reqs := map[string]*requirement{
		"shared": {constraints: []ConflictEntry{
			{Constraint: ">=1.0.0", RequiredBy: "a"},
			{Constraint: "<=2.0.0", RequiredBy: "b"},
		}},
	}
	conflicts := detectConflicts(pl, reqs)
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts, got %v", conflicts)
	}
}

func TestDetectConflictsWithConflict(t *testing.T) {
	pl := &deps.PackLock{}
	pl.Upsert(deps.PackLockEntry{Slug: "shared", Version: "1.0.0"})

	reqs := map[string]*requirement{
		"shared": {constraints: []ConflictEntry{
			{Constraint: ">=2.0.0", RequiredBy: "a"},
			{Constraint: "<2.0.0", RequiredBy: "b"},
		}},
	}
	conflicts := detectConflicts(pl, reqs)
	if len(conflicts) != 1 {
		t.Fatalf("expected 1 conflict, got %d: %v", len(conflicts), conflicts)
	}
	if conflicts[0].Slug != "shared" {
		t.Errorf("conflict slug: expected 'shared', got %q", conflicts[0].Slug)
	}
}

func TestDetectConflictsSingleConstraintNoConflict(t *testing.T) {
	pl := &deps.PackLock{}
	pl.Upsert(deps.PackLockEntry{Slug: "solo", Version: "1.0.0"})

	reqs := map[string]*requirement{
		"solo": {constraints: []ConflictEntry{
			{Constraint: ">=1.0.0", RequiredBy: "a"},
		}},
	}
	conflicts := detectConflicts(pl, reqs)
	// Only 1 constraint — no cross-pack conflict possible.
	if len(conflicts) != 0 {
		t.Errorf("expected no conflicts for single constraint, got %v", conflicts)
	}
}

// ---- Conflict.Error() ----

func TestConflictError(t *testing.T) {
	c := Conflict{
		Slug: "mypack",
		Entries: []ConflictEntry{
			{Constraint: ">=2.0.0", RequiredBy: "a"},
		},
	}
	msg := c.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	if msg[:len("version conflict")] != "version conflict" {
		t.Errorf("unexpected prefix: %q", msg)
	}
}
