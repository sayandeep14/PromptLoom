package resolve_test

import (
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// ---- test fixtures (spec example pack) ----

var specSources = map[string]string{
	"base.prompt": `
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
}`,

	"code-review.prompt": `
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
}`,

	"spring-boot-rules.prompt": `
block SpringBootRules {
  constraints:
    - Check controller, service, and repository separation.
    - Check transaction boundaries.
    - Check JPA entity mappings.
    - Check exception handling.
    - Check retry and timeout behavior for external calls.
}`,

	"spring-boot-review.prompt": `
prompt SpringBootReviewer inherits CodeReviewer {
  use SpringBootRules

  context:
    The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging.

  objective :=
    Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness.
}`,

	"test-writer.prompt": `
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
}`,
}

func buildSpecReg(t *testing.T) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for filename, src := range specSources {
		nodes, err := parser.Parse(filename, src)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatalf("register %s: %v", filename, err)
		}
	}
	return reg
}

// ---- BaseEngineer: root prompt, no inheritance ----

func TestResolveBaseEngineer(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("BaseEngineer", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rp.Summary != "General-purpose engineering assistant prompt." {
		t.Errorf("summary: got %q", rp.Summary)
	}
	if rp.Persona != "You are a senior software engineer who writes clear, maintainable, production-ready code." {
		t.Errorf("persona: got %q", rp.Persona)
	}
	if rp.Objective != "Help the user solve software engineering tasks with correctness, clarity, and practical judgment." {
		t.Errorf("objective: got %q", rp.Objective)
	}
	assertList(t, "constraints", rp.Constraints, []string{
		"Do not hallucinate APIs.",
		"Ask for missing information only when necessary.",
		"Prefer simple solutions before complex ones.",
		"Explain important trade-offs.",
	})
	assertList(t, "format", rp.Format, []string{
		"Summary", "Analysis", "Recommendation",
	})

	// No inheritance chain beyond itself
	if len(rp.InheritsChain) != 1 || rp.InheritsChain[0] != "BaseEngineer" {
		t.Errorf("InheritsChain: got %v", rp.InheritsChain)
	}
	if len(rp.UsedBlocks) != 0 {
		t.Errorf("UsedBlocks: expected none, got %v", rp.UsedBlocks)
	}
}

// ---- CodeReviewer: overrides objective, appends instructions and format ----

func TestResolveCodeReviewer(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("CodeReviewer", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inherited from BaseEngineer
	if rp.Summary != "General-purpose engineering assistant prompt." {
		t.Errorf("summary: got %q", rp.Summary)
	}

	// Overridden by CodeReviewer
	if rp.Objective != "Review the provided code for correctness, maintainability, readability, and production readiness." {
		t.Errorf("objective: got %q", rp.Objective)
	}

	// Appended by CodeReviewer (BaseEngineer had none)
	assertList(t, "instructions", rp.Instructions, []string{
		"Read the code carefully.",
		"Identify correctness issues.",
		"Identify maintainability issues.",
		"Suggest practical improvements.",
	})

	// Overridden by CodeReviewer
	assertList(t, "format", rp.Format, []string{
		"Summary", "Issues Found", "Suggested Fixes", "Final Recommendation",
	})

	// Constraints unchanged from BaseEngineer
	assertList(t, "constraints", rp.Constraints, []string{
		"Do not hallucinate APIs.",
		"Ask for missing information only when necessary.",
		"Prefer simple solutions before complex ones.",
		"Explain important trade-offs.",
	})

	assertChain(t, rp, []string{"BaseEngineer", "CodeReviewer"})
	if len(rp.UsedBlocks) != 0 {
		t.Errorf("UsedBlocks: expected none, got %v", rp.UsedBlocks)
	}
}

// ---- SpringBootReviewer: 3-level chain + block composition ----

func TestResolveSpringBootReviewer(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("SpringBootReviewer", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Context defined by SpringBootReviewer
	if rp.Context != "The project is a Spring Boot backend service using JPA, REST APIs, database access, and event-driven messaging." {
		t.Errorf("context: got %q", rp.Context)
	}

	// Objective overridden by SpringBootReviewer
	if rp.Objective != "Review the Spring Boot implementation for correctness, maintainability, data consistency, and production readiness." {
		t.Errorf("objective: got %q", rp.Objective)
	}

	// Constraints = BaseEngineer (4 items) + SpringBootRules block (5 items)
	assertList(t, "constraints", rp.Constraints, []string{
		"Do not hallucinate APIs.",
		"Ask for missing information only when necessary.",
		"Prefer simple solutions before complex ones.",
		"Explain important trade-offs.",
		"Check controller, service, and repository separation.",
		"Check transaction boundaries.",
		"Check JPA entity mappings.",
		"Check exception handling.",
		"Check retry and timeout behavior for external calls.",
	})

	// Format overridden by CodeReviewer
	assertList(t, "format", rp.Format, []string{
		"Summary", "Issues Found", "Suggested Fixes", "Final Recommendation",
	})

	assertChain(t, rp, []string{"BaseEngineer", "CodeReviewer", "SpringBootReviewer"})

	if len(rp.UsedBlocks) != 1 || rp.UsedBlocks[0] != "SpringBootRules" {
		t.Errorf("UsedBlocks: expected [SpringBootRules], got %v", rp.UsedBlocks)
	}
}

// ---- TestWriter: multiple list appends ----

func TestResolveTestWriter(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("TestWriter", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rp.Objective != "Generate useful tests for the provided code." {
		t.Errorf("objective: got %q", rp.Objective)
	}

	assertList(t, "instructions", rp.Instructions, []string{
		"Identify the behavior that needs to be tested.",
		"Cover success cases, failure cases, and edge cases.",
		"Prefer readable tests over overly clever tests.",
	})

	// constraints = BaseEngineer (4) + TestWriter append (2)
	assertList(t, "constraints", rp.Constraints, []string{
		"Do not hallucinate APIs.",
		"Ask for missing information only when necessary.",
		"Prefer simple solutions before complex ones.",
		"Explain important trade-offs.",
		"Do not change production code unless necessary.",
		"Use the testing framework appropriate for the project.",
	})

	assertList(t, "format", rp.Format, []string{
		"Test Strategy", "Test Cases", "Generated Test Code", "Notes",
	})
}

// ---- scalar += appends with double newline ----

func TestScalarAppend(t *testing.T) {
	reg := registry.New()
	nodes, _ := parser.Parse("t.prompt", `
prompt Base {
  context:
    First paragraph.
}

prompt Child inherits Base {
  context +=
    Second paragraph.
}`)
	_ = reg.Register(nodes)

	rp, err := resolve.Resolve("Child", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := "First paragraph.\n\nSecond paragraph."
	if rp.Context != expected {
		t.Errorf("context: expected %q, got %q", expected, rp.Context)
	}
}

// ---- list -= removes items by exact match ----

func TestListRemove(t *testing.T) {
	reg := registry.New()
	nodes, _ := parser.Parse("t.prompt", `
prompt Base {
  constraints:
    - Keep item A.
    - Remove item B.
    - Keep item C.
}

prompt Child inherits Base {
  constraints -=
    - Remove item B.
}`)
	_ = reg.Register(nodes)

	rp, err := resolve.Resolve("Child", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertList(t, "constraints", rp.Constraints, []string{"Keep item A.", "Keep item C."})
}

// ---- := fully replaces inherited list ----

func TestListOverride(t *testing.T) {
	reg := registry.New()
	nodes, _ := parser.Parse("t.prompt", `
prompt Base {
  format:
    - Old item 1.
    - Old item 2.
}

prompt Child inherits Base {
  format :=
    - New item 1.
    - New item 2.
    - New item 3.
}`)
	_ = reg.Register(nodes)

	rp, err := resolve.Resolve("Child", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertList(t, "format", rp.Format, []string{"New item 1.", "New item 2.", "New item 3."})
}

// ---- SourceTrace records last writer per field ----

func TestSourceTrace(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("SpringBootReviewer", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if rp.SourceTrace["objective"] != "SpringBootReviewer" {
		t.Errorf("trace objective: expected SpringBootReviewer, got %q", rp.SourceTrace["objective"])
	}
	if rp.SourceTrace["persona"] != "BaseEngineer" {
		t.Errorf("trace persona: expected BaseEngineer, got %q", rp.SourceTrace["persona"])
	}
	if rp.SourceTrace["context"] != "SpringBootReviewer" {
		t.Errorf("trace context: expected SpringBootReviewer, got %q", rp.SourceTrace["context"])
	}
	// constraints: last written by SpringBootRules block
	if rp.SourceTrace["constraints"] != "SpringBootRules" {
		t.Errorf("trace constraints: expected SpringBootRules, got %q", rp.SourceTrace["constraints"])
	}
	if rp.Fingerprint == "" || !strings.HasPrefix(rp.Fingerprint, "sha256:") {
		t.Errorf("expected fingerprint, got %q", rp.Fingerprint)
	}
}

func TestListSourcesTrackResolvedItems(t *testing.T) {
	reg := buildSpecReg(t)
	rp, err := resolve.Resolve("SpringBootReviewer", reg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(rp.ListSources["constraints"]) != len(rp.Constraints) {
		t.Fatalf("expected %d source entries, got %d", len(rp.Constraints), len(rp.ListSources["constraints"]))
	}
	found := false
	for _, contrib := range rp.ListSources["constraints"] {
		if contrib.Value == "Check transaction boundaries." {
			found = true
			if contrib.Source != "SpringBootRules" {
				t.Fatalf("expected SpringBootRules, got %q", contrib.Source)
			}
			if contrib.Pos.File != "spring-boot-rules.prompt" {
				t.Fatalf("expected spring-boot-rules.prompt, got %q", contrib.Pos.File)
			}
		}
	}
	if !found {
		t.Fatal("expected to find source contribution for 'Check transaction boundaries.'")
	}
}

// ---- error: unknown prompt ----

func TestResolveUnknownPrompt(t *testing.T) {
	reg := registry.New()
	_, err := resolve.Resolve("DoesNotExist", reg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestResolveVariablesVariantsAndOverlay(t *testing.T) {
	reg := registry.New()
	nodes, err := parser.Parse("language.prompt", `
prompt LanguageReviewer {
  var language = "Python"
  slot framework { required: true }

  context:
    Review {{ language }} services using {{ framework }}.

  constraints:
    - Prefer idiomatic {{ language }}.

  variant concise {
    constraints +=
      - Keep the review short.
  }
}`)
	if err != nil {
		t.Fatalf("parse prompt: %v", err)
	}
	if err := reg.Register(nodes); err != nil {
		t.Fatalf("register prompt: %v", err)
	}

	overlayNodes, err := parser.Parse("security-focus.overlay", `
overlay SecurityFocus {
  constraints +=
    - Focus on authentication.
}`)
	if err != nil {
		t.Fatalf("parse overlay: %v", err)
	}
	if err := reg.Register(overlayNodes); err != nil {
		t.Fatalf("register overlay: %v", err)
	}

	rp, err := resolve.ResolveWithOptions("LanguageReviewer", reg, resolve.Options{
		Variables: map[string]string{"framework": "Spring Boot", "language": "Go"},
		Variant:   "concise",
		Overlays:  []string{"security-focus"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if rp.Context != "Review Go services using Spring Boot." {
		t.Fatalf("unexpected context: %q", rp.Context)
	}
	assertList(t, "constraints", rp.Constraints, []string{
		"Prefer idiomatic Go.",
		"Keep the review short.",
		"Focus on authentication.",
	})
	if rp.AppliedVariant != "concise" {
		t.Fatalf("expected variant concise, got %q", rp.AppliedVariant)
	}
	if len(rp.AppliedOverlays) != 1 || rp.AppliedOverlays[0] != "SecurityFocus" {
		t.Fatalf("unexpected overlays: %v", rp.AppliedOverlays)
	}
	if len(rp.UnresolvedTokens) != 0 {
		t.Fatalf("expected all variables resolved, got %v", rp.UnresolvedTokens)
	}
}

// ---- helpers ----

func assertList(t *testing.T, name string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: expected %d items, got %d\n  want: %v\n  got:  %v", name, len(want), len(got), want, got)
		return
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%s[%d]: expected %q, got %q", name, i, want[i], got[i])
		}
	}
}

func assertChain(t *testing.T, rp *ast.ResolvedPrompt, want []string) {
	t.Helper()
	if len(rp.InheritsChain) != len(want) {
		t.Errorf("InheritsChain: expected %v, got %v", want, rp.InheritsChain)
		return
	}
	for i := range want {
		if rp.InheritsChain[i] != want[i] {
			t.Errorf("InheritsChain[%d]: expected %q, got %q", i, want[i], rp.InheritsChain[i])
		}
	}
}
