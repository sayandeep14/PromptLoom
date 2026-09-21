package format_test

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

func TestSlotMetadataSurvivesFormatting(t *testing.T) {
	cases := []struct {
		decl             string
		secret, required bool
		def              string
	}{
		{`slot a { secret: true }`, true, true, ""},
		{`slot b { required: false }`, false, false, ""},
		{`slot c { default: "x" }`, false, false, "x"},
		{`slot d { required: true, secret: true }`, true, true, ""},
		{`slot e { default: "x", secret: true }`, true, false, "x"},
		{`slot f { required: true }`, false, true, ""},
	}
	for _, c := range cases {
		src := "prompt P {\n  " + c.decl + "\n}\n"
		out, err := format.Source("t.loom", src)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := parser.Parse("t.loom", out)
		if err != nil {
			t.Fatalf("%s: formatted output does not parse: %v\n%s", c.decl, err, out)
		}
		v := nodes[0].Vars[0]
		if v.Secret != c.secret || v.Required != c.required || v.Default != c.def {
			t.Errorf("%s -> %q changed meaning: secret=%v required=%v default=%q, want secret=%v required=%v default=%q",
				c.decl, strings.TrimSpace(out), v.Secret, v.Required, v.Default, c.secret, c.required, c.def)
		}
	}
}

func TestEnvBlocksSurviveFormatting(t *testing.T) {
	src := "prompt P {\n  persona :=\n    x.\n\n  env prod {\n    constraints :=\n      - No debug logging.\n  }\n}\n"
	out, err := format.Source("t.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "env prod {") || !strings.Contains(out, "No debug logging.") {
		t.Errorf("the env block was lost:\n%s", out)
	}
}

func TestCommentsSurviveInEveryPosition(t *testing.T) {
	src := `// file header

// leads the prompt
prompt P {
  // about tags
  tags: a, b

  // about the var
  var language = "Go"

  // about use
  use Rules

  // about persona
  persona :=
    x.

  // about the variant
  variant v {
    // inside variant
    constraints :=
      - a
  }

  // about env
  env prod {
    // inside env
    constraints :=
      - b
  }

  // before the end
}

// trailing file comment
`
	out, err := format.Source("t.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"// file header", "// leads the prompt", "// about tags", "// about the var", "// about use",
		"// about persona", "// about the variant", "// inside variant", "// about env", "// inside env",
		"// before the end", "// trailing file comment",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("comment %q was lost:\n%s", want, out)
		}
	}
	// a comment stays directly above the element it introduced
	if !strings.Contains(out, "  // about persona\n  persona :=\n") {
		t.Errorf("leading comment detached from its field:\n%s", out)
	}
	if !strings.Contains(out, "// leads the prompt\nprompt P {") {
		t.Errorf("comment detached from its prompt:\n%s", out)
	}
}
