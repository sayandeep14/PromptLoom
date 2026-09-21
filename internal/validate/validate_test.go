package validate_test

import (
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/validate"
)

// buildReg parses each source and registers all nodes.
func buildReg(t *testing.T, sources map[string]string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for filename, src := range sources {
		nodes, err := parser.Parse(filename, src)
		if err != nil {
			t.Fatalf("parse error in %s: %v", filename, err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatalf("register error: %v", err)
		}
	}
	return reg
}

func defaultCfg() *config.Config {
	return config.Defaults()
}

func hasError(diags []validate.Diagnostic, substr string) bool {
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

func hasWarning(diags []validate.Diagnostic, substr string) bool {
	for _, d := range diags {
		if d.Sev == validate.Warning && strings.Contains(d.Message, substr) {
			return true
		}
	}
	return false
}

// ---- spec example pack should produce 0 errors ----

func TestSpecExamplePackNoErrors(t *testing.T) {
	sources := map[string]string{
		"prompts/base.prompt": `
prompt BaseEngineer {
  summary:
    General-purpose engineering assistant.
  persona:
    You are a senior engineer.
  objective:
    Help with engineering tasks.
  constraints:
    - Do not hallucinate APIs.
  format:
    - Summary
    - Recommendation
}`,
		"prompts/code-review.prompt": `
prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review code for correctness and readability.
  instructions :=
    from(parent[0]) and {
      - Read the code carefully.
    }
  format :=
    - Summary
    - Issues Found
    - Suggested Fixes
    - Final Recommendation
}`,
		"blocks/spring-boot-rules.prompt": `
block SpringBootRules {
  constraints:
    - Check transaction boundaries.
    - Check JPA entity mappings.
}`,
		"prompts/spring-boot-review.prompt": `
prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules
  context:
    Spring Boot backend service.
  objective :=
    Review Spring Boot code for correctness and production readiness.
}`,
	}

	reg := buildReg(t, sources)
	diags := validate.Validate(reg, defaultCfg())

	for _, d := range diags {
		if d.Sev == validate.Error {
			t.Errorf("unexpected error: %s", d)
		}
	}
}

// ---- error: unknown parent ----

func TestUnknownParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Child inherits NonExistent {
  objective:
    something
  format:
    - item
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "unknown prompt") {
		t.Errorf("expected 'unknown prompt' error, got: %v", diags)
	}
}

// ---- error: unknown block ----

func TestUnknownBlock(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Foo {
  use NoSuchBlock
  objective:
    do stuff
  format:
    - item
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "unknown block") {
		t.Errorf("expected 'unknown block' error, got: %v", diags)
	}
}

// ---- error: inheritance cycle ----

func TestInheritanceCycle(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"cycle.prompt": `
prompt A inherits B {
  objective:
    x
  format:
    - item
}

prompt B inherits C {
  objective:
    y
  format:
    - item
}

prompt C inherits A {
  objective:
    z
  format:
    - item
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "cycle") {
		t.Errorf("expected cycle error, got: %v", diags)
	}
}

// ---- error: invalid field name ----

func TestInvalidFieldName(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Foo {
  unknownfield:
    some content
  objective:
    do stuff
  format:
    - item
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "unknown field") {
		t.Errorf("expected 'unknown field' error, got: %v", diags)
	}
}

// ---- error: -= on scalar field ----

func TestRemoveOnScalarField(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"base.prompt": `
prompt Base {
  persona:
    Senior engineer.
  objective:
    Help.
  format:
    - item
}`,
		"child.prompt": `
prompt Child inherits Base {
  persona -=
    Senior engineer.
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "not supported on scalar") {
		t.Errorf("expected scalar -=  error, got: %v", diags)
	}
}

// ---- warning: missing objective ----

func TestMissingObjectiveWarning(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Foo {
  summary:
    A prompt without an objective.
  format:
    - item
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasWarning(diags, "objective") {
		t.Errorf("expected objective warning, got: %v", diags)
	}
}

// ---- warning: missing format ----

func TestMissingFormatWarning(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Foo {
  objective:
    Do stuff.
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasWarning(diags, "format") {
		t.Errorf("expected format warning, got: %v", diags)
	}
}

// ---- warning: deep inheritance ----

func TestDeepInheritanceWarning(t *testing.T) {
	// Build a chain of depth 4 (> max 3)
	reg := buildReg(t, map[string]string{
		"chain.prompt": `
prompt A {
  objective:
    root
  format:
    - item
}

prompt B inherits A {
  objective :=
    b
}

prompt C inherits B {
  objective :=
    c
}

prompt D inherits C {
  objective :=
    d
}
`,
	})
	cfg := defaultCfg()
	cfg.Validation.MaxInheritanceDepth = 2 // D has depth 3, which exceeds 2
	diags := validate.Validate(reg, cfg)
	if !hasWarning(diags, "inheritance depth") {
		t.Errorf("expected deep inheritance warning, got: %v", diags)
	}
}

// ---- warning: ambiguous ':' redefine of inherited field ----

func TestAmbiguousRedefineWarning(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"base.prompt": `
prompt Base {
  constraints:
    - item one
  objective:
    help
  format:
    - item
}`,
		"child.prompt": `
prompt Child inherits Base {
  constraints:
    - overriding without explicit operator
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasWarning(diags, "explicit operator") {
		t.Errorf("expected ambiguous redefine warning, got: %v", diags)
	}
}

func TestUndeclaredVariableError(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt LanguageReviewer {
  var language = "Python"
  objective:
    Review {{ language }} code with {{ framework }}.
  format:
    - Summary
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "undeclared variable") {
		t.Fatalf("expected undeclared variable error, got: %v", diags)
	}
}

func TestRequiredSlotWarning(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt MigrationPlanner {
  slot source_version { required: true }
  objective:
    Plan migration from {{ source_version }}.
  format:
    - Plan
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasWarning(diags, "requires runtime values") {
		t.Fatalf("expected required slot warning, got: %v", diags)
	}
}

// ---- suggest: typo in parent name ----

func TestSuggestionOnTypo(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.prompt": `
prompt Good {
  objective:
    good
  format:
    - item
}

prompt Child inherits Goud {
  objective :=
    child
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	found := false
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "Did you mean") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'Did you mean' suggestion, got: %v", diags)
	}
}

// ---- duplicate prompt name ----

// ---- Phase 7: multi-parent validation ----

func TestMultiParentBothUnknown(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"p.loom": `
prompt Child inherits NoSuchA, NoSuchB {
  instructions :=
    from(parent[*])
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	count := 0
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "unknown prompt") {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 'unknown prompt' errors (one per unknown parent), got %d: %v", count, diags)
	}
}

func TestMultiParentOnlyOneUnknown(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
		"child.loom": `
prompt Child inherits A, Ghost {
  instructions :=
    from(parent[*])
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "unknown prompt") {
		t.Errorf("expected 'unknown prompt' error for Ghost, got: %v", diags)
	}
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "\"A\"") {
			t.Errorf("should not report A as unknown, got: %s", d.Message)
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
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "cycle") {
		t.Errorf("expected cycle error, got: %v", diags)
	}
}

func TestDiamondNoCycleError(t *testing.T) {
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
	diags := validate.Validate(reg, defaultCfg())
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "cycle") {
			t.Errorf("diamond inheritance should not report a cycle, got: %s", d.Message)
		}
	}
}

