// Package resolve walks a prompt's inheritance chain, applies block fields,
// and merges all field operations to produce a ResolvedPrompt.
package resolve

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/fingerprint"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	ivars "github.com/sayandeepgiri/promptloom/internal/vars"
)

// Options controls render-time resolution features introduced in V2.
type Options struct {
	Variables map[string]string
	Variant   string
	Overlays  []string
	Env       string // apply the named env block after base resolution (e.g. "prod", "dev")
}

// Resolve fully resolves the named prompt with default options.
func Resolve(name string, reg *registry.Registry) (*ast.ResolvedPrompt, error) {
	return ResolveWithOptions(name, reg, Options{})
}

// ResolveWithOptions fully resolves the named prompt using render-time options.
func ResolveWithOptions(name string, reg *registry.Registry, opts Options) (*ast.ResolvedPrompt, error) {
	chain, err := buildChain(name, reg)
	if err != nil {
		return nil, err
	}

	target := chain[len(chain)-1]
	rp := &ast.ResolvedPrompt{
		Name:          name,
		SourceTrace:   make(map[string]string),
		FullTrace:     make(map[string][]ast.TraceEntry),
		ScalarSources: make(map[string]ast.SourceContribution),
		ListSources:   make(map[string][]ast.SourceContribution),
		Vars:          nil, // populated after chain walk below
		Variants:      append([]ast.VariantBlock(nil), target.Variants...),
	}

	for _, node := range chain {
		for _, blockName := range node.Uses {
			block, ok := reg.LookupBlock(blockName)
			if !ok {
				return nil, fmt.Errorf("block %q not found (run `loom inspect` first)", blockName)
			}
			if err := applyNodeFields(rp, block, blockName, true); err != nil {
				return nil, err
			}
		}
		if err := applyNodeFields(rp, node, node.Name, false); err != nil {
			return nil, err
		}
	}

	if opts.Variant != "" {
		variant, ok := lookupVariant(target, opts.Variant)
		if !ok {
			return nil, fmt.Errorf("variant %q not found on prompt %q", opts.Variant, target.Name)
		}
		if err := applyFieldOps(rp, variant.Fields, target.Name+"::"+variant.Name, false); err != nil {
			return nil, err
		}
		rp.AppliedVariant = variant.Name
	}

	for _, overlayRef := range opts.Overlays {
		overlay, ok := lookupOverlay(reg, overlayRef)
		if !ok {
			return nil, fmt.Errorf("overlay %q not found", overlayRef)
		}
		if err := applyNodeFields(rp, overlay, overlay.Name, true); err != nil {
			return nil, err
		}
		rp.AppliedOverlays = append(rp.AppliedOverlays, overlay.Name)
	}

	// Apply env block if requested — env blocks are additive (always +=).
	if opts.Env != "" {
		applied := false
		for _, node := range chain {
			for _, eb := range node.EnvBlocks {
				if strings.EqualFold(eb.Name, opts.Env) {
					if err := applyFieldOps(rp, eb.Fields, target.Name+"::env:"+eb.Name, false); err != nil {
						return nil, err
					}
					applied = true
				}
			}
		}
		if !applied {
			return nil, fmt.Errorf("env %q not declared on prompt %q", opts.Env, target.Name)
		}
		rp.AppliedEnv = opts.Env
	}

	// Collect var/slot declarations from the full inheritance chain so that
	// {{ tokens }} introduced by parent fields are resolvable in child prompts.
	// Ancestor defaults are overridden by closer ancestors, then by the target,
	// and finally by any runtime --set / --vars values.
	var chainVars []ast.VarDecl
	for _, node := range chain {
		chainVars = append(chainVars, node.Vars...)
	}
	rp.Vars = chainVars
	rp.VarValues = effectiveVarValues(chainVars, opts.Variables)
	rp.UnresolvedTokens = applyVariableSubstitution(rp)
	rp.Fingerprint, err = fingerprint.Compute(rp)
	if err != nil {
		return nil, err
	}

	rp.InheritsChain = chainNames(chain)
	rp.UsedBlocks = collectUsedBlocks(chain)

	return rp, nil
}

