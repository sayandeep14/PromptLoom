// Package validate checks a populated registry for errors and warnings.
package validate

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	ivars "github.com/sayandeepgiri/promptloom/internal/vars"
)

// Severity classifies a diagnostic.
type Severity int

const (
	Error   Severity = iota // hard failure; non-zero exit
	Warning                 // advisory; does not block render
)

func (s Severity) String() string {
	if s == Error {
		return "Error"
	}
	return "Warning"
}

// Diagnostic is a single validation finding.
type Diagnostic struct {
	Sev     Severity
	Message string
	Pos     ast.Position
}

func (d Diagnostic) String() string {
	if d.Pos.File != "" {
		return fmt.Sprintf("%s: %s: %s", d.Pos, d.Sev, d.Message)
	}
	return fmt.Sprintf("%s: %s", d.Sev, d.Message)
}

// Validate runs all checks over the registry and returns the diagnostics.
func Validate(reg *registry.Registry, cfg *config.Config) []Diagnostic {
	var diags []Diagnostic

	for _, n := range sortedPrompts(reg) {
		diags = append(diags, checkPrompt(n, reg, cfg)...)
	}
	for _, n := range sortedBlocks(reg) {
		diags = append(diags, checkBlock(n, reg)...)
	}
	for _, n := range sortedOverlays(reg) {
		diags = append(diags, checkOverlay(n)...)
	}

	return diags
}

// ---- per-node checks ----