// ---- Phase 7: from() static validation ----

func TestFromParentAllOnScalarError(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    from(parent[*])
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "from(parent[*]) cannot be used on scalar") {
		t.Errorf("expected scalar from(parent[*]) error, got: %v", diags)
	}
}

func TestFromParentIndexOutOfBounds(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  instructions :=
    - do A.
}`,
		"b.loom": `
prompt B inherits A {
  instructions :=
    from(parent[5])
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "out of range") {
		t.Errorf("expected out-of-range error for parent[5], got: %v", diags)
	}
}

func TestFromParentIndexValid(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  instructions :=
    - do A.
}`,
		"b.loom": `
prompt B {
  instructions :=
    - do B.
}`,
		"c.loom": `
prompt C inherits A, B {
  instructions :=
    from(parent[0])
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "out of range") {
			t.Errorf("parent[0] should be valid for 2-parent prompt, got: %s", d.Message)
		}
	}
}

func TestFromNamedRefNotInParents(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base A.
}`,
		"b.loom": `
prompt B {
  persona :=
    base B.
}`,
		"c.loom": `
prompt C inherits A {
  persona :=
    from(B)
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	if !hasError(diags, "not a declared parent") {
		t.Errorf("expected 'not a declared parent' error for from(B), got: %v", diags)
	}
}

