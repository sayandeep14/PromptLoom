package format

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
)

func migrate(t *testing.T, src string) MigrateResult {
	t.Helper()
	res, err := Migrate("m.loom", src, nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, src)
	}
	return res
}

// resolved builds a registry from src and resolves every prompt, so that the v1 meaning of the
// original can be compared with the v2 meaning of the migrated file.
func resolved(t *testing.T, src string) map[string]*ast.ResolvedPrompt {
	t.Helper()
	// the reference side may still say `extends`, which the lexer refuses; only that keyword is
	// normalised, so the operators (the thing being compared) are still v1
	src = strings.Join(func() []string {
		ls := strings.Split(src, "\n")
		for i, l := range ls {
			ls[i] = extendsRe.ReplaceAllString(l, "${1}inherits${2}")
		}
		return ls
	}(), "\n")
	nodes, err := parser.Parse("m.loom", src)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, src)
	}
	reg := registry.New()
	if err := reg.Register(nodes); err != nil {
		t.Fatal(err)
	}
	out := map[string]*ast.ResolvedPrompt{}
	for _, p := range reg.Prompts() {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			t.Fatalf("resolve %s: %v\n%s", p.Name, err, src)
		}
		out[p.Name] = rp
	}
	return out
}

func sameMeaning(t *testing.T, before, after string) {
	t.Helper()
	a, b := resolved(t, before), resolved(t, after)
	for name, x := range a {
		y := b[name]
		got := [][]string{{y.Persona, y.Objective, y.Notes}, y.Instructions, y.Constraints, y.Examples, y.Format}
		want := [][]string{{x.Persona, x.Objective, x.Notes}, x.Instructions, x.Constraints, x.Examples, x.Format}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s changed meaning\n before: %v\n after:  %v\n--- migrated source:\n%s", name, want, got, after)
		}
	}
}

const v1Library = `// the base
prompt Base {
  persona:
    You are careful.

  instructions:
    - Read the code.
    - Run the tests.

  constraints:
    - Be kind.
}

block Rules {
  constraints +=
    - Never guess.
}

overlay Terse {
  instructions +=
    - Be brief.
}

prompt Child extends Base {
  use Rules

  instructions +=
    - Explain the fix.
    - Add a test.
}

prompt Grandchild inherits Child {
  instructions +=
    - Summarise at the end.
}

prompt Solo {
  persona:
    Alone.

  instructions +=
    - Only item.
}
`

func TestMigrateRewritesV1AndKeepsItsMeaning(t *testing.T) {
	res := migrate(t, v1Library)
	if !res.Changed() || len(res.Manual) != 0 {
		t.Fatalf("changed=%v manual=%v", res.Changed(), res.Manual)
	}
	out := res.Output

	for _, gone := range []string{"+=", "-=", " extends ", "persona:\n", "instructions:\n"} {
		if strings.Contains(out, gone) {
			t.Errorf("v1 syntax %q survived:\n%s", gone, out)
		}
	}
	for _, want := range []string{
		"prompt Child inherits Base {",
		"instructions :=\n    from(parent[*]) and {\n      - Explain the fix.\n      - Add a test.\n    }",
		"// the base", // comments survive
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}

	// the migrated library must not use a single legacy construct any more...
	nodes, err := parser.Parse("m.loom", out)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		for _, f := range n.Fields {
			if f.Op != ast.OpOverride {
				t.Errorf("%s.%s still uses %s", n.Name, f.FieldName, f.Op)
			}
		}
	}
	// ...and must mean exactly what the v1 library meant.
	sameMeaning(t, v1Library, out)
}

func TestMigrateIsIdempotent(t *testing.T) {
	first := migrate(t, v1Library)
	second := migrate(t, first.Output)
	if second.Changed() || len(second.Manual) != 0 || second.Output != first.Output {
		t.Errorf("a migrated file must be a fixed point:\n%+v", second)
	}
}

func TestMigrateLeavesV2SourceUntouched(t *testing.T) {
	src := "prompt A {\n  persona :=\n    p\n}\n"
	res := migrate(t, src)
	if res.Changed() || res.Output != src {
		t.Errorf("%+v", res)
	}
	// even if it is not in canonical format: migrate is not fmt
	odd := "prompt A {\n    persona :=\n        p\n}\n"
	if res := migrate(t, odd); res.Output != odd {
		t.Errorf("v2 input must come back byte for byte:\n%s", res.Output)
	}
}

func TestMigrateReportsWhatNeedsAHuman(t *testing.T) {
	src := `prompt Base {
  constraints :=
    - a
    - b

  persona :=
    p
}

prompt Child inherits Base {
  constraints -=
    - b

  persona +=
    and more

  instructions +=
    - x
  instructions +=
    - y

  variant strict {
    constraints +=
      - z
  }
}
`
	res := migrate(t, src)
	joined := ""
	for _, n := range res.Manual {
		joined += n.Message + "\n"
	}
	if len(res.Manual) != 5 {
		t.Fatalf("want 5 manual items, got %d:\n%s", len(res.Manual), joined)
	}
	for _, want := range []string{"-=", "scalar field that is inherited", "same body", "variant strict"} {
		if !strings.Contains(joined, want) {
			t.Errorf("no manual note about %q:\n%s", want, joined)
		}
	}
	// nothing was mechanical here, so the file is not touched at all
	if res.Changed() || res.Output != src {
		t.Errorf("unmigratable input must be returned unchanged:\n%s", res.Output)
	}
	for _, n := range res.Manual {
		if n.Line == 0 {
			t.Errorf("manual note without a line: %+v", n)
		}
	}
}

