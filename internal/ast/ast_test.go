package ast

import "testing"

func TestOperatorString(t *testing.T) {
	for op, want := range map[Operator]string{OpDefine: ":", OpOverride: ":=", OpAppend: "+=", OpRemove: "-=", Operator(99): "?"} {
		if op.String() != want {
			t.Errorf("Operator(%d) = %q, want %q", op, op.String(), want)
		}
	}
}

func TestPositionString(t *testing.T) {
	if got := (Position{File: "a.loom", Line: 3, Col: 5}).String(); got != "a.loom:3:5" {
		t.Error(got)
	}
	if got := (Position{File: "a.loom", Line: 3}).String(); got != "a.loom:3" {
		t.Error(got)
	}
}

// The field tables drive validation, formatting and resolution; if they drift apart the
// toolchain disagrees with itself about what a field is.
func TestFieldTables(t *testing.T) {
	for f := range ScalarFields {
		if ListFields[f] {
			t.Errorf("%q is both a scalar and a list field", f)
		}
		if !ValidFields[f] {
			t.Errorf("scalar %q missing from ValidFields", f)
		}
	}
	for f := range ListFields {
		if !ValidFields[f] {
			t.Errorf("list %q missing from ValidFields", f)
		}
	}
	if len(ValidFields) != len(ScalarFields)+len(ListFields) {
		t.Errorf("ValidFields has %d entries, want %d", len(ValidFields), len(ScalarFields)+len(ListFields))
	}
	// the fields the language documents
	for _, f := range []string{"summary", "persona", "context", "objective", "notes", "instructions", "constraints", "examples", "format"} {
		if !ValidFields[f] {
			t.Errorf("documented field %q is not valid", f)
		}
	}
	for _, f := range []string{"", "name", "Persona", "instruction", "tags"} {
		if ValidFields[f] {
			t.Errorf("%q must not be a field (case-sensitive, plural forms only)", f)
		}
	}
}

func TestKindsAreDistinct(t *testing.T) {
	if KindPrompt == KindBlock || KindBlock == KindOverlay || KindPrompt == KindOverlay {
		t.Error("node kinds must differ")
	}
	if KindPrompt != 0 {
		t.Error("the zero value of NodeKind is a prompt")
	}
}