func TestFromNamedRefValidParent(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": `
prompt A {
  persona :=
    base A.
}`,
		"b.loom": `
prompt B inherits A {
  persona :=
    from(A)
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	for _, d := range diags {
		if d.Sev == validate.Error && strings.Contains(d.Message, "not a declared parent") {
			t.Errorf("from(A) should be valid when A is a declared parent, got: %s", d.Message)
		}
	}
}

// ---- Phase 7: deprecated operator warnings ----

func firstError(diags []validate.Diagnostic, substr string) *validate.Diagnostic {
	for i := range diags {
		if diags[i].Sev == validate.Error && strings.Contains(diags[i].Message, substr) {
			return &diags[i]
		}
	}
	return nil
}

func countMentions(diags []validate.Diagnostic, substr string) int {
	n := 0
	for _, d := range diags {
		if strings.Contains(d.Message, substr) {
			n++
		}
	}
	return n
}

func TestAppendOperatorIsAnErrorWithTheExactFix(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": "prompt A {\n  instructions :=\n    - base item.\n}",
		"b.loom": "prompt B inherits A {\n  instructions +=\n    - extra item.\n}",
	})
	d := firstError(validate.Validate(reg, defaultCfg()), "'+=' is not valid in v2")
	if d == nil {
		t.Fatal("expected an error for '+='")
	}
	for _, want := range []string{"instructions :=", "from(parent[0]) and {"} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message should contain the rewrite %q:\n%s", want, d.Message)
		}
	}
	if d.Pos.Line != 2 || d.Pos.File != "b.loom" {
		t.Errorf("position should point at the offending line, got %v", d.Pos)
	}
}

