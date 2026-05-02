package format_test

import (
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/format"
	"github.com/sayandeepgiri/promptloom/internal/parser"
)

// roundtrip parses src, formats it, parses again, and formats again.
// The two formatted outputs must be identical (idempotent).
func roundtrip(t *testing.T, src string) string {
	t.Helper()
	nodes, err := parser.Parse("t.prompt", src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	first := format.Nodes(nodes)

	nodes2, err := parser.Parse("t.prompt", first)
	if err != nil {
		t.Fatalf("parse error after first format: %v\nformatted:\n%s", err, first)
	}
	second := format.Nodes(nodes2)

	if first != second {
		t.Errorf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	return first
}

func TestFormatBasePrompt(t *testing.T) {
	src := `
prompt BaseEngineer {
  summary:
    General-purpose engineering assistant prompt.

  persona:
    You are a senior software engineer who writes clear, maintainable, production-ready code.

  constraints:
    - Do not hallucinate APIs.
    - Ask for missing information only when necessary.

  format:
    - Summary
    - Recommendation
}`
	out := roundtrip(t, src)

	want := `prompt BaseEngineer {
  summary:
    General-purpose engineering assistant prompt.

  persona:
    You are a senior software engineer who writes clear, maintainable, production-ready code.

  constraints:
    - Do not hallucinate APIs.
    - Ask for missing information only when necessary.

  format:
    - Summary
    - Recommendation
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatInheritance(t *testing.T) {
	src := `prompt Child   inherits   Parent {
  objective :=
    Do the thing.
}`
	out := roundtrip(t, src)

	want := `prompt Child inherits Parent {
  objective :=
    Do the thing.
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatBlock(t *testing.T) {
	src := `block MyRules {
  constraints:
    - Rule one.
    - Rule two.
}`
	out := roundtrip(t, src)

	want := `block MyRules {
  constraints:
    - Rule one.
    - Rule two.
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatUseStatements(t *testing.T) {
	src := `prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules
  use SecurityChecklist

  context:
    Spring Boot backend.

  objective :=
    Review the code.
}`
	out := roundtrip(t, src)

	want := `prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules
  use SecurityChecklist

  context:
    Spring Boot backend.

  objective :=
    Review the code.
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatAllOperators(t *testing.T) {
	src := `prompt Ops inherits Base {
  summary:=
    override
  notes +=
    appended note
  constraints -=
    - remove this
}`
	out := roundtrip(t, src)

	// All operators should have correct spacing.
	want := `prompt Ops inherits Base {
  summary :=
    override

  notes +=
    appended note

  constraints -=
    - remove this
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatMultipleNodes(t *testing.T) {
	src := `block A {
  constraints:
    - item
}

prompt B {
  objective:
    do stuff
  format:
    - Result
}`
	out := roundtrip(t, src)

	want := `block A {
  constraints:
    - item
}

prompt B {
  objective:
    do stuff

  format:
    - Result
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatIdempotentOnSpecExamples(t *testing.T) {
	examples := []string{
		`prompt BaseEngineer {
  summary:
    General-purpose engineering assistant prompt.
  persona:
    You are a senior software engineer.
  objective:
    Help with engineering tasks.
  constraints:
    - Do not hallucinate APIs.
  format:
    - Summary
    - Recommendation
}`,
		`prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review the provided code.
  instructions +=
    - Read the code carefully.
    - Identify issues.
  format :=
    - Summary
    - Issues Found
}`,
		`block SpringBootRules {
  constraints:
    - Check transaction boundaries.
    - Check JPA entity mappings.
}`,
	}
	for _, src := range examples {
		roundtrip(t, src) // idempotency checked inside roundtrip
	}
}

func TestFormatVariantAndOverlay(t *testing.T) {
	src := `prompt LanguageReviewer {
  var language = "Python"
  slot framework { required: true }
  context:
    Review {{ language }} services using {{ framework }}.
  variant concise {
    constraints +=
      - Keep it short.
  }
}`
	out := roundtrip(t, src)

	want := `prompt LanguageReviewer {
  var language = "Python"
  slot framework { required: true }

  context:
    Review {{ language }} services using {{ framework }}.

  variant concise {
    constraints +=
      - Keep it short.
  }
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}
