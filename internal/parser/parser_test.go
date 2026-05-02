package parser_test

import (
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/parser"
)

// ---- spec example sources ----

const srcBaseEngineer = `
prompt BaseEngineer {
  summary:
    General-purpose engineering assistant prompt.

  persona:
    You are a senior software engineer who writes clear, maintainable, production-ready code.

  objective:
    Help the user solve software engineering tasks with correctness, clarity, and practical judgment.

  constraints:
    - Do not hallucinate APIs.
    - Ask for missing information only when necessary.
    - Prefer simple solutions before complex ones.
    - Explain important trade-offs.

  format:
    - Summary
    - Analysis
    - Recommendation
}
`

const srcCodeReviewer = `
prompt CodeReviewer inherits BaseEngineer {
  objective :=
    Review the provided code for correctness, maintainability, readability, and production readiness.

  instructions +=
    - Read the code carefully.
    - Identify correctness issues.
    - Identify maintainability issues.
    - Suggest practical improvements.

  format :=
    - Summary
    - Issues Found
    - Suggested Fixes
    - Final Recommendation
}
`

const srcSpringBootRules = `
block SpringBootRules {
  constraints:
    - Check controller, service, and repository separation.
    - Check transaction boundaries.
    - Check JPA entity mappings.
    - Check exception handling.
    - Check retry and timeout behavior for external calls.
}
`

const srcSpringBootReviewer = `
prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules

  context:
    The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging.

  objective :=
    Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness.
}
`

const srcTestWriter = `
prompt TestWriter inherits BaseEngineer {
  objective :=
    Generate useful tests for the provided code.

  instructions +=
    - Identify the behavior that needs to be tested.
    - Cover success cases, failure cases, and edge cases.
    - Prefer readable tests over overly clever tests.

  constraints +=
    - Do not change production code unless necessary.
    - Use the testing framework appropriate for the project.

  format :=
    - Test Strategy
    - Test Cases
    - Generated Test Code
    - Notes
}
`

// ---- helpers ----

func mustParse(t *testing.T, filename, src string) []*ast.Node {
	t.Helper()
	nodes, err := parser.Parse(filename, src)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	return nodes
}

func expectError(t *testing.T, src string) {
	t.Helper()
	_, err := parser.Parse("test.prompt", src)
	if err == nil {
		t.Fatal("expected error but got none")
	}
}

// ---- spec example tests ----

func TestBaseEngineer(t *testing.T) {
	nodes := mustParse(t, "base.prompt", srcBaseEngineer)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if n.Kind != ast.KindPrompt {
		t.Errorf("expected KindPrompt, got %v", n.Kind)
	}
	if n.Name != "BaseEngineer" {
		t.Errorf("expected name BaseEngineer, got %q", n.Name)
	}
	if n.Parent != "" {
		t.Errorf("expected no parent, got %q", n.Parent)
	}
	if len(n.Uses) != 0 {
		t.Errorf("expected no uses, got %v", n.Uses)
	}

	fieldMap := fieldsByName(n)

	assertField(t, fieldMap, "summary", ast.OpDefine, []string{
		"General-purpose engineering assistant prompt.",
	})
	assertField(t, fieldMap, "persona", ast.OpDefine, []string{
		"You are a senior software engineer who writes clear, maintainable, production-ready code.",
	})
	assertField(t, fieldMap, "constraints", ast.OpDefine, []string{
		"- Do not hallucinate APIs.",
		"- Ask for missing information only when necessary.",
		"- Prefer simple solutions before complex ones.",
		"- Explain important trade-offs.",
	})
	assertField(t, fieldMap, "format", ast.OpDefine, []string{
		"- Summary",
		"- Analysis",
		"- Recommendation",
	})
}

func TestCodeReviewer(t *testing.T) {
	nodes := mustParse(t, "code-review.prompt", srcCodeReviewer)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if n.Name != "CodeReviewer" {
		t.Errorf("expected name CodeReviewer, got %q", n.Name)
	}
	if n.Parent != "BaseEngineer" {
		t.Errorf("expected parent BaseEngineer, got %q", n.Parent)
	}

	fieldMap := fieldsByName(n)

	assertField(t, fieldMap, "objective", ast.OpOverride, []string{
		"Review the provided code for correctness, maintainability, readability, and production readiness.",
	})
	assertField(t, fieldMap, "instructions", ast.OpAppend, []string{
		"- Read the code carefully.",
		"- Identify correctness issues.",
		"- Identify maintainability issues.",
		"- Suggest practical improvements.",
	})
	assertField(t, fieldMap, "format", ast.OpOverride, []string{
		"- Summary",
		"- Issues Found",
		"- Suggested Fixes",
		"- Final Recommendation",
	})
}

func TestSpringBootRulesBlock(t *testing.T) {
	nodes := mustParse(t, "spring-boot-rules.prompt", srcSpringBootRules)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if n.Kind != ast.KindBlock {
		t.Errorf("expected KindBlock, got %v", n.Kind)
	}
	if n.Name != "SpringBootRules" {
		t.Errorf("expected name SpringBootRules, got %q", n.Name)
	}

	fieldMap := fieldsByName(n)
	assertField(t, fieldMap, "constraints", ast.OpDefine, []string{
		"- Check controller, service, and repository separation.",
		"- Check transaction boundaries.",
		"- Check JPA entity mappings.",
		"- Check exception handling.",
		"- Check retry and timeout behavior for external calls.",
	})
}

