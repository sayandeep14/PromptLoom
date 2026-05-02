package render_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/render"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

func buildReg(t *testing.T, sources map[string]string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for filename, src := range sources {
		nodes, err := parser.Parse(filename, src)
		if err != nil {
			t.Fatalf("parse %s: %v", filename, err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	return reg
}

func mustResolve(t *testing.T, name string, reg *registry.Registry) *ast.ResolvedPrompt {
	t.Helper()
	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	return rp
}

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
}

// ---- section presence ----

func TestBaseEngineerSections(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "BaseEngineer", reg)
	out := render.Render(rp, config.Defaults())

	assertContains(t, out, "# BaseEngineer")
	assertContains(t, out, "## Summary")
	assertContains(t, out, "General-purpose engineering assistant prompt.")
	assertContains(t, out, "## Persona")
	assertContains(t, out, "## Objective")
	assertContains(t, out, "## Constraints")
	assertContains(t, out, "- Do not hallucinate APIs.")
	assertContains(t, out, "## Output Format") // "format" → "Output Format"
	assertContains(t, out, "- Summary")
	assertContains(t, out, "- Analysis")
	assertContains(t, out, "- Recommendation")

	// Instructions is empty for BaseEngineer — section should be absent.
	assertAbsent(t, out, "## Instructions")
	// Context is empty — section should be absent.
	assertAbsent(t, out, "## Context")
}

// ---- format field maps to "Output Format" not "Format" ----

func TestFormatHeadingIsOutputFormat(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "BaseEngineer", reg)
	out := render.Render(rp, config.Defaults())

	assertAbsent(t, out, "## Format\n")
	assertContains(t, out, "## Output Format")
}

// ---- list items rendered as bullets ----

func TestListFieldsAsBullets(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "SpringBootReviewer", reg)
	out := render.Render(rp, config.Defaults())

	// All 9 constraint items should appear as bullets.
	assertContains(t, out, "- Do not hallucinate APIs.")
	assertContains(t, out, "- Check transaction boundaries.")
	assertContains(t, out, "- Check JPA entity mappings.")
}

// ---- canonical section order ----

func TestSectionOrder(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "SpringBootReviewer", reg)
	out := render.Render(rp, config.Defaults())

	order := []string{
		"## Summary",
		"## Persona",
		"## Context",
		"## Objective",
		"## Instructions",
		"## Constraints",
		"## Output Format",
	}
	assertOrder(t, out, order)
}

// ---- empty sections are skipped ----

func TestEmptySectionsSkipped(t *testing.T) {
	reg := registry.New()
	nodes, _ := parser.Parse("t.prompt", `
prompt Minimal {
  objective:
    Do the thing.
  format:
    - Result
}`)
	_ = reg.Register(nodes)
	rp := mustResolve(t, "Minimal", reg)
	out := render.Render(rp, config.Defaults())

	assertAbsent(t, out, "## Summary")
	assertAbsent(t, out, "## Persona")
	assertAbsent(t, out, "## Context")
	assertAbsent(t, out, "## Instructions")
	assertAbsent(t, out, "## Constraints")
	assertAbsent(t, out, "## Examples")
	assertAbsent(t, out, "## Notes")
	assertContains(t, out, "## Objective")
	assertContains(t, out, "## Output Format")
}

// ---- scalar += renders as two paragraphs ----

func TestScalarAppendRendersAsParagraphs(t *testing.T) {
	reg := registry.New()
	nodes, _ := parser.Parse("t.prompt", `
prompt Base {
  context:
    First paragraph about the project.
}
prompt Child inherits Base {
  context +=
    Second paragraph with more detail.
}`)
	_ = reg.Register(nodes)
	rp := mustResolve(t, "Child", reg)
	out := render.Render(rp, config.Defaults())

	assertContains(t, out, "First paragraph about the project.\n\nSecond paragraph with more detail.")
}

// ---- front matter when include_metadata = true ----

func TestFrontMatterIncluded(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "SpringBootReviewer", reg)

	cfg := config.Defaults()
	cfg.Render.IncludeMetadata = true
	cfg.Render.IncludeFingerprint = true
	out := render.Render(rp, cfg)

	assertContains(t, out, "---")
	assertContains(t, out, "name: SpringBootReviewer")
	assertContains(t, out, "inherits:")
	assertContains(t, out, "  - BaseEngineer")
	assertContains(t, out, "  - CodeReviewer")
	assertContains(t, out, "blocks:")
	assertContains(t, out, "  - SpringBootRules")
	assertContains(t, out, "fingerprint: sha256:")
	// Front matter must come before the heading.
	assertOrder(t, out, []string{"---", "# SpringBootReviewer"})
}

