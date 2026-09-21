// Package format re-serialises parsed AST nodes into canonical source.
package format

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

// Nodes formats a slice of nodes (a full file) into canonical source.
// It knows nothing about comments; use Source to format a file and keep them.
func Nodes(nodes []*ast.Node) string {
	c := &commentCtx{}
	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = c.node(n)
	}
	return strings.Join(parts, "\n")
}

// Node formats a single prompt, block, or overlay node into canonical source.
func Node(n *ast.Node) string {
	return (&commentCtx{}).node(n)
}

func (c *commentCtx) node(n *ast.Node) string {
	var sb strings.Builder

	c.emit(&sb, n.Pos.Line, 0)
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

	groups := c.renderBodyGroups(n)
	for i, group := range groups {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(group)
	}

	// Comments that sit after the last element, plus any whose element was not rendered
	// (so a comment can never be silently dropped).
	if left := c.leftover(n); len(left) > 0 {
		if len(groups) > 0 {
			sb.WriteString("\n")
		}
		writeComments(&sb, left, 2, 0)
	}

	sb.WriteString("}\n")
	return sb.String()
}

func (c *commentCtx) renderBodyGroups(n *ast.Node) []string {
	var groups []string

	if len(n.Tags) > 0 {
		var sb strings.Builder
		c.emit(&sb, n.TagsLine, 2)
		sb.WriteString("  tags: " + strings.Join(n.Tags, ", ") + "\n")
		groups = append(groups, sb.String())
	}

	if n.Kind == ast.KindPrompt && len(n.Vars) > 0 {
		var sb strings.Builder
		for _, v := range n.Vars {
			c.emit(&sb, v.Pos.Line, 2)
			if v.IsSlot {
				sb.WriteString("  " + formatSlot(v) + "\n")
			} else {
				sb.WriteString("  " + formatVar(v) + "\n")
			}
		}
		groups = append(groups, sb.String())
	}

	if len(n.Uses) > 0 {
		var sb strings.Builder
		for i, use := range n.Uses {
			if i < len(n.UsePos) {
				c.emit(&sb, n.UsePos[i].Line, 2)
			}
			sb.WriteString("  use " + use + "\n")
		}
		groups = append(groups, sb.String())
	}

	if len(n.Fields) > 0 {
		groups = append(groups, c.formatFieldOps(n.Fields, 2))
	}

	if n.Kind == ast.KindPrompt {
		for _, variant := range n.Variants {
			var sb strings.Builder
			c.emit(&sb, variant.Pos.Line, 2)
			fmt.Fprintf(&sb, "  variant %s {\n", variant.Name)
			sb.WriteString(c.formatFieldOps(variant.Fields, 4))
			sb.WriteString("  }\n")
			groups = append(groups, sb.String())
		}

		for _, env := range n.EnvBlocks {
			var sb strings.Builder
			c.emit(&sb, env.Pos.Line, 2)
			fmt.Fprintf(&sb, "  env %s {\n", env.Name)
			sb.WriteString(c.formatFieldOps(env.Fields, 4))
			sb.WriteString("  }\n")
			groups = append(groups, sb.String())
		}

		if n.Contract != nil {
			var sb strings.Builder
			c.emit(&sb, n.Contract.Pos.Line, 2)
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
			c.emit(&sb, n.Capabilities.Pos.Line, 2)
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

// formatSlot writes every piece of slot metadata back out. Dropping any of it changes
// meaning: without `secret: true` a secret slot would be rendered as plain text, and
// without `required: false` an optional slot becomes required.
func formatSlot(v ast.VarDecl) string {
	var meta []string
	switch {
	case v.Default != "":
		meta = append(meta, "default: "+strconv.Quote(v.Default))
	case v.Required:
		meta = append(meta, "required: true")
	default:
		meta = append(meta, "required: false")
	}
	if v.Secret {
		meta = append(meta, "secret: true")
	}
	return fmt.Sprintf("slot %s { %s }", v.Name, strings.Join(meta, ", "))
}

func formatFieldOps(fields []ast.FieldOperation, indent int) string {
	return (&commentCtx{}).formatFieldOps(fields, indent)
}

func (c *commentCtx) formatFieldOps(fields []ast.FieldOperation, indent int) string {
	var sb strings.Builder
	prefix := strings.Repeat(" ", indent)
	bodyPrefix := strings.Repeat(" ", indent+2)

	for i, f := range fields {
		c.emit(&sb, f.Pos.Line, indent)
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
