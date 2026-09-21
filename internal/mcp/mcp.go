// Package mcp generates MCP-compatible tool manifests from resolved prompt metadata.
package mcp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
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

	// Stable order: the manifest is meant to be committed and diffed.
	nodes := reg.Prompts()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })

	seen := map[string]string{}
	for _, node := range nodes {
		rp, err := resolve.Resolve(node.Name, reg)
		if err != nil {
			allWarnings = append(allWarnings, node.Name+": resolve error: "+err.Error())
			continue
		}
		tool, warnings := buildTool(node.Name, node, rp)
		if other, dup := seen[tool.Name]; dup {
			allWarnings = append(allWarnings, fmt.Sprintf("%s: tool name %q is already used by %s; skipped", node.Name, tool.Name, other))
			continue
		}
		seen[tool.Name] = node.Name
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
		Name:        toolName(name),
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

// toolName makes a prompt name a valid MCP tool name ([a-z0-9_-] only): namespaced names such
// as "team/Reviewer" would otherwise carry a "/" that clients reject.
func toolName(s string) string {
	var b strings.Builder
	sep := byte(0) // pending separator: '_' if the run contained one, else '-'
	for _, r := range toKebab(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if sep != 0 && b.Len() > 0 {
				b.WriteByte(sep)
			}
			sep = 0
			b.WriteRune(r)
		case r == '_':
			sep = '_'
		default: // '-', '/', '.', spaces, anything else
			if sep == 0 {
				sep = '-'
			}
		}
	}
	return b.String()
}

func toKebab(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				prevLower := runes[i-1] >= 'a' && runes[i-1] <= 'z'
				nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
				if prevLower || nextLower {
					b.WriteByte('-')
				}
			}
			b.WriteRune(r + 32)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