// ---- front matter absent by default ----

func TestFrontMatterAbsentByDefault(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "BaseEngineer", reg)
	out := render.Render(rp, config.Defaults())

	// Default config has include_metadata = false.
	if strings.HasPrefix(out, "---") {
		t.Error("expected no front matter by default, but output starts with ---")
	}
	if strings.HasPrefix(out, "# BaseEngineer") {
		// Good — heading is first.
	}
}

// ---- SpringBootReviewer full output matches spec §12 ----

func TestSpringBootReviewerFullOutput(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "SpringBootReviewer", reg)
	out := render.Render(rp, config.Defaults())

	wantSections := []string{
		"# SpringBootReviewer",
		"## Summary",
		"General-purpose engineering assistant prompt.",
		"## Persona",
		"You are a senior software engineer",
		"## Context",
		"Spring Boot backend service",
		"## Objective",
		"Review the Spring Boot implementation",
		"## Instructions",
		"- Read the code carefully.",
		"## Constraints",
		"- Do not hallucinate APIs.",
		"- Check controller, service, and repository separation.",
		"- Check JPA entity mappings.",
		"## Output Format",
		"- Summary",
		"- Issues Found",
		"- Suggested Fixes",
		"- Final Recommendation",
	}
	for _, want := range wantSections {
		assertContains(t, out, want)
	}
}

func TestRenderFormatJSONAnthropic(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "BaseEngineer", reg)

	out, format, err := render.RenderFormat(rp, config.Defaults(), "json-anthropic")
	if err != nil {
		t.Fatalf("RenderFormat: %v", err)
	}
	if format.ID != "json-anthropic" {
		t.Fatalf("format ID: got %q", format.ID)
	}

	var payload map[string]string
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("unmarshal json-anthropic: %v", err)
	}
	assertContains(t, payload["system"], "# BaseEngineer")
	assertContains(t, payload["system"], "## Objective")
}

func TestRenderFormatCursorRuleForcesFrontMatter(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "SpringBootReviewer", reg)

	cfg := config.Defaults()
	cfg.Render.IncludeMetadata = false

	out, format, err := render.RenderFormat(rp, cfg, "cursor-rule")
	if err != nil {
		t.Fatalf("RenderFormat: %v", err)
	}
	if format.DefaultFileName("CodeAssistant") != "CodeAssistant.mdc" {
		t.Fatalf("unexpected cursor-rule filename: %q", format.DefaultFileName("CodeAssistant"))
	}
	if !strings.HasPrefix(out, "---\n") {
		t.Fatalf("cursor-rule should include front matter, got:\n%s", out)
	}
	assertContains(t, out, "description:")
	assertContains(t, out, "globs: []")
	assertContains(t, out, "alwaysApply: false")
	// cursor-rule should NOT include loom-specific metadata like "name:"
	if strings.Contains(out, "name: SpringBootReviewer") {
		t.Fatalf("cursor-rule should not include loom name metadata, got:\n%s", out)
	}
}

func TestRenderFormatPlain(t *testing.T) {
	reg := buildReg(t, specSources)
	rp := mustResolve(t, "BaseEngineer", reg)

	out, format, err := render.RenderFormat(rp, config.Defaults(), "plain")
	if err != nil {
		t.Fatalf("RenderFormat: %v", err)
	}
	if format.DefaultFileName("BaseEngineer") != "BaseEngineer.txt" {
		t.Fatalf("unexpected plain filename: %q", format.DefaultFileName("BaseEngineer"))
	}
	assertContains(t, out, "BaseEngineer")
	assertContains(t, out, "Objective:")
	assertAbsent(t, out, "## Objective")
}

// ---- helpers ----

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected output to contain %q\n\nFull output:\n%s", needle, haystack)
	}
}

func assertAbsent(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("expected output NOT to contain %q", needle)
	}
}

// assertOrder checks that all strings in order appear in haystack in sequence.
func assertOrder(t *testing.T, haystack string, order []string) {
	t.Helper()
	pos := 0
	for _, s := range order {
		idx := strings.Index(haystack[pos:], s)
		if idx == -1 {
			t.Errorf("expected %q to appear after position %d in output\n\nFull output:\n%s", s, pos, haystack)
			return
		}
		pos += idx + len(s)
	}
}