func checkPrompt(n *ast.Node, reg *registry.Registry, cfg *config.Config) []Diagnostic {
	var diags []Diagnostic

	// Unknown parent references — validate all parents (multiple inheritance).
	for _, parentRef := range n.Parents {
		if _, _, ok := reg.LookupPromptFull(parentRef, ""); !ok {
			msg := fmt.Sprintf("prompt %q inherits unknown prompt %q", n.Name, parentRef)
			if !strings.Contains(parentRef, ".") {
				if suggestion := suggest(parentRef, promptNames(reg)); suggestion != "" {
					msg += fmt.Sprintf("\n  Did you mean %q?", suggestion)
				}
			}
			diags = append(diags, Diagnostic{Sev: Error, Message: msg, Pos: n.Pos})
		}
	}

	// Unknown block references — namespace-aware lookup.
	for _, use := range n.Uses {
		if _, ok := reg.LookupBlockFull(use, ""); !ok {
			msg := fmt.Sprintf("prompt %q uses unknown block %q", n.Name, use)
			if !strings.Contains(use, ".") {
				if suggestion := suggest(use, blockNames(reg)); suggestion != "" {
					msg += fmt.Sprintf("\n  Did you mean %q?", suggestion)
				}
			}
			diags = append(diags, Diagnostic{Sev: Error, Message: msg, Pos: n.Pos})
		}
	}

	// Inheritance cycle detection.
	if cycle := detectCycle(n.Name, reg); cycle != "" {
		diags = append(diags, Diagnostic{
			Sev:     Error,
			Message: fmt.Sprintf("inheritance cycle detected: %s", cycle),
			Pos:     n.Pos,
		})
	}

	// Invalid field names.
	for _, f := range n.Fields {
		if f.FieldName == "tags" {
			// Tags must use the inline `tags: a, b, c` syntax, not a field operator.
			diags = append(diags, Diagnostic{
				Sev:     Warning,
				Message: fmt.Sprintf("prompt %q: use `tags: value1, value2` inline syntax for tags — operator syntax (%s) is not supported", n.Name, f.Op),
				Pos:     f.Pos,
			})
			continue
		}
		if !ast.ValidFields[f.FieldName] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("prompt %q uses unknown field %q", n.Name, f.FieldName),
				Pos:     f.Pos,
			})
		}
	}

	// Duplicate var/slot names.
	seenVars := map[string]ast.Position{}
	for _, decl := range n.Vars {
		if first, ok := seenVars[decl.Name]; ok {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("prompt %q declares %q more than once (first declared at %s)", n.Name, decl.Name, first),
				Pos:     decl.Pos,
			})
			continue
		}
		seenVars[decl.Name] = decl.Pos
	}

	// Duplicate variant names.
	seenVariants := map[string]ast.Position{}
	for _, variant := range n.Variants {
		if first, ok := seenVariants[variant.Name]; ok {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("prompt %q declares variant %q more than once (first declared at %s)", n.Name, variant.Name, first),
				Pos:     variant.Pos,
			})
			continue
		}
		seenVariants[variant.Name] = variant.Pos
		for _, f := range variant.Fields {
			if !ast.ValidFields[f.FieldName] {
				diags = append(diags, Diagnostic{
					Sev:     Error,
					Message: fmt.Sprintf("prompt %q variant %q uses unknown field %q", n.Name, variant.Name, f.FieldName),
					Pos:     f.Pos,
				})
			}
		}
	}

	// -=  on scalar fields is unsupported.
	for _, f := range n.Fields {
		if f.Op == ast.OpRemove && ast.ScalarFields[f.FieldName] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("prompt %q: operator '-=' is not supported on scalar field %q", n.Name, f.FieldName),
				Pos:     f.Pos,
			})
		}
	}

	// Legacy operators. v2 has exactly one field operator, ':='.
	inherited := map[string]bool{}
	if len(n.Parents) > 0 {
		inherited = allAncestorFields(n.Parents, reg)
	}
	diags = append(diags, checkLegacyOperators("prompt", n.Name, n.Fields, len(n.Parents), inherited)...)

	// Variant and env blocks obey the same operator rules as the prompt body.
	for _, v := range n.Variants {
		diags = append(diags, checkLegacyOperators("variant", v.Name, v.Fields, len(n.Parents), nil)...)
	}
	for _, e := range n.EnvBlocks {
		diags = append(diags, checkLegacyOperators("env", e.Name, e.Fields, len(n.Parents), nil)...)
	}

	// Static validation of from() expressions (prompt body, variants and env blocks).
	diags = append(diags, checkFromExpressions(n)...)

	// Required fields (configurable).
	fieldSet := fieldNameSet(n)
	depth := inheritanceDepth(n.Name, reg)
	_ = depth

	if cfg.Validation.RequireObjective && !fieldSet["objective"] && !hasInheritedField(n.Name, "objective", reg) {
		diags = append(diags, Diagnostic{
			Sev:     Warning,
			Message: fmt.Sprintf("prompt %q has no objective field", n.Name),
			Pos:     n.Pos,
		})
	}
	if cfg.Validation.RequireFormat && !fieldSet["format"] && !hasInheritedField(n.Name, "format", reg) {
		diags = append(diags, Diagnostic{
			Sev:     Warning,
			Message: fmt.Sprintf("prompt %q has no output format field", n.Name),
			Pos:     n.Pos,
		})
	}

	// Empty context warning.
	if cfg.Validation.WarnOnEmptyContext {
		for _, f := range n.Fields {
			if f.FieldName == "context" && len(f.Value) == 0 {
				diags = append(diags, Diagnostic{
					Sev:     Warning,
					Message: fmt.Sprintf("prompt %q has an empty context field", n.Name),
					Pos:     f.Pos,
				})
			}
		}
	}

	// Deep inheritance warning.
	if cfg.Validation.WarnOnDeepInheritance && depth > cfg.Validation.MaxInheritanceDepth {
		diags = append(diags, Diagnostic{
			Sev: Warning,
			Message: fmt.Sprintf(
				"prompt %q has inheritance depth %d (max %d); consider using blocks instead of deep inheritance",
				n.Name, depth, cfg.Validation.MaxInheritanceDepth,
			),
			Pos: n.Pos,
		})
	}

	// Redefining an inherited field with the legacy ':' (more specific than the generic
	// bare-colon warning, which is skipped for these fields).
	if len(n.Parents) > 0 {
		inheritedFields := allAncestorFields(n.Parents, reg)
		for _, f := range n.Fields {
			if f.Op == ast.OpDefine && inheritedFields[f.FieldName] {
				diags = append(diags, Diagnostic{
					Sev: Warning,
					Message: fmt.Sprintf(
						"prompt %q redefines inherited field %q with ':' instead of ':=' — use an explicit operator to clarify intent",
						n.Name, f.FieldName,
					),
					Pos: f.Pos,
				})
			}
		}
	}

	// Kind–block mismatch: warn when a block declares a kind that differs from the prompt's kind.
	promptKind := nodeKindTag(n)
	if promptKind != "" {
		for _, use := range n.Uses {
			if blk, ok := reg.LookupBlock(use); ok {
				blkKind := nodeKindTag(blk)
				if blkKind != "" && blkKind != promptKind {
					diags = append(diags, Diagnostic{
						Sev: Warning,
						Message: fmt.Sprintf(
							"prompt %q (kind: %s) uses block %q (kind: %s) — kind mismatch may indicate a misapplied block",
							n.Name, promptKind, use, blkKind,
						),
						Pos: n.Pos,
					})
				}
			}
		}
	}

	declaredVars := declaredVarSet(n)
	for _, usage := range placeholderUsages(n) {
		if !declaredVars[usage.name] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("prompt %q references undeclared variable %q", n.Name, usage.name),
				Pos:     usage.pos,
			})
		}
	}

	requiredAtRuntime := requiredRuntimeVars(n)
	if len(requiredAtRuntime) > 0 {
		diags = append(diags, Diagnostic{
			Sev:     Warning,
			Message: fmt.Sprintf("prompt %q requires runtime values for: %s", n.Name, strings.Join(requiredAtRuntime, ", ")),
			Pos:     n.Pos,
		})
	}

	return diags
}

