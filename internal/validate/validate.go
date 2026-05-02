// Package validate checks a populated registry for errors and warnings.
package validate

import (
	"fmt"
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

	// Unknown parent reference.
	if n.Parent != "" {
		if _, ok := reg.LookupPrompt(n.Parent); !ok {
			msg := fmt.Sprintf("prompt %q inherits unknown prompt %q", n.Name, n.Parent)
			if suggestion := suggest(n.Parent, promptNames(reg)); suggestion != "" {
				msg += fmt.Sprintf("\n  Did you mean %q?", suggestion)
			}
			diags = append(diags, Diagnostic{Sev: Error, Message: msg, Pos: n.Pos})
		}
	}

	// Unknown block references.
	for _, use := range n.Uses {
		if _, ok := reg.LookupBlock(use); !ok {
			msg := fmt.Sprintf("prompt %q uses unknown block %q", n.Name, use)
			if suggestion := suggest(use, blockNames(reg)); suggestion != "" {
				msg += fmt.Sprintf("\n  Did you mean %q?", suggestion)
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

	// Required fields (configurable).
	fieldSet := fieldNameSet(n)
	depth := inheritanceDepth(n.Name, reg)
	isLeaf := depth == 0 || n.Parent != "" // only check prompts that extend something or stand alone

	_ = isLeaf // check all prompts for required fields for simplicity
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

	// Ambiguous redefinition warning: using ':' on an inherited field.
	if n.Parent != "" {
		inheritedFields := allAncestorFields(n.Parent, reg)
		for _, f := range n.Fields {
			if f.Op == ast.OpDefine && inheritedFields[f.FieldName] {
				diags = append(diags, Diagnostic{
					Sev: Warning,
					Message: fmt.Sprintf(
						"prompt %q redefines inherited field %q with ':' instead of ':=' or '+=' — use an explicit operator to clarify intent",
						n.Name, f.FieldName,
					),
					Pos: f.Pos,
				})
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

func checkBlock(n *ast.Node, reg *registry.Registry) []Diagnostic {
	var diags []Diagnostic

	for _, f := range n.Fields {
		if !ast.ValidFields[f.FieldName] {
			diags = append(diags, Diagnostic{
				Sev:     Error,
				Message: fmt.Sprintf("block %q uses unknown field %q", n.Name, f.FieldName),
				Pos:     f.Pos,
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
	for _, f := range n.Fields {
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

// ---- helpers ----

// detectCycle returns a string like "A -> B -> C -> A" if a cycle is found, else "".
func detectCycle(name string, reg *registry.Registry) string {
	visited := []string{}
	seen := map[string]bool{}
	current := name

	for {
		if seen[current] {
			// Find where the cycle starts in the visited chain.
			for i, v := range visited {
				if v == current {
					cycle := append(visited[i:], current)
					return strings.Join(cycle, " -> ")
				}
			}
		}
		seen[current] = true
		visited = append(visited, current)

		n, ok := reg.LookupPrompt(current)
		if !ok || n.Parent == "" {
			return ""
		}
		current = n.Parent
	}
}

// inheritanceDepth returns how many ancestors the prompt named `name` has.
func inheritanceDepth(name string, reg *registry.Registry) int {
	depth := 0
	current := name
	seen := map[string]bool{}
	for {
		n, ok := reg.LookupPrompt(current)
		if !ok || n.Parent == "" {
			return depth
		}
		if seen[n.Parent] {
			return depth // cycle — caught separately
		}
		seen[n.Parent] = true
		depth++
		current = n.Parent
	}
}

// hasInheritedField returns true if any ancestor of the named prompt defines fieldName.
func hasInheritedField(name, fieldName string, reg *registry.Registry) bool {
	seen := map[string]bool{}
	current := name
	for {
		n, ok := reg.LookupPrompt(current)
		if !ok || n.Parent == "" {
			return false
		}
		if seen[n.Parent] {
			return false
		}
		seen[n.Parent] = true
		parent, ok := reg.LookupPrompt(n.Parent)
		if !ok {
			return false
		}
		for _, f := range parent.Fields {
			if f.FieldName == fieldName {
				return true
			}
		}
		current = n.Parent
	}
}

// allAncestorFields returns the set of field names defined by any ancestor prompt.
func allAncestorFields(parentName string, reg *registry.Registry) map[string]bool {
	result := map[string]bool{}
	seen := map[string]bool{}
	current := parentName
	for {
		n, ok := reg.LookupPrompt(current)
		if !ok {
			break
		}
		for _, f := range n.Fields {
			result[f.FieldName] = true
		}
		if n.Parent == "" || seen[n.Parent] {
			break
		}
		seen[n.Parent] = true
		current = n.Parent
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
