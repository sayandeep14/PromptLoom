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
		if len(n.Parents) > 0 {
			fmt.Fprintf(&sb, "prompt %s inherits %s {\n", n.Name, strings.Join(n.Parents, ", "))
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

	if len(n.Tags) > 0 {
		groups = append(groups, "  tags: "+strings.Join(n.Tags, ", ")+"\n")
	}

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

		if f.FromExpr != nil {
			simplified := simplifyFromExpr(f.FromExpr)
			sb.WriteString(formatFromExpr(simplified, bodyPrefix))
		} else {
			for _, line := range f.Value {
				sb.WriteString(bodyPrefix)
				sb.WriteString(line)
				sb.WriteString("\n")
			}
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

// ── from() expression serialisation ──────────────────────────────────────────

// formatFromExpr serialises a (possibly simplified) from() expression into
// canonical indented source.
//
// Layout rules:
//   - If there are no FromLiteral units: one line at bodyPrefix.
//   - If there is a literal block: non-literal units first, then `and {`,
//     items at bodyPrefix+2, closing `}` at bodyPrefix.
func formatFromExpr(fe *ast.FromExpression, bodyPrefix string) string {
	if fe == nil || len(fe.Units) == 0 {
		return ""
	}

	var nonLiteralParts []string
	var literalItems []string

	for _, unit := range fe.Units {
		if unit.Kind == ast.FromLiteral {
			for _, item := range unit.Items {
				literalItems = append(literalItems, item)
			}
		} else {
			part := formatFromUnit(unit)
			if part != "" {
				nonLiteralParts = append(nonLiteralParts, part)
			}
		}
	}

	var sb strings.Builder
	itemPrefix := bodyPrefix + "  "

	if len(literalItems) == 0 {
		// Simple one-line form.
		sb.WriteString(bodyPrefix)
		sb.WriteString(strings.Join(nonLiteralParts, " and "))
		sb.WriteString("\n")
	} else {
		// Multi-line form with literal block.
		if len(nonLiteralParts) > 0 {
			sb.WriteString(bodyPrefix)
			sb.WriteString(strings.Join(nonLiteralParts, " and "))
			sb.WriteString(" and {\n")
		} else {
			sb.WriteString(bodyPrefix + "{\n")
		}
		for _, item := range literalItems {
			sb.WriteString(itemPrefix)
			// Items from the AST have bullet prefixes already stripped; add them back.
			if !strings.HasPrefix(item, "- ") && !strings.HasPrefix(item, "-\t") {
				sb.WriteString("- ")
			}
			sb.WriteString(item)
			sb.WriteString("\n")
		}
		sb.WriteString(bodyPrefix + "}\n")
	}

	return sb.String()
}

// formatFromUnit serialises a single from() unit to its inline string form.
// FromLiteral is handled separately in formatFromExpr and returns "".
func formatFromUnit(unit ast.FromUnit) string {
	switch unit.Kind {
	case ast.FromParentRef:
		return "from(parent" + formatSubscript(unit.ParentSub) + ")"
	case ast.FromNamedRef:
		return "from(" + unit.ParentName + ")"
	case ast.FromFieldRef:
		return "parent" + formatSubscript(unit.SourceSub) + "." + unit.FieldName + formatSubscript(unit.FieldSub)
	}
	return ""
}

// formatSubscript serialises a subscript expression to its bracket form.
func formatSubscript(s ast.Subscript) string {
	switch s.Kind {
	case ast.SubAll:
		return "[*]"
	case ast.SubIndex:
		return "[" + strconv.Itoa(s.N) + "]"
	case ast.SubRange:
		return "[" + strconv.Itoa(s.N) + ".." + strconv.Itoa(s.M) + "]"
	}
	return "[*]"
}

// ── Semantic simplification ───────────────────────────────────────────────────

// simplifyFromExpr applies semantic simplifications to a from() expression
// that are safe to perform without registry context:
//
//  1. Remove empty FromLiteral units (no items — they are no-ops).
//  2. Deduplicate adjacent identical non-literal units.
//  3. Collapse single-element ranges to a plain index:
//     from(parent[N..N+1]) → from(parent[N]).
func simplifyFromExpr(fe *ast.FromExpression) *ast.FromExpression {
	if fe == nil || len(fe.Units) == 0 {
		return fe
	}

	out := make([]ast.FromUnit, 0, len(fe.Units))
	for _, unit := range fe.Units {
		// Rule 1: drop empty literal blocks.
		if unit.Kind == ast.FromLiteral && len(unit.Items) == 0 {
			continue
		}

		// Rule 2: deduplicate adjacent identical non-literal units.
		if unit.Kind != ast.FromLiteral && len(out) > 0 {
			prev := out[len(out)-1]
			if prev.Kind != ast.FromLiteral && fromUnitsEqual(prev, unit) {
				continue
			}
		}

		// Rule 3: collapse single-element ranges to a plain index.
		u := unit
		if u.Kind == ast.FromParentRef && u.ParentSub.Kind == ast.SubRange {
			if u.ParentSub.M == u.ParentSub.N+1 {
				u.ParentSub = ast.Subscript{Kind: ast.SubIndex, N: u.ParentSub.N}
			}
		}
		if u.Kind == ast.FromFieldRef && u.SourceSub.Kind == ast.SubRange {
			if u.SourceSub.M == u.SourceSub.N+1 {
				u.SourceSub = ast.Subscript{Kind: ast.SubIndex, N: u.SourceSub.N}
			}
		}

		out = append(out, u)
	}

	if len(out) == 0 {
		return fe // safety: never produce an empty expression
	}
	return &ast.FromExpression{Units: out, Pos: fe.Pos}
}

// fromUnitsEqual reports whether two non-literal from() units are semantically
// identical (same kind + same subscripts / names).
func fromUnitsEqual(a, b ast.FromUnit) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case ast.FromParentRef:
		return a.ParentSub == b.ParentSub
	case ast.FromNamedRef:
		return a.ParentName == b.ParentName
	case ast.FromFieldRef:
		return a.SourceSub == b.SourceSub && a.FieldName == b.FieldName && a.FieldSub == b.FieldSub
	}
	return false
}