// checkFromExpressions statically validates all from() expressions in a prompt's fields.
// Checks: scalar type errors, out-of-bounds parent indices, named refs not in parents list.
func checkFromExpressions(n *ast.Node) []Diagnostic {
	all := append([]ast.FieldOperation(nil), n.Fields...)
	for _, v := range n.Variants {
		all = append(all, v.Fields...)
	}
	for _, e := range n.EnvBlocks {
		all = append(all, e.Fields...)
	}
	return checkFromFields(n, all)
}

func checkFromFields(n *ast.Node, fields []ast.FieldOperation) []Diagnostic {
	var diags []Diagnostic
	parentCount := len(n.Parents)

	for _, f := range fields {
		if f.FromExpr == nil {
			continue
		}
		isScalar := ast.ScalarFields[f.FieldName]

		for _, unit := range f.FromExpr.Units {
			switch unit.Kind {
			case ast.FromParentRef:
				switch unit.ParentSub.Kind {
				case ast.SubAll:
					if isScalar {
						diags = append(diags, Diagnostic{
							Sev: Error,
							Message: fmt.Sprintf(
								"prompt %q field %q: from(parent[*]) cannot be used on scalar fields — use from(parent[N]) to select one specific parent",
								n.Name, f.FieldName,
							),
							Pos: unit.Pos,
						})
					}
				case ast.SubIndex:
					if unit.ParentSub.N >= parentCount {
						diags = append(diags, Diagnostic{
							Sev: Error,
							Message: fmt.Sprintf(
								"prompt %q field %q: parent[%d] is out of range — prompt has %d parent(s) (indices are 0-based)",
								n.Name, f.FieldName, unit.ParentSub.N, parentCount,
							),
							Pos: unit.Pos,
						})
					}
				case ast.SubRange:
					if unit.ParentSub.N >= parentCount || unit.ParentSub.M > parentCount {
						diags = append(diags, Diagnostic{
							Sev: Error,
							Message: fmt.Sprintf(
								"prompt %q field %q: parent[%d..%d] is out of range — prompt has %d parent(s)",
								n.Name, f.FieldName, unit.ParentSub.N, unit.ParentSub.M, parentCount,
							),
							Pos: unit.Pos,
						})
					}
				}

			case ast.FromNamedRef:
				// The referenced name must be one of the declared parents.
				if !slices.Contains(n.Parents, unit.ParentName) {
					diags = append(diags, Diagnostic{
						Sev: Error,
						Message: fmt.Sprintf(
							"prompt %q field %q: from(%s) references %q which is not a declared parent — only declared parents may appear in from() expressions",
							n.Name, f.FieldName, unit.ParentName, unit.ParentName,
						),
						Pos: unit.Pos,
					})
				}

			case ast.FromFieldRef:
				// The referenced field name must be valid.
				if unit.FieldName != "" && !ast.ValidFields[unit.FieldName] {
					diags = append(diags, Diagnostic{
						Sev: Error,
						Message: fmt.Sprintf(
							"prompt %q field %q: from() references unknown field %q",
							n.Name, f.FieldName, unit.FieldName,
						),
						Pos: unit.Pos,
					})
				}
				// Validate parent subscript bounds.
				if unit.SourceSub.Kind == ast.SubIndex && unit.SourceSub.N >= parentCount {
					diags = append(diags, Diagnostic{
						Sev: Error,
						Message: fmt.Sprintf(
							"prompt %q field %q: parent[%d] is out of range in from() expression — prompt has %d parent(s)",
							n.Name, f.FieldName, unit.SourceSub.N, parentCount,
						),
						Pos: unit.Pos,
					})
				}
			}
		}
	}
	return diags
}

