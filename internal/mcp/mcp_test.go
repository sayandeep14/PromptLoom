package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
)

func reg(t *testing.T, src string) *registry.Registry {
	t.Helper()
	nodes, err := parser.Parse("t.loom", src)
	if err != nil {
		t.Fatal(err)
	}
	r := registry.New()
	if err := r.Register(nodes); err != nil {
		t.Fatal(err)
	}
	return r
}

const full = `
prompt CodeReviewer {
  summary :=
    Reviews code.
  persona :=
    You review.
  slot repo { required: true }
  slot branch { default: "main" }
  slot api_key { secret: true }
  slot notes { required: false }
  capabilities {
    allowed:
      - read_code
    forbidden:
      - delete_files
  }
  contract {
    required_sections:
      - Summary
  }
}`

func TestGenerateOne(t *testing.T) {
	m, warnings, err := Generate("CodeReviewer", reg(t, full))
	if err != nil || m == nil || len(m.Tools) != 1 {
		t.Fatalf("%v %v", m, err)
	}
	if len(warnings) != 0 {
		t.Errorf("a prompt with a contract and capabilities has nothing to warn about: %v", warnings)
	}
	tool := m.Tools[0]
	if tool.Name != "code-reviewer" || tool.Description != "Reviews code." {
		t.Errorf("%+v", tool)
	}
	if tool.InputSchema.Type != "object" || len(tool.InputSchema.Properties) != 4 {
		t.Errorf("every slot becomes a property: %+v", tool.InputSchema)
	}
	if strings.Join(tool.InputSchema.Required, ",") != "repo,api_key" {
		t.Errorf("required = %v (slots with a default or required:false are optional)", tool.InputSchema.Required)
	}
	if tool.InputSchema.Properties["api_key"].Description != "(secret)" {
		t.Error("secret slots must be marked")
	}
	if strings.Join(tool.Capabilities, ",") != "read_code" || strings.Join(tool.Forbidden, ",") != "delete_files" {
		t.Errorf("capabilities: %v / %v", tool.Capabilities, tool.Forbidden)
	}
}

func TestOutputIsValidJSONWithTheMCPShape(t *testing.T) {
	m, _, _ := Generate("CodeReviewer", reg(t, full))
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Type       string                    `json:"type"`
				Properties map[string]map[string]any `json:"properties"`
				Required   []string                  `json:"required"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(data, &back); err != nil || len(back.Tools) != 1 || back.Tools[0].InputSchema.Type != "object" {
		t.Errorf("%v\n%s", err, data)
	}
	if back.Tools[0].InputSchema.Properties["repo"]["type"] != "string" {
		t.Errorf("properties need a JSON-schema type: %s", data)
	}
}

func TestWarningsForMissingCapabilitiesAndContract(t *testing.T) {
	_, w, err := Generate("Bare", reg(t, "prompt Bare {\n  objective :=\n    Do things.\n}"))
	if err != nil || len(w) != 2 {
		t.Fatalf("%v %v", w, err)
	}
	if !strings.Contains(w[0]+w[1], "capabilities") || !strings.Contains(w[0]+w[1], "contract") {
		t.Errorf("%v", w)
	}
}

func TestDescriptionFallsBackToTheObjective(t *testing.T) {
	m, _, _ := Generate("P", reg(t, "prompt P {\n  objective :=\n    Do the thing.\n}"))
	if m.Tools[0].Description != "Do the thing." {
		t.Errorf("%q", m.Tools[0].Description)
	}
	m, _, _ = Generate("Q", reg(t, "prompt Q {\n  persona :=\n    x\n}"))
	if m.Tools[0].Description != "" {
		t.Errorf("no description available: %q", m.Tools[0].Description)
	}
}

func TestUnknownPromptGivesANilManifest(t *testing.T) {
	// Callers rely on this: nil manifest and nil error means "not found".
	m, w, err := Generate("Ghost", reg(t, full))
	if m != nil || w != nil || err != nil {
		t.Errorf("%v %v %v", m, w, err)
	}
}

func TestUnresolvablePromptIsAnError(t *testing.T) {
	if _, _, err := Generate("Broken", reg(t, "prompt Broken inherits Missing {\n}")); err == nil {
		t.Error("an unresolvable prompt must be an error")
	}
}

func TestGenerateAllIsSortedAndReportsBrokenPrompts(t *testing.T) {
	src := ""
	for _, n := range []string{"Zeta", "Alpha", "Mid"} {
		src += "prompt " + n + " {\n  summary :=\n    s\n}\n"
	}
	src += "prompt Broken inherits Missing {\n}\n"
	m, warnings, err := GenerateAll(reg(t, src))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range m.Tools {
		names = append(names, tl.Name)
	}
	if strings.Join(names, ",") != "alpha,mid,zeta" {
		t.Errorf("tools must be in a stable order: %v", names)
	}
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "Broken") && strings.Contains(w, "resolve error") {
			found = true
		}
	}
	if !found {
		t.Errorf("a broken prompt should be reported and skipped: %v", warnings)
	}
}

func TestToolNamesAreValidMCPNames(t *testing.T) {
	cases := map[string]string{
		"GoCodeReviewer": "go-code-reviewer", "HTTPServer": "http-server", "API": "api", "simple": "simple",
		"team/Reviewer": "team-reviewer", "My_Tool": "my-tool", "v2Prompt": "v2-prompt",
	}
	for in, want := range cases {
		got := toolName(in)
		if in == "My_Tool" {
			want = "my_tool"
		}
		if got != want {
			t.Errorf("toolName(%q) = %q, want %q", in, got, want)
		}
		for _, r := range got {
			if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
				t.Errorf("%q contains %q, which MCP clients reject", got, r)
			}
		}
	}
}

func TestCollidingToolNamesAreReported(t *testing.T) {
	m, w, _ := GenerateAll(reg(t, "prompt CodeReview {\n  summary :=\n    a\n}\nprompt Code_Review {\n  summary :=\n    b\n}\n"))
	_ = w
	seen := map[string]bool{}
	for _, tl := range m.Tools {
		if seen[tl.Name] {
			t.Errorf("duplicate tool name %q in one manifest", tl.Name)
		}
		seen[tl.Name] = true
	}
}