func TestAppendFixDependsOnTheSituation(t *testing.T) {
	cases := []struct {
		name string
		srcs map[string]string
		want []string
		not  []string
	}{
		{"several parents", map[string]string{
			"a.loom": "prompt A {\n  instructions :=\n    - a\n}\nprompt B {\n  instructions :=\n    - b\n}",
			"c.loom": "prompt C inherits A, B {\n  instructions +=\n    - c\n}",
		}, []string{"from(parent[*]) and {", "from(parent[N])"}, nil},
		{"no parent", map[string]string{
			"a.loom": "prompt A {\n  instructions +=\n    - a\n}",
		}, []string{"no parent to append to", "instructions :="}, []string{"from("}},
		{"scalar field", map[string]string{
			"a.loom": "prompt A {\n  persona :=\n    x\n}",
			"b.loom": "prompt B inherits A {\n  persona +=\n    more\n}",
		}, []string{"scalar field cannot be appended"}, nil},
		{"in a block", map[string]string{
			"a.loom": "block B {\n  constraints +=\n    - x\n}",
		}, []string{"already ADD their list items", "constraints :="}, []string{"from("}},
		{"in an overlay", map[string]string{
			"a.loom": "overlay O {\n  constraints +=\n    - x\n}",
		}, []string{"already ADD their list items"}, nil},
	}
	for _, c := range cases {
		d := firstError(validate.Validate(buildReg(t, c.srcs), defaultCfg()), "'+=' is not valid")
		if d == nil {
			t.Errorf("%s: expected an error", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(d.Message, w) {
				t.Errorf("%s: message missing %q:\n%s", c.name, w, d.Message)
			}
		}
		for _, w := range c.not {
			if strings.Contains(d.Message, w) {
				t.Errorf("%s: message must not contain %q:\n%s", c.name, w, d.Message)
			}
		}
	}
}

func TestRemoveOperatorIsAnError(t *testing.T) {
	reg := buildReg(t, map[string]string{
		"a.loom": "prompt A {\n  constraints :=\n    - a\n    - b\n}",
		"b.loom": "prompt B inherits A {\n  constraints -=\n    - b\n}",
	})
	d := firstError(validate.Validate(reg, defaultCfg()), "'-=' is not valid in v2")
	if d == nil || !strings.Contains(d.Message, "no direct replacement") || !strings.Contains(d.Message, "parent[0].constraints[1..3]") {
		t.Errorf("got %+v", d)
	}
	// On a scalar the dedicated rule reports it, once.
	reg = buildReg(t, map[string]string{"a.loom": "prompt A {\n  summary :=\n    x\n}\nprompt B inherits A {\n  summary -=\n    x\n}"})
	diags := validate.Validate(reg, defaultCfg())
	if got := countMentions(diags, "'-='"); got != 1 {
		t.Errorf("'-=' on a scalar should be reported exactly once, got %d: %v", got, diags)
	}
}

func TestBareColonWarnsOnceWithTheReplacement(t *testing.T) {
	// not inherited
	reg := buildReg(t, map[string]string{"a.loom": "prompt A {\n  persona:\n    x\n}"})
	diags := validate.Validate(reg, defaultCfg())
	if !hasWarning(diags, `Change "persona:" to "persona :="`) {
		t.Errorf("expected the bare-colon warning: %v", diags)
	}
	// a child that has a parent but does not redefine an inherited field still gets it
	reg = buildReg(t, map[string]string{
		"a.loom": "prompt A {\n  persona :=\n    x\n}",
		"b.loom": "prompt B inherits A {\n  objective:\n    y\n}",
	})
	if !hasWarning(validate.Validate(reg, defaultCfg()), `Change "objective:" to "objective :="`) {
		t.Error("a bare ':' on a NEW field of a child prompt must warn too")
	}
	// redefining an inherited field: the specific message only, not two warnings
	reg = buildReg(t, map[string]string{
		"a.loom": "prompt A {\n  persona :=\n    x\n}",
		"b.loom": "prompt B inherits A {\n  persona:\n    y\n}",
	})
	diags = validate.Validate(reg, defaultCfg())
	if n := countMentions(diags, `"persona"`); n != 1 {
		t.Errorf("expected a single warning for the redefinition, got %d: %v", n, diags)
	}
	// the modern operator is silent
	reg = buildReg(t, map[string]string{"a.loom": "prompt A {\n  persona :=\n    x\n}"})
	if hasWarning(validate.Validate(reg, defaultCfg()), "uses ':'") {
		t.Error("':=' must not warn")
	}
}

func TestFromExprInBlockIsError(t *testing.T) {
	// Verify we can parse a block with a normal := (no from()) without errors.
	reg := buildReg(t, map[string]string{
		"b.loom": `
block MyBlock {
  instructions :=
    - block instruction.
}`,
	})
	diags := validate.Validate(reg, defaultCfg())
	for _, d := range diags {
		if d.Sev == validate.Error {
			t.Errorf("clean block should have no errors, got: %s", d.Message)
		}
	}
}

// ---- duplicate prompt name ----

func TestDuplicatePromptName(t *testing.T) {
	reg := registry.New()
	nodes1, _ := parser.Parse("a.prompt", `
prompt Dup {
  objective:
    first
  format:
    - item
}`)
	nodes2, _ := parser.Parse("b.prompt", `
prompt Dup {
  objective:
    second
  format:
    - item
}`)

	_ = ast.KindPrompt // ensure ast is used
	if err := reg.Register(nodes1); err != nil {
		t.Fatalf("unexpected error registering first: %v", err)
	}
	if err := reg.Register(nodes2); err == nil {
		t.Error("expected duplicate error, got none")
	}
}