func TestSpringBootReviewer(t *testing.T) {
	nodes := mustParse(t, "spring-boot-review.prompt", srcSpringBootReviewer)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if n.Name != "SpringBootReviewer" {
		t.Errorf("expected name SpringBootReviewer, got %q", n.Name)
	}
	if n.Parent != "CodeReviewer" {
		t.Errorf("expected parent CodeReviewer, got %q", n.Parent)
	}
	if len(n.Uses) != 1 || n.Uses[0] != "SpringBootRules" {
		t.Errorf("expected uses [SpringBootRules], got %v", n.Uses)
	}

	fieldMap := fieldsByName(n)
	assertField(t, fieldMap, "context", ast.OpDefine, []string{
		"The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging.",
	})
	assertField(t, fieldMap, "objective", ast.OpOverride, []string{
		"Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness.",
	})
}

func TestTestWriter(t *testing.T) {
	nodes := mustParse(t, "test-writer.prompt", srcTestWriter)

	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]

	if n.Name != "TestWriter" {
		t.Errorf("expected name TestWriter, got %q", n.Name)
	}
	if n.Parent != "BaseEngineer" {
		t.Errorf("expected parent BaseEngineer, got %q", n.Parent)
	}

	fieldMap := fieldsByName(n)
	assertField(t, fieldMap, "instructions", ast.OpAppend, []string{
		"- Identify the behavior that needs to be tested.",
		"- Cover success cases, failure cases, and edge cases.",
		"- Prefer readable tests over overly clever tests.",
	})
	assertField(t, fieldMap, "constraints", ast.OpAppend, []string{
		"- Do not change production code unless necessary.",
		"- Use the testing framework appropriate for the project.",
	})
}

// TestMultipleNodesInOneFile ensures a file with multiple declarations parses correctly.
func TestMultipleNodesInOneFile(t *testing.T) {
	src := `
block BlockA {
  constraints:
    - item one
}

block BlockB {
  instructions:
    - step one
}
`
	nodes := mustParse(t, "multi.prompt", src)
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Name != "BlockA" || nodes[1].Name != "BlockB" {
		t.Errorf("unexpected names: %q, %q", nodes[0].Name, nodes[1].Name)
	}
}

func TestParseVarsVariantsAndOverlay(t *testing.T) {
	promptSrc := `
prompt LanguageReviewer {
  var language = "Python"
  slot framework { required: true }

  context:
    Review {{ language }} services using {{ framework }}.

  variant concise {
    constraints +=
      - Keep the review short.
  }
}
`
	nodes := mustParse(t, "language.prompt", promptSrc)
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if len(n.Vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(n.Vars))
	}
	if n.Vars[0].Name != "language" || n.Vars[0].Default != "Python" || n.Vars[0].Required {
		t.Fatalf("unexpected var declaration: %+v", n.Vars[0])
	}
	if n.Vars[1].Name != "framework" || !n.Vars[1].IsSlot || !n.Vars[1].Required {
		t.Fatalf("unexpected slot declaration: %+v", n.Vars[1])
	}
	if len(n.Variants) != 1 || n.Variants[0].Name != "concise" {
		t.Fatalf("expected concise variant, got %+v", n.Variants)
	}
	assertField(t, fieldsByName(n), "context", ast.OpDefine, []string{
		"Review {{ language }} services using {{ framework }}.",
	})

	overlaySrc := `
overlay SecurityFocus {
  constraints +=
    - Focus on authentication.
}`
	overlayNodes := mustParse(t, "security-focus.overlay", overlaySrc)
	if len(overlayNodes) != 1 || overlayNodes[0].Kind != ast.KindOverlay {
		t.Fatalf("expected one overlay node, got %+v", overlayNodes)
	}
}

// ---- error path tests ----

func TestErrorMissingBrace(t *testing.T) {
	expectError(t, `prompt Foo {
  summary:
    some content
`)
}

func TestErrorUnknownTopLevel(t *testing.T) {
	expectError(t, `foo Bar { }`)
}

func TestErrorInvalidOperator(t *testing.T) {
	expectError(t, `prompt Foo {
  summary =+
    bad
}`)
}

func TestErrorUnexpectedBodyToken(t *testing.T) {
	expectError(t, `prompt Foo {
  {nested}
}`)
}

// ---- helpers ----

func fieldsByName(n *ast.Node) map[string]ast.FieldOperation {
	m := make(map[string]ast.FieldOperation)
	for _, f := range n.Fields {
		m[f.FieldName] = f
	}
	return m
}

func assertField(t *testing.T, fields map[string]ast.FieldOperation, name string, op ast.Operator, value []string) {
	t.Helper()
	f, ok := fields[name]
	if !ok {
		t.Errorf("field %q not found", name)
		return
	}
	if f.Op != op {
		t.Errorf("field %q: expected op %v, got %v", name, op, f.Op)
	}
	if len(f.Value) != len(value) {
		t.Errorf("field %q: expected %d value lines, got %d: %v", name, len(value), len(f.Value), f.Value)
		return
	}
	for i, v := range value {
		if f.Value[i] != v {
			t.Errorf("field %q value[%d]: expected %q, got %q", name, i, v, f.Value[i])
		}
	}
}