func TestMigrateMixedFileChangesWhatItCanAndReportsTheRest(t *testing.T) {
	src := "prompt Base {\n  persona:\n    p\n\n  constraints :=\n    - a\n}\n\nprompt Child inherits Base {\n  constraints -=\n    - a\n\n  instructions +=\n    - x\n}\n"
	res := migrate(t, src)
	if len(res.Manual) != 1 || !strings.Contains(res.Manual[0].Message, "-=") {
		t.Fatalf("%+v", res.Manual)
	}
	if !strings.Contains(res.Output, "constraints -=") {
		t.Errorf("the -= must be left exactly where it was:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "persona :=") || !strings.Contains(res.Output, "from(parent[*]) and {") {
		t.Errorf("mechanical parts must still be migrated:\n%s", res.Output)
	}
}

func TestMigrateMultiParentAndBlockScalars(t *testing.T) {
	src := "prompt A {\n  instructions :=\n    - a\n}\n\nprompt B {\n  instructions :=\n    - b\n}\n\nprompt C inherits A, B {\n  instructions +=\n    - c\n}\n\nblock G {\n  persona +=\n    x\n}\n"
	res := migrate(t, src)
	if !strings.Contains(res.Output, "from(parent[*]) and {") {
		t.Errorf("%s", res.Output)
	}
	if len(res.Manual) != 1 || !strings.Contains(res.Manual[0].Message, "scalar field in a block") {
		t.Errorf("%+v", res.Manual)
	}
}

func TestMigrateOnlyRewritesExtendsOnDeclarationLines(t *testing.T) {
	src := "prompt A extends Base {\n  notes:\n    this text says prompt X extends Y and must stay\n}\n\nprompt Base {\n  persona :=\n    p\n}\n"
	res := migrate(t, src)
	if !strings.Contains(res.Output, "prompt A inherits Base {") || !strings.Contains(res.Output, "prompt X extends Y and must stay") {
		t.Errorf("%s", res.Output)
	}
}

func TestMigrateRefusesFilesThatDoNotParse(t *testing.T) {
	src := "prompt A {\n  what is this\n}\n"
	res, err := Migrate("bad.loom", src, nil)
	if err == nil || !strings.Contains(err.Error(), "bad.loom") {
		t.Errorf("%v", err)
	}
	if res.Output != src {
		t.Error("the input must come back unchanged on error")
	}
}

func TestMigrateKeepsContractsSlotsAndVariants(t *testing.T) {
	src := `prompt A {
  slot repo { required: true }
  var lang = "go"
  tags: one, two

  persona:
    Works on {{repo}} in {{lang}}.

  variant short {
    persona:
      Brief.
  }

  contract {
    must_include:
      - done
  }
}
`
	res := migrate(t, src)
	for _, keep := range []string{"slot repo { required: true }", `var lang = "go"`, "tags: one, two", "must_include:", "variant short {", "persona :=\n      Brief."} {
		if !strings.Contains(res.Output, keep) {
			t.Errorf("lost %q:\n%s", keep, res.Output)
		}
	}
	sameMeaning(t, src, res.Output)
}

// v1: parent + block + own items. v2: a prompt that writes the field replaces the block's items.
// Rewriting `+=` mechanically here would silently drop the block's constraints.
func TestMigrateDoesNotDropBlockItems(t *testing.T) {
	src := "block Rules {\n  constraints :=\n    - Never guess.\n}\n\nprompt Base {\n  constraints :=\n    - Be kind.\n}\n\nprompt Child inherits Base {\n  use Rules\n\n  constraints +=\n    - Cite sources.\n}\n"
	res := migrate(t, src)
	if res.Changed() || len(res.Manual) != 1 || !strings.Contains(res.Manual[0].Message, "block Rules") {
		t.Fatalf("%+v", res)
	}
	if res.Output != src {
		t.Errorf("the file must not be touched:\n%s", res.Output)
	}

	// a block that does NOT define the field is no obstacle
	other := strings.Replace(src, "constraints :=\n    - Never guess.", "instructions :=\n    - Think first.", 1)
	res = migrate(t, other)
	if len(res.Manual) != 0 || !res.Changed() {
		t.Fatalf("%+v", res)
	}
	sameMeaning(t, other, res.Output)
}

func TestMigrateUsesBlocksFromOtherFiles(t *testing.T) {
	prompt := "prompt Child {\n  use Rules\n\n  constraints +=\n    - Mine.\n}\n"
	rules := "block Rules {\n  constraints :=\n    - Never guess.\n}\n"

	// without knowing the block, it cannot be proven safe
	res := migrate(t, prompt)
	if len(res.Manual) != 1 || !strings.Contains(res.Manual[0].Message, "not in the files scanned") {
		t.Errorf("unknown block must be treated as possibly conflicting: %+v", res)
	}

	lib := NewLibrary()
	lib.AddSource("Rules.block.loom", rules)
	res, err := Migrate("Child.prompt.loom", prompt, lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Manual) != 1 || !strings.Contains(res.Manual[0].Message, "also defines") {
		t.Errorf("a block that defines the field conflicts: %+v", res.Manual)
	}

	lib = NewLibrary()
	lib.AddSource("Rules.block.loom", "block Rules {\n  instructions :=\n    - Think.\n}\n")
	res, _ = Migrate("Child.prompt.loom", prompt, lib)
	if len(res.Manual) != 0 || !strings.Contains(res.Output, "constraints :=") {
		t.Errorf("a block that does not define the field is fine: %+v", res)
	}
	// unparseable library files are skipped, not fatal
	lib.AddSource("broken.loom", "prompt {")
}
