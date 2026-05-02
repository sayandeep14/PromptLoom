// Package format re-serialises parsed AST nodes into canonical source.
package format

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

// Nodes formats a slice of nodes (a full file) into canonical source.
func Nodes(nodes []*ast.Node) string {
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = Node(n)
	}
	return strings.Join(parts, "\n")
}

// Node formats a single prompt, block, or overlay node into canonical source.
func Node(n *ast.Node) string {
	var sb strings.Builder

	switch n.Kind {
	case ast.KindPrompt:
		if n.Parent != "" {
			fmt.Fprintf(&sb, "prompt %s inherits %s {\n", n.Name, n.Parent)
		} else {
			fmt.Fprintf(&sb, "prompt %s {\n", n.Name)
		}
	case ast.KindBlock:
		fmt.Fprintf(&sb, "block %s {\n", n.Name)
	case ast.KindOverlay:
		fmt.Fprintf(&sb, "overlay %s {\n", n.Name)
	}

	groups := renderBodyGroups(n)
	for i, group := range groups {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(group)
	}

	sb.WriteString("}\n")
	return sb.String()
}

func renderBodyGroups(n *ast.Node) []string {
	var groups []string

	if n.Kind == ast.KindPrompt && len(n.Vars) > 0 {
		var lines []string
		for _, v := range n.Vars {
			if v.IsSlot {
				lines = append(lines, "  "+formatSlot(v))
			} else {
				lines = append(lines, "  "+formatVar(v))
			}
		}
		groups = append(groups, strings.Join(lines, "\n")+"\n")
	}

	if len(n.Uses) > 0 {
		var lines []string
		for _, use := range n.Uses {
			lines = append(lines, "  use "+use)
		}
		groups = append(groups, strings.Join(lines, "\n")+"\n")
	}

	if len(n.Fields) > 0 {
		groups = append(groups, formatFieldOps(n.Fields, 2))
	}

	if n.Kind == ast.KindPrompt {
		for _, variant := range n.Variants {
			var sb strings.Builder
			fmt.Fprintf(&sb, "  variant %s {\n", variant.Name)
			sb.WriteString(formatFieldOps(variant.Fields, 4))
			sb.WriteString("  }\n")
			groups = append(groups, sb.String())
		}

		if n.Contract != nil {
			var sb strings.Builder
			sb.WriteString("  contract {\n")
			sb.WriteString(formatListField("required_sections", n.Contract.RequiredSections, 4))
			sb.WriteString(formatListField("forbidden_sections", n.Contract.ForbiddenSections, 4))
			sb.WriteString(formatListField("must_include", n.Contract.MustInclude, 4))
			sb.WriteString(formatListField("must_not_include", n.Contract.MustNotInclude, 4))
			sb.WriteString("  }\n")
			groups = appendNonEmpty(groups, sb.String(), "  contract {\n  }\n")
		}

		if n.Capabilities != nil {
			var sb strings.Builder
			sb.WriteString("  capabilities {\n")
			sb.WriteString(formatListField("allowed", n.Capabilities.Allowed, 4))
			sb.WriteString(formatListField("forbidden", n.Capabilities.Forbidden, 4))
			sb.WriteString("  }\n")
			groups = appendNonEmpty(groups, sb.String(), "  capabilities {\n  }\n")
		}
	}

	return groups
}

func appendNonEmpty(groups []string, value, emptySentinel string) []string {
	if value == emptySentinel {
		return groups
	}
	return append(groups, value)
}

func formatVar(v ast.VarDecl) string {
	return fmt.Sprintf("var %s = %s", v.Name, strconv.Quote(v.Default))
}

func formatSlot(v ast.VarDecl) string {
	if v.Default != "" {
		return fmt.Sprintf("slot %s { default: %s }", v.Name, strconv.Quote(v.Default))
	}
	if v.Required {
		return fmt.Sprintf("slot %s { required: true }", v.Name)
	}
	return fmt.Sprintf("slot %s {}", v.Name)
}

func formatFieldOps(fields []ast.FieldOperation, indent int) string {
	var sb strings.Builder
	prefix := strings.Repeat(" ", indent)
	bodyPrefix := strings.Repeat(" ", indent+2)

	for i, f := range fields {
		sb.WriteString(prefix)
		sb.WriteString(f.FieldName)
		sb.WriteString(opSuffix(f.Op))
		sb.WriteString("\n")
		for _, line := range f.Value {
			sb.WriteString(bodyPrefix)
			sb.WriteString(line)
			sb.WriteString("\n")
		}
		if i < len(fields)-1 {
			sb.WriteString("\n")
		}
	}

	return sb.String()
}

func formatListField(name string, values []string, indent int) string {
	if len(values) == 0 {
		return ""
	}
	field := ast.FieldOperation{
		FieldName: name,
		Op:        ast.OpDefine,
	}
	for _, value := range values {
		field.Value = append(field.Value, "- "+value)
	}
	return formatFieldOps([]ast.FieldOperation{field}, indent)
}

// opSuffix returns the operator portion of a field declaration line.
func opSuffix(op ast.Operator) string {
	switch op {
	case ast.OpDefine:
		return ":"
	case ast.OpOverride:
		return " :="
	case ast.OpAppend:
		return " +="
	case ast.OpRemove:
		return " -="
	}
	return ":"
}