func checkBlock(n *ast.Node, _ *registry.Registry) []Diagnostic {
	var diags []Diagnostic
	diags = append(diags, checkLegacyOperators("block", n.Name, n.Fields, 0, nil)...)

	for _, f := range n.Fields {
		if f.FieldName == "tags" {
			diags = append(diags, Diagnostic{
				Sev:     Warning,
				Message: fmt.Sprintf("block %q: use `tags: value1, value2` inline syntax for tags — operator syntax (%s) is not supported", n.Name, f.Op),
				Pos:     f.Pos,
			})
			continue
		}
		if !ast.ValidFields[f.FieldName] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("block %q uses unknown field %q", n.Name, f.FieldName),
				Pos:     f.Pos,
			})
		}
		// from() expressions make no sense in blocks (blocks have no parents).
		if f.FromExpr != nil {
			diags = append(diags, Diagnostic{
				Sev: Error,
				Message: fmt.Sprintf(
					"block %q field %q: from() expressions are not valid in blocks — blocks have no parents",
					n.Name, f.FieldName,
				),
				Pos: f.Pos,
			})
		}
		for _, line := range f.Value {
			for _, token := range ivars.Tokens(line) {
				diags = append(diags, Diagnostic{
					Sev:     Warning,
					Message: fmt.Sprintf("block %q uses {{ %s }} — variables must be declared in the consuming prompt", n.Name, token),
					Pos:     f.Pos,
				})
			}
		}
	}
	return diags
}

func checkOverlay(n *ast.Node) []Diagnostic {
	var diags []Diagnostic
	diags = append(diags, checkLegacyOperators("overlay", n.Name, n.Fields, 0, nil)...)
	for _, f := range n.Fields {
		if f.FieldName == "tags" {
			diags = append(diags, Diagnostic{
				Sev:     Warning,
				Message: fmt.Sprintf("overlay %q: use `tags: value1, value2` inline syntax for tags — operator syntax (%s) is not supported", n.Name, f.Op),
				Pos:     f.Pos,
			})
			continue
		}
		if !ast.ValidFields[f.FieldName] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("overlay %q uses unknown field %q", n.Name, f.FieldName),
				Pos:     f.Pos,
			})
		}
		for _, line := range f.Value {
			for _, token := range ivars.Tokens(line) {
				diags = append(diags, Diagnostic{
					Sev:     Warning,
					Message: fmt.Sprintf("overlay %q uses {{ %s }} — variables must be declared in the consuming prompt", n.Name, token),
					Pos:     f.Pos,
				})
			}
		}
	}
	return diags
}

// ---- legacy operators ----

// checkLegacyOperators reports the v1 operators. v2 has exactly one field operator,
// ':='. '+=' and '-=' are errors (each message contains the rewrite to paste in);
// a bare ':' still works but earns a warning with the exact replacement.
// kind is "prompt", "block" or "overlay"; parents is the number of declared parents;
// inherited holds field names an ancestor defines (reported separately for bare ':').
func checkLegacyOperators(kind, name string, fields []ast.FieldOperation, parents int, inherited map[string]bool) []Diagnostic {
	var diags []Diagnostic
	for _, f := range fields {
		if f.FieldName == "tags" {
			continue // tags use their own inline syntax
		}
		isScalar := ast.ScalarFields[f.FieldName]
		switch f.Op {
		case ast.OpAppend:
			diags = append(diags, Diagnostic{Sev: Error, Pos: f.Pos,
				Message: appendMessage(kind, name, f.FieldName, isScalar, parents)})
		case ast.OpRemove:
			if isScalar {
				continue // reported by the dedicated scalar rule
			}
			diags = append(diags, Diagnostic{Sev: Error, Pos: f.Pos, Message: fmt.Sprintf(
				"%s %q field %q: '-=' is not valid in v2 and has no direct replacement.\n"+
					"  Write the list you want with ':=' instead. To keep only some parent items, select them:\n"+
					"    %s :=\n      parent[0].%s[1..3] and {\n        - an extra item\n      }",
				kind, name, f.FieldName, f.FieldName, f.FieldName)})
		case ast.OpDefine:
			if inherited[f.FieldName] {
				continue // the more specific "redefines inherited field" warning covers it
			}
			diags = append(diags, Diagnostic{Sev: Warning, Pos: f.Pos, Message: fmt.Sprintf(
				"%s %q field %q uses ':' — v2 uses ':='. Change \"%s:\" to \"%s :=\"",
				kind, name, f.FieldName, f.FieldName, f.FieldName)})
		}
	}
	return diags
}

