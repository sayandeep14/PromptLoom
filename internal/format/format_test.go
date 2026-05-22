package format_test

import (
	"strings"
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

// ── Phase 9: multi-parent inherits ───────────────────────────────────────────

func TestFormatMultiParentInheritance(t *testing.T) {
	src := `prompt Child  inherits   A,   B {
  instructions :=
    from(parent[*])
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A, B {
  instructions :=
    from(parent[*])
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatThreeParentInheritance(t *testing.T) {
	src := `prompt D inherits A, B, C {
  instructions :=
    from(parent[*])
}`
	out := roundtrip(t, src)
	want := `prompt D inherits A, B, C {
  instructions :=
    from(parent[*])
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// ── Phase 9: from() expression formatting ────────────────────────────────────

func TestFormatFromExprSimpleAll(t *testing.T) {
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[*])
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A, B {
  instructions :=
    from(parent[*])
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatFromExprSingleIndex(t *testing.T) {
	src := `prompt Child inherits A, B {
  persona :=
    from(parent[0])
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A, B {
  persona :=
    from(parent[0])
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatFromExprWithLiteralBlock(t *testing.T) {
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[*]) and {
      - extra item.
      - another item.
    }
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A, B {
  instructions :=
    from(parent[*]) and {
      - extra item.
      - another item.
    }
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatFromExprScalarNamedRef(t *testing.T) {
	src := `prompt Child inherits A {
  persona :=
    from(A)
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A {
  persona :=
    from(A)
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

func TestFormatFromExprMultipleFields(t *testing.T) {
	src := `prompt Child inherits A, B {
  persona :=
    from(parent[0])
  instructions :=
    from(parent[*]) and {
      - new step.
    }
}`
	out := roundtrip(t, src)
	want := `prompt Child inherits A, B {
  persona :=
    from(parent[0])

  instructions :=
    from(parent[*]) and {
      - new step.
    }
}
`
	if out != want {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", out, want)
	}
}

// ── Phase 9: semantic simplification ─────────────────────────────────────────

func TestSimplifyFromExprEmptyLiteral(t *testing.T) {
	// from(parent[*]) and {} → from(parent[*])  (empty literal is a no-op)
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[*]) and {}
}`
	out := roundtrip(t, src)
	// After simplification the empty literal is dropped.
	if !strings.Contains(out, "from(parent[*])") {
		t.Errorf("expected from(parent[*]) in output, got:\n%s", out)
	}
	if strings.Contains(out, "and {}") {
		t.Errorf("empty literal block should be simplified away, got:\n%s", out)
	}
}

func TestSimplifyFromExprSingleElementRange(t *testing.T) {
	// from(parent[0..1]) → from(parent[0])
	src := `prompt Child inherits A, B {
  persona :=
    from(parent[0..1])
}`
	out := roundtrip(t, src)
	if !strings.Contains(out, "from(parent[0])") {
		t.Errorf("expected single-element range to simplify to from(parent[0]), got:\n%s", out)
	}
	if strings.Contains(out, "0..1") {
		t.Errorf("single-element range should be simplified, got:\n%s", out)
	}
}

func TestSimplifyFromExprDuplicateUnits(t *testing.T) {
	// from(parent[*]) and from(parent[*]) → from(parent[*])
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[*]) and from(parent[*])
}`
	out := roundtrip(t, src)
	// After deduplication only one from(parent[*]) unit remains.
	count := strings.Count(out, "from(parent[*])")
	if count != 1 {
		t.Errorf("expected exactly 1 from(parent[*]) after deduplication, got %d:\n%s", count, out)
	}
}

func TestSimplifyFromExprPreservesDistinctUnits(t *testing.T) {
	// from(parent[0]) and from(parent[1]) should NOT be collapsed
	// (they are distinct — different parent indices).
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[0]) and from(parent[1])
}`
	out := roundtrip(t, src)
	if !strings.Contains(out, "from(parent[0])") || !strings.Contains(out, "from(parent[1])") {
		t.Errorf("distinct parent refs should both be preserved:\n%s", out)
	}
}

func TestSimplifyFromExprPreservesLiteralItems(t *testing.T) {
	// Literal block with items must NOT be removed.
	src := `prompt Child inherits A, B {
  instructions :=
    from(parent[*]) and {
      - keep this.
    }
}`
	out := roundtrip(t, src)
	if !strings.Contains(out, "keep this") {
		t.Errorf("literal items must not be removed:\n%s", out)
	}
	if !strings.Contains(out, "and {") {
		t.Errorf("non-empty literal block must be preserved:\n%s", out)
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