func buildChain(name string, reg *registry.Registry) ([]*ast.Node, error) {
	var chain []*ast.Node
	seen := map[string]bool{}
	current := name

	for {
		n, ok := reg.LookupPrompt(current)
		if !ok {
			return nil, fmt.Errorf("prompt %q not found", current)
		}
		if seen[current] {
			return nil, fmt.Errorf("inheritance cycle involving %q", current)
		}
		seen[current] = true
		chain = append(chain, n)
		if n.Parent == "" {
			break
		}
		current = n.Parent
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func lookupVariant(node *ast.Node, ref string) (*ast.VariantBlock, bool) {
	for _, variant := range node.Variants {
		if variant.Name == ref || toKebab(variant.Name) == ref {
			v := variant
			return &v, true
		}
	}
	return nil, false
}

func lookupOverlay(reg *registry.Registry, ref string) (*ast.Node, bool) {
	if overlay, ok := reg.LookupOverlay(ref); ok {
		return overlay, true
	}
	for _, overlay := range reg.Overlays() {
		if toKebab(overlay.Name) == ref {
			return overlay, true
		}
	}
	return nil, false
}

func applyNodeFields(rp *ast.ResolvedPrompt, node *ast.Node, sourceName string, fromComposable bool) error {
	return applyFieldOps(rp, node.Fields, sourceName, fromComposable)
}

func applyFieldOps(rp *ast.ResolvedPrompt, fields []ast.FieldOperation, sourceName string, fromComposable bool) error {
	for _, fo := range fields {
		if err := applyField(rp, fo, sourceName, fromComposable); err != nil {
			return err
		}
	}
	return nil
}

func applyField(rp *ast.ResolvedPrompt, fo ast.FieldOperation, sourceName string, fromComposable bool) error {
	switch {
	case ast.ScalarFields[fo.FieldName]:
		applyScalar(rp, fo, sourceName)
	case ast.ListFields[fo.FieldName]:
		applyList(rp, fo, sourceName, fromComposable)
	}
	return nil
}

func applyScalar(rp *ast.ResolvedPrompt, fo ast.FieldOperation, sourceName string) {
	newVal := strings.Join(fo.Value, "\n")
	contrib := ast.SourceContribution{
		FieldName: fo.FieldName,
		Value:     newVal,
		Source:    sourceName,
		Pos:       fo.Pos,
		Op:        fo.Op,
	}

	switch fo.Op {
	case ast.OpDefine, ast.OpOverride:
		setScalar(rp, fo.FieldName, newVal)
		rp.ScalarSources[fo.FieldName] = contrib
	case ast.OpAppend:
		existing := getScalar(rp, fo.FieldName)
		if existing == "" {
			setScalar(rp, fo.FieldName, newVal)
		} else {
			setScalar(rp, fo.FieldName, existing+"\n\n"+newVal)
		}
		rp.ScalarSources[fo.FieldName] = contrib
	}

	rp.SourceTrace[fo.FieldName] = sourceName
	rp.FullTrace[fo.FieldName] = append(rp.FullTrace[fo.FieldName], ast.TraceEntry{
		Op:     fo.Op,
		Source: sourceName,
		Pos:    fo.Pos,
	})
}

func applyList(rp *ast.ResolvedPrompt, fo ast.FieldOperation, sourceName string, fromComposable bool) {
	items := stripBullets(fo.Value)
	contribs := make([]ast.SourceContribution, 0, len(items))
	for i, item := range items {
		contribs = append(contribs, ast.SourceContribution{
			FieldName: fo.FieldName,
			Value:     item,
			Source:    sourceName,
			Pos: ast.Position{
				File: fo.Pos.File,
				Line: fo.Pos.Line + i + 1,
				Col:  fo.Pos.Col,
			},
			Op:        fo.Op,
			FromBlock: fromComposable,
		})
	}

	switch fo.Op {
	case ast.OpDefine:
		if fromComposable {
			existing := getList(rp, fo.FieldName)
			setList(rp, fo.FieldName, append(existing, items...))
			rp.ListSources[fo.FieldName] = append(rp.ListSources[fo.FieldName], contribs...)
		} else {
			setList(rp, fo.FieldName, items)
			rp.ListSources[fo.FieldName] = contribs
		}
	case ast.OpOverride:
		setList(rp, fo.FieldName, items)
		rp.ListSources[fo.FieldName] = contribs
	case ast.OpAppend:
		existing := getList(rp, fo.FieldName)
		setList(rp, fo.FieldName, append(existing, items...))
		rp.ListSources[fo.FieldName] = append(rp.ListSources[fo.FieldName], contribs...)
	case ast.OpRemove:
		existing := getList(rp, fo.FieldName)
		setList(rp, fo.FieldName, removeItems(existing, items))
		rp.ListSources[fo.FieldName] = removeContribs(rp.ListSources[fo.FieldName], items)
	}

	rp.SourceTrace[fo.FieldName] = sourceName
	rp.FullTrace[fo.FieldName] = append(rp.FullTrace[fo.FieldName], ast.TraceEntry{
		Op:        fo.Op,
		Source:    sourceName,
		Pos:       fo.Pos,
		FromBlock: fromComposable,
	})
}

func effectiveVarValues(decls []ast.VarDecl, overrides map[string]string) map[string]string {
	values := make(map[string]string, len(decls))
	for _, decl := range decls {
		values[decl.Name] = decl.Default
	}
	for key, value := range overrides {
		values[key] = value
	}
	return values
}

func applyVariableSubstitution(rp *ast.ResolvedPrompt) []string {
	unresolved := map[string]bool{}

	substituteScalar := func(fieldName string) {
		value := getScalar(rp, fieldName)
		if value == "" {
			return
		}
		next, missing := ivars.SubstituteString(value, rp.VarValues)
		setScalar(rp, fieldName, next)
		if contrib, ok := rp.ScalarSources[fieldName]; ok {
			contrib.Value = next
			rp.ScalarSources[fieldName] = contrib
		}
		for _, name := range missing {
			unresolved[name] = true
		}
	}

	substituteList := func(fieldName string) {
		values := getList(rp, fieldName)
		if len(values) == 0 {
			return
		}
		out := make([]string, len(values))
		for i, value := range values {
			next, missing := ivars.SubstituteString(value, rp.VarValues)
			out[i] = next
			if i < len(rp.ListSources[fieldName]) {
				rp.ListSources[fieldName][i].Value = next
			}
			for _, name := range missing {
				unresolved[name] = true
			}
		}
		setList(rp, fieldName, out)
	}

	for _, fieldName := range []string{"summary", "persona", "context", "objective", "notes", "kind"} {
		substituteScalar(fieldName)
	}
	for _, fieldName := range []string{"instructions", "constraints", "examples", "format", "todo", "compatible_with"} {
		substituteList(fieldName)
	}

	var out []string
	for name := range unresolved {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func stripBullets(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimPrefix(l, "- ")
	}
	return out
}

func removeItems(existing, toRemove []string) []string {
	rm := make(map[string]bool, len(toRemove))
	for _, r := range toRemove {
		rm[r] = true
	}
	var out []string
	for _, e := range existing {
		if !rm[e] {
			out = append(out, e)
		}
	}
	return out
}

func removeContribs(existing []ast.SourceContribution, toRemove []string) []ast.SourceContribution {
	rm := make(map[string]int, len(toRemove))
	for _, item := range toRemove {
		rm[item]++
	}
	var out []ast.SourceContribution
	for _, contrib := range existing {
		if rm[contrib.Value] > 0 {
			rm[contrib.Value]--
			continue
		}
		out = append(out, contrib)
	}
	return out
}

func getScalar(rp *ast.ResolvedPrompt, name string) string {
	switch name {
	case "summary":
		return rp.Summary
	case "persona":
		return rp.Persona
	case "context":
		return rp.Context
	case "objective":
		return rp.Objective
	case "notes":
		return rp.Notes
	case "kind":
		return rp.Kind
	}
	return ""
}

func setScalar(rp *ast.ResolvedPrompt, name, val string) {
	switch name {
	case "summary":
		rp.Summary = val
	case "persona":
		rp.Persona = val
	case "context":
		rp.Context = val
	case "objective":
		rp.Objective = val
	case "notes":
		rp.Notes = val
	case "kind":
		rp.Kind = val
	}
}

func getList(rp *ast.ResolvedPrompt, name string) []string {
	switch name {
	case "instructions":
		return rp.Instructions
	case "constraints":
		return rp.Constraints
	case "examples":
		return rp.Examples
	case "format":
		return rp.Format
	case "todo":
		return rp.Todo
	case "compatible_with":
		return rp.CompatibleWith
	}
	return nil
}

func setList(rp *ast.ResolvedPrompt, name string, val []string) {
	switch name {
	case "instructions":
		rp.Instructions = val
	case "constraints":
		rp.Constraints = val
	case "examples":
		rp.Examples = val
	case "format":
		rp.Format = val
	case "todo":
		rp.Todo = val
	case "compatible_with":
		rp.CompatibleWith = val
	}
}

func chainNames(chain []*ast.Node) []string {
	names := make([]string, len(chain))
	for i, n := range chain {
		names[i] = n.Name
	}
	return names
}

func collectUsedBlocks(chain []*ast.Node) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range chain {
		for _, b := range n.Uses {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
	}
	return out
}

func toKebab(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r + 32)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
