package format_test

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// fieldsOf parses src and returns the fields of the node named `name` — how a real caller
// builds newFields: parse a candidate, take its fields.
func fieldsOf(t *testing.T, src, name string) []ast.FieldOperation {
	t.Helper()
	nodes, err := parser.Parse("candidate.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == name {
			return n.Fields
		}
	}
	t.Fatalf("no node %q in candidate", name)
	return nil
}

const replaceLibRaw = `// file header comment
block Guard {
  constraints :=
    - be safe
}

// about Base
prompt Base {
  // persona comment
  persona :=
    You are careful.

  instructions :=
    - Read the code.
    // trailing comment inside instructions
}
// after Base

prompt Child inherits Base {
  use Guard
  var lang = "go"
  tags: a, b

  persona :=
    You review {{lang}} code.

  constraints :=
    from(parent[0]) and {
      - Cite sources.
    }

  variant terse {
    persona :=
      Brief.
  }

  contract {
    must_include:
      - verdict
  }
}
// trailing file comment
`

// replaceLib is the fixture above, canonically formatted first: ReplaceFields (like
// format.Source) re-renders the WHOLE file, so unrelated nodes only stay byte-for-byte
// identical when they started in canonical form. A real project keeps its files that way
// (`loom fmt`), so this is the realistic case to test byte-exactness against.
func replaceLib(t *testing.T) string {
	t.Helper()
	out, err := format.Source("f.loom", replaceLibRaw)
	if err != nil {
		t.Fatal(err)
	}
	again, err := format.Source("f.loom", out)
	if err != nil || again != out {
		t.Fatalf("fixture is not stable under formatting: %v", err)
	}
	return out
}

func TestReplaceFieldsKeepsEverythingElseByteForByte(t *testing.T) {
	lib := replaceLib(t)
	newFields := fieldsOf(t, "prompt X {\n  persona :=\n    You review carefully and kindly.\n\n  format :=\n    - Verdict\n}\n", "X")
	out, err := format.ReplaceFields("f.loom", lib, "Child", newFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "You review carefully and kindly.") || !strings.Contains(out, "format :=\n    - Verdict") {
		t.Errorf("new content missing:\n%s", out)
	}
	if strings.Contains(out, "Cite sources") || strings.Contains(out, "You review {{lang}} code.") {
		t.Errorf("old field content survived:\n%s", out)
	}
	for _, want := range []string{"prompt Child inherits Base {", "use Guard", `var lang = "go"`, "tags: a, b", "variant terse {", "contract {", "must_include:\n      - verdict"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	// every declaration that was NOT touched, and its comments, are byte-identical
	i := strings.Index(lib, "prompt Child")
	untouched := lib[:i]
	if !strings.HasPrefix(out, untouched) {
		t.Errorf("content before Child changed:\nwant prefix:\n%s\n--- got ---\n%s", untouched, out)
	}
	if !strings.Contains(out, "// trailing file comment") {
		t.Error("the file-trailing comment must survive")
	}
	if _, err := parser.Parse("f.loom", out); err != nil {
		t.Fatalf("result does not parse: %v", err)
	}
	if formatted, err := format.Source("f.loom", out); err != nil || formatted != out {
		t.Errorf("result is not canonically formatted:\n%s", out)
	}
}

func TestReplaceFieldsRefusesAnUnknownOrDuplicateName(t *testing.T) {
	lib := replaceLib(t)
	fields := fieldsOf(t, "prompt X {\n  persona :=\n    p\n}\n", "X")
	if _, err := format.ReplaceFields("f.loom", lib, "Nope", fields); err == nil || !strings.Contains(err.Error(), "no declaration named") {
		t.Errorf("%v", err)
	}
	dup := "prompt A {\n  persona :=\n    x\n}\nprompt A {\n  persona :=\n    y\n}\n"
	if _, err := format.ReplaceFields("f.loom", dup, "A", fields); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("%v", err)
	}
}

// No comment is ever lost — same guarantee as format.Source. One that sat inside the field being
// replaced moves to the end of the node's body (its original context is gone); every comment
// outside the changed node, even in an untouched sibling, keeps its exact text and position.
func TestReplaceFieldsNeverLosesAComment(t *testing.T) {
	lib := replaceLib(t)
	newFields := fieldsOf(t, "prompt X {\n  instructions :=\n    - Completely different.\n}\n", "X")
	out, err := format.ReplaceFields("f.loom", lib, "Base", newFields)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"// file header comment", "// about Base", "// after Base", "// trailing file comment", "// persona comment", "// trailing comment inside instructions"} {
		if !strings.Contains(out, want) {
			t.Errorf("a comment was lost, missing %q:\n%s", want, out)
		}
	}
	// the two comments that were inside Base's changed field moved to the end of its body,
	// after the new field content
	newContent := "- Completely different."
	if i, j := strings.Index(out, newContent), strings.Index(out, "persona comment"); i < 0 || j < i {
		t.Errorf("the relocated comments should follow the new content:\n%s", out)
	}
	// Child, entirely untouched, is byte-identical
	origChild := lib[strings.Index(lib, "prompt Child"):]
	if !strings.HasSuffix(out, origChild) {
		t.Errorf("Child changed even though only Base was targeted:\nwant suffix:\n%s\n--- got ---\n%s", origChild, out)
	}
}

func TestReplaceFieldsOnABlock(t *testing.T) {
	lib := replaceLib(t)
	newFields := fieldsOf(t, "block X {\n  constraints :=\n    - stay careful\n    - stay kind\n}\n", "X")
	out, err := format.ReplaceFields("f.loom", lib, "Guard", newFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "- stay careful") || !strings.Contains(out, "- stay kind") || strings.Contains(out, "be safe") {
		t.Errorf("%s", out)
	}
}

func TestReplaceFieldsInputMustParse(t *testing.T) {
	if _, err := format.ReplaceFields("f.loom", "prompt {\n", "X", nil); err == nil {
		t.Error("a source file that does not parse must be an error")
	}
}

// ReplaceFields never applies anything but Fields: even if the caller (by mistake) passes fields
// extracted from a candidate that also declared different parents/vars/etc., only field content
// moves — structure always comes from the ORIGINAL node. Callers that need to reject a candidate
// whose structure changed do so before calling ReplaceFields (see internal/optimize).
func TestReplaceFieldsIgnoresEverythingButFieldsInTheInput(t *testing.T) {
	lib := replaceLib(t)
	// a candidate with a different parent, extra var, and no contract — only its Fields are used
	candidate := "prompt X inherits Guard {\n  var extra = \"1\"\n  persona :=\n    changed\n}\n"
	newFields := fieldsOf(t, candidate, "X")
	out, err := format.ReplaceFields("f.loom", lib, "Child", newFields)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "prompt Child inherits Base {") || strings.Contains(out, "inherits Guard") {
		t.Errorf("parents must still come from the original node:\n%s", out)
	}
	if strings.Contains(out, "extra") || !strings.Contains(out, `var lang = "go"`) {
		t.Errorf("vars must still come from the original node:\n%s", out)
	}
	if !strings.Contains(out, "contract {") {
		t.Errorf("the contract must still come from the original node:\n%s", out)
	}
}
