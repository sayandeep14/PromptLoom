// Package mcp generates MCP-compatible tool manifests from resolved prompt metadata.
package mcp

import (
	"encoding/json"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
)

// Property is one JSON-Schema property in an inputSchema.
type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

// InputSchema is the JSON-Schema object for a tool's inputs.
type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

// Tool is one MCP tool definition.
type Tool struct {
	Name         string      `json:"name"`
	Description  string      `json:"description,omitempty"`
	InputSchema  InputSchema `json:"inputSchema"`
	Capabilities []string    `json:"capabilities,omitempty"`
	Forbidden    []string    `json:"forbidden,omitempty"`
}

// Manifest is the top-level MCP manifest.
type Manifest struct {
	Tools []Tool `json:"tools"`
}

// Generate produces an MCP manifest for a single named prompt.
func Generate(name string, reg *registry.Registry) (*Manifest, []string, error) {
	node, ok := reg.LookupPrompt(name)
	if !ok {
		// Try block
		node, ok = reg.LookupBlock(name)
		if !ok {
			return nil, nil, nil
		}
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return nil, nil, err
	}

	tool, warnings := buildTool(name, node, rp)
	return &Manifest{Tools: []Tool{tool}}, warnings, nil
}

// GenerateAll produces an MCP manifest for all prompts in the registry.
func GenerateAll(reg *registry.Registry) (*Manifest, []string, error) {
	var tools []Tool
	var allWarnings []string

	for _, node := range reg.Prompts() {
		rp, err := resolve.Resolve(node.Name, reg)
		if err != nil {
			allWarnings = append(allWarnings, node.Name+": resolve error: "+err.Error())
			continue
		}
		tool, warnings := buildTool(node.Name, node, rp)
		tools = append(tools, tool)
		allWarnings = append(allWarnings, warnings...)
	}
	return &Manifest{Tools: tools}, allWarnings, nil
}

// MarshalJSON encodes the manifest as indented JSON.
func (m *Manifest) MarshalJSON() ([]byte, error) {
	type alias Manifest
	return json.MarshalIndent((*alias)(m), "", "  ")
}

func buildTool(name string, node *ast.Node, rp *ast.ResolvedPrompt) (Tool, []string) {
	var warnings []string

	tool := Tool{
		Name:        toKebab(name),
		Description: rp.Summary,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}

	// Slots → inputSchema properties.
	for _, v := range node.Vars {
		if !v.IsSlot {
			continue
		}
		desc := ""
		if v.Secret {
			desc = "(secret)"
		}
		tool.InputSchema.Properties[v.Name] = Property{
			Type:        "string",
			Description: desc,
		}
		if v.Required {
			tool.InputSchema.Required = append(tool.InputSchema.Required, v.Name)
		}
	}

	// Capabilities block → capabilities and forbidden lists.
	if node.Capabilities != nil {
		tool.Capabilities = node.Capabilities.Allowed
		tool.Forbidden = node.Capabilities.Forbidden
	} else {
		warnings = append(warnings, name+": no capabilities {} block declared")
	}

	// Supplement description from objective if summary is absent.
	if tool.Description == "" && rp.Objective != "" {
		tool.Description = rp.Objective
	}
	if node.Contract == nil {
		warnings = append(warnings, name+": no contract {} block declared")
	}

	return tool, warnings
}

func toKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