func appendMessage(kind, name, field string, isScalar bool, parents int) string {
	head := fmt.Sprintf("%s %q field %q: '+=' is not valid in v2 (the only operator is ':=').", kind, name, field)
	if kind == "variant" || kind == "env" {
		switch {
		case isScalar:
			return head + "\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text."
		case parents == 0:
			return head + fmt.Sprintf("\n  Write the complete list for this %s with ':=':\n    %s :=\n      - your item", kind, field)
		default:
			return head + fmt.Sprintf("\n  Write the complete list with ':=', or start from the parent's list "+
				"(note: this takes the PARENT's items, not this prompt's own):\n    %s :=\n      from(parent[0]) and {\n        - your item\n      }", field)
		}
	}
	switch {
	case kind != "prompt":
		if isScalar {
			return head + "\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text."
		}
		return head + fmt.Sprintf("\n  Blocks and overlays already ADD their list items to the prompt, so write:\n    %s :=\n      - your item", field)
	case isScalar:
		return head + "\n  A scalar field cannot be appended to; replace its value with ':=' and write the full text\n" +
			"  (or copy the parent's with  " + field + " :=\n    from(parent[0])  and edit from there)."
	case parents == 0:
		return head + fmt.Sprintf("\n  This prompt has no parent to append to. Write the whole list with ':=':\n    %s :=\n      - your item", field)
	default:
		src := "parent[0]"
		note := ""
		if parents > 1 {
			src = "parent[*]"
			note = " (use from(parent[N]) to take just one parent's items)"
		}
		return head + fmt.Sprintf("\n  To extend the inherited list, write:%s\n    %s :=\n      from(%s) and {\n        - your item\n      }", note, field, src)
	}
}

// ---- helpers ----

// detectCycle runs a DFS from `name` over the multi-parent graph and returns
// a cycle path string like "A -> B -> C -> A" if a cycle is reachable, else "".
func detectCycle(name string, reg *registry.Registry) string {
	onPath := map[string]bool{}
	visited := map[string]bool{}
	var path []string

	var dfs func(cur string) string
	dfs = func(cur string) string {
		if onPath[cur] {
			// Back-edge found — build the cycle path.
			for i, v := range path {
				if v == cur {
					cycle := make([]string, 0, len(path)-i+1)
					cycle = append(cycle, path[i:]...)
					cycle = append(cycle, cur)
					return strings.Join(cycle, " -> ")
				}
			}
			return cur + " -> ..."
		}
		if visited[cur] {
			return ""
		}
		onPath[cur] = true
		path = append(path, cur)

		if n, ok := reg.LookupPrompt(cur); ok {
			for _, p := range n.Parents {
				if res := dfs(p); res != "" {
					return res
				}
			}
		}

		path = path[:len(path)-1]
		delete(onPath, cur)
		visited[cur] = true
		return ""
	}

	return dfs(name)
}

// inheritanceDepth returns the maximum ancestor depth of the prompt named `name`.
// For multi-parent prompts the deepest parent chain wins.
func inheritanceDepth(name string, reg *registry.Registry) int {
	seen := map[string]bool{name: true}

	var maxDepth func(cur string) int
	maxDepth = func(cur string) int {
		n, ok := reg.LookupPrompt(cur)
		if !ok || len(n.Parents) == 0 {
			return 0
		}
		best := 0
		for _, p := range n.Parents {
			if seen[p] {
				continue // cycle guard
			}
			seen[p] = true
			d := 1 + maxDepth(p)
			if d > best {
				best = d
			}
			delete(seen, p)
		}
		return best
	}

	return maxDepth(name)
}

// hasInheritedField returns true if any ancestor of the named prompt defines fieldName.
func hasInheritedField(name, fieldName string, reg *registry.Registry) bool {
	n, ok := reg.LookupPrompt(name)
	if !ok {
		return false
	}
	seen := map[string]bool{name: true}

	var walk func(cur string) bool
	walk = func(cur string) bool {
		node, ok := reg.LookupPrompt(cur)
		if !ok {
			return false
		}
		for _, f := range node.Fields {
			if f.FieldName == fieldName {
				return true
			}
		}
		for _, p := range node.Parents {
			if seen[p] {
				continue
			}
			seen[p] = true
			if walk(p) {
				return true
			}
		}
		return false
	}

	for _, p := range n.Parents {
		if seen[p] {
			continue
		}
		seen[p] = true
		if walk(p) {
			return true
		}
	}
	return false
}

// allAncestorFields returns the set of field names defined by any ancestor reachable
// from the given parent names. Used to detect ambiguous ':' redefinitions.
func allAncestorFields(parentNames []string, reg *registry.Registry) map[string]bool {
	result := map[string]bool{}
	seen := map[string]bool{}

	var walk func(cur string)
	walk = func(cur string) {
		if seen[cur] {
			return
		}
		seen[cur] = true
		n, ok := reg.LookupPrompt(cur)
		if !ok {
			return
		}
		for _, f := range n.Fields {
			result[f.FieldName] = true
		}
		for _, p := range n.Parents {
			walk(p)
		}
	}

	for _, p := range parentNames {
		walk(p)
	}
	return result
}

// fieldNameSet returns the set of field names defined directly in n.
func fieldNameSet(n *ast.Node) map[string]bool {
	m := map[string]bool{}
	for _, f := range n.Fields {
		m[f.FieldName] = true
	}
	return m
}

func promptNames(reg *registry.Registry) []string {
	var names []string
	for _, n := range reg.Prompts() {
		names = append(names, n.Name)
	}
	return names
}

func blockNames(reg *registry.Registry) []string {
	var names []string
	for _, n := range reg.Blocks() {
		names = append(names, n.Name)
	}
	return names
}

func sortedPrompts(reg *registry.Registry) []*ast.Node {
	nodes := reg.Prompts()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

func sortedBlocks(reg *registry.Registry) []*ast.Node {
	nodes := reg.Blocks()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

func sortedOverlays(reg *registry.Registry) []*ast.Node {
	nodes := reg.Overlays()
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes
}

type placeholderUsage struct {
	name string
	pos  ast.Position
}

func placeholderUsages(n *ast.Node) []placeholderUsage {
	var out []placeholderUsage
	for _, f := range n.Fields {
		out = append(out, tokensForField(f)...)
	}
	for _, variant := range n.Variants {
		for _, f := range variant.Fields {
			out = append(out, tokensForField(f)...)
		}
	}
	return out
}

func tokensForField(f ast.FieldOperation) []placeholderUsage {
	var out []placeholderUsage
	for _, line := range f.Value {
		for _, token := range ivars.Tokens(line) {
			out = append(out, placeholderUsage{name: token, pos: f.Pos})
		}
	}
	return out
}

func declaredVarSet(n *ast.Node) map[string]bool {
	out := map[string]bool{}
	for _, decl := range n.Vars {
		out[decl.Name] = true
	}
	return out
}

func requiredRuntimeVars(n *ast.Node) []string {
	used := map[string]bool{}
	for _, usage := range placeholderUsages(n) {
		used[usage.name] = true
	}

	var out []string
	for _, decl := range n.Vars {
		if decl.Required && used[decl.Name] {
			out = append(out, decl.Name)
		}
	}
	sort.Strings(out)
	return out
}

// suggest returns the closest name from candidates to target using a simple
// edit-distance heuristic, or "" if nothing is close enough.
func suggest(target string, candidates []string) string {
	best := ""
	bestDist := 4 // threshold — only suggest if within 3 edits
	for _, c := range candidates {
		d := levenshtein(strings.ToLower(target), strings.ToLower(c))
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	return best
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if ra[i-1] == rb[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// nodeKindTag returns the kind tag value declared in the node's fields, or "".
func nodeKindTag(n *ast.Node) string {
	for _, f := range n.Fields {
		if f.FieldName == "kind" && len(f.Value) > 0 {
			return strings.TrimSpace(f.Value[0])
		}
	}
	return ""
}
