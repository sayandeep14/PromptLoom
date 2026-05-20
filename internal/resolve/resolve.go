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
	rp, err := resolveNode(name, reg, "", make(map[string]bool))
	if err != nil {
		return nil, err
	}

	if opts.Variant != "" {
		variant, ok := lookupVariantFromSlice(rp.Variants, opts.Variant)
		if !ok {
			return nil, fmt.Errorf("variant %q not found on prompt %q", opts.Variant, name)
		}
		if err := applyFieldOps(rp, variant.Fields, name+"::"+variant.Name, false); err != nil {
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

	if opts.Env != "" {
		applied := false
		for _, eb := range rp.AllEnvBlocks {
			if strings.EqualFold(eb.Name, opts.Env) {
				if err := applyFieldOps(rp, eb.Fields, name+"::env:"+eb.Name, false); err != nil {
					return nil, err
				}
				applied = true
			}
		}
		if !applied {
			return nil, fmt.Errorf("env %q not declared on prompt %q", opts.Env, name)
		}
		rp.AppliedEnv = opts.Env
	}

	// rp.Vars already accumulated depth-first by resolveNode.
	rp.VarValues = effectiveVarValues(rp.Vars, opts.Variables)
	rp.UnresolvedTokens = applyVariableSubstitution(rp)
	fp, fpErr := fingerprint.Compute(rp)
	if fpErr != nil {
		return nil, fpErr
	}
	rp.Fingerprint = fp

	return rp, nil
}

// resolveNode recursively resolves a prompt and all its parents, returning the
// fully merged ResolvedPrompt. contextNS is the namespace in which bare parent
// names are resolved (set to the pack slug when inside a pack).
func resolveNode(name string, reg *registry.Registry, contextNS string, inProgress map[string]bool) (*ast.ResolvedPrompt, error) {
	node, resolvedNS, ok := reg.LookupPromptFull(name, contextNS)
	if !ok {
		return nil, fmt.Errorf("prompt %q not found", name)
	}

	key := nsKey(name, resolvedNS)
	if inProgress[key] {
		return nil, fmt.Errorf("inheritance cycle involving %q", name)
	}
	inProgress[key] = true
	defer func() { delete(inProgress, key) }()

	rp := &ast.ResolvedPrompt{
		Name:          node.Name,
		SourceTrace:   make(map[string]string),
		FullTrace:     make(map[string][]ast.TraceEntry),
		ScalarSources: make(map[string]ast.SourceContribution),
		ListSources:   make(map[string][]ast.SourceContribution),
	}

	if len(node.Parents) == 0 {
		if err := applyBlocks(rp, node, reg, resolvedNS); err != nil {
			return nil, err
		}
		if err := applyNodeFields(rp, node, node.Name, false); err != nil {
			return nil, err
		}
		rp.InheritsChain = []string{node.Name}
		rp.UsedBlocks = nodeBlockNames(node)
		rp.Vars = append([]ast.VarDecl(nil), node.Vars...)
		rp.Variants = append([]ast.VariantBlock(nil), node.Variants...)
		rp.AllEnvBlocks = append([]ast.EnvBlock(nil), node.EnvBlocks...)
		return rp, nil
	}

	// Recursively resolve every declared parent (depth-first, left-to-right).
	parents := make([]*ast.ResolvedPrompt, len(node.Parents))
	for i, parentRef := range node.Parents {
		pr, err := resolveNode(parentRef, reg, resolvedNS, inProgress)
		if err != nil {
			return nil, fmt.Errorf("resolving parent %q of %q: %w", parentRef, name, err)
		}
		parents[i] = pr
	}

	// Merge parent fields into rp (first parent wins for conflicting fields).
	warnings := mergeParentResults(rp, parents, node.Parents)
	rp.Warnings = append(rp.Warnings, warnings...)

	// Apply this node's blocks before its own fields.
	if err := applyBlocks(rp, node, reg, resolvedNS); err != nil {
		return nil, err
	}

	// Apply this node's field operations, evaluating from() expressions when present.
	if err := applyChildFields(rp, node, parents); err != nil {
		return nil, err
	}

	rp.InheritsChain = buildInheritsChain(node.Name, parents)
	rp.UsedBlocks = buildUsedBlocks(node, parents)

	// Accumulate var declarations: ancestors first so child defaults override.
	for _, pr := range parents {
		rp.Vars = append(rp.Vars, pr.Vars...)
	}
	rp.Vars = append(rp.Vars, node.Vars...)

	// Variants: only from this node, not inherited.
	rp.Variants = append([]ast.VariantBlock(nil), node.Variants...)

	// Env blocks: accumulated chain-wide so opts.Env can match any ancestor's block.
	for _, pr := range parents {
		rp.AllEnvBlocks = append(rp.AllEnvBlocks, pr.AllEnvBlocks...)
	}
	rp.AllEnvBlocks = append(rp.AllEnvBlocks, node.EnvBlocks...)

	deduplicateLists(rp)

	return rp, nil
}

// nsKey returns a canonical key for cycle-detection; qualifies bare names when inside a pack.
func nsKey(name, resolvedNS string) string {
	if strings.IndexByte(name, '.') >= 0 {
		return name
	}
	if resolvedNS != "" {
		return resolvedNS + "." + name
	}
	return name
}

// applyBlocks applies each block named in node.Uses to rp (fromComposable semantics).
func applyBlocks(rp *ast.ResolvedPrompt, node *ast.Node, reg *registry.Registry, contextNS string) error {
	for _, blockName := range node.Uses {
		block, ok := reg.LookupBlockFull(blockName, contextNS)
		if !ok {
			return fmt.Errorf("block %q not found (run `loom inspect` first)", blockName)
		}
		if err := applyNodeFields(rp, block, blockName, true); err != nil {
			return err
		}
	}
	return nil
}

// applyChildFields applies a node's own field operations. Fields with a parsed
// from() expression are evaluated against parentResults; others use legacy operators.
func applyChildFields(rp *ast.ResolvedPrompt, node *ast.Node, parentResults []*ast.ResolvedPrompt) error {
	for _, fo := range node.Fields {
		if fo.FromExpr != nil {
			if err := applyFromExpr(rp, fo, node.Name, parentResults, node.Parents); err != nil {
				return fmt.Errorf("field %q: %w", fo.FieldName, err)
			}
		} else {
			if err := applyField(rp, fo, node.Name, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// applyFromExpr evaluates a from() expression and writes the result to rp.
func applyFromExpr(rp *ast.ResolvedPrompt, fo ast.FieldOperation, sourceName string, parents []*ast.ResolvedPrompt, parentNames []string) error {
	fe := fo.FromExpr
	isScalar := ast.ScalarFields[fo.FieldName]

	// Count non-literal units to detect scalar type errors ("and" chains on scalars).
	nonLiteralUnits := 0
	for _, u := range fe.Units {
		if u.Kind != ast.FromLiteral {
			nonLiteralUnits++
		}
	}

	var scalarVal string
	var listVals []string

	for _, unit := range fe.Units {
		switch unit.Kind {
		case ast.FromParentRef:
			indices, err := resolveParentIndices(unit.ParentSub, len(parents))
			if err != nil {
				return fmt.Errorf("from(parent[...]): %w", err)
			}
			if isScalar {
				if unit.ParentSub.Kind == ast.SubAll || len(indices) > 1 || nonLiteralUnits > 1 {
					return fmt.Errorf("scalar field %q: from(parent[*]) is not valid on scalar fields — use [N] to select a specific parent", fo.FieldName)
				}
				scalarVal = getScalar(parents[indices[0]], fo.FieldName)
			} else {
				for _, idx := range indices {
					listVals = append(listVals, getList(parents[idx], fo.FieldName)...)
				}
			}

		case ast.FromNamedRef:
			idx, err := findParentByName(unit.ParentName, parentNames)
			if err != nil {
				return err
			}
			if isScalar {
				if nonLiteralUnits > 1 {
					return fmt.Errorf("scalar field %q: 'and' chains are not valid on scalar fields", fo.FieldName)
				}
				scalarVal = getScalar(parents[idx], fo.FieldName)
			} else {
				listVals = append(listVals, getList(parents[idx], fo.FieldName)...)
			}

		case ast.FromFieldRef:
			indices, err := resolveParentIndices(unit.SourceSub, len(parents))
			if err != nil {
				return fmt.Errorf("parent[...].%s[...]: %w", unit.FieldName, err)
			}
			for _, idx := range indices {
				items := getList(parents[idx], unit.FieldName)
				sliced, err := applySubscriptToList(items, unit.FieldSub)
				if err != nil {
					return fmt.Errorf("subscript on field %q of parent[%d]: %w", unit.FieldName, idx, err)
				}
				if isScalar && len(sliced) > 0 {
					scalarVal = sliced[0]
				} else {
					listVals = append(listVals, sliced...)
				}
			}

		case ast.FromLiteral:
			if isScalar {
				return fmt.Errorf("scalar field %q: 'and { }' literal block is not valid on scalar fields", fo.FieldName)
			}
			listVals = append(listVals, unit.Items...)
		}
	}

	if isScalar {
		setScalar(rp, fo.FieldName, scalarVal)
		rp.SourceTrace[fo.FieldName] = sourceName
		rp.ScalarSources[fo.FieldName] = ast.SourceContribution{
			FieldName: fo.FieldName, Value: scalarVal, Source: sourceName, Pos: fo.Pos, Op: fo.Op,
		}
	} else {
		setList(rp, fo.FieldName, listVals)
		rp.SourceTrace[fo.FieldName] = sourceName
		contribs := make([]ast.SourceContribution, len(listVals))
		for i, v := range listVals {
			contribs[i] = ast.SourceContribution{
				FieldName: fo.FieldName, Value: v, Source: sourceName, Pos: fo.Pos, Op: fo.Op,
			}
		}
		rp.ListSources[fo.FieldName] = contribs
	}
	rp.FullTrace[fo.FieldName] = append(rp.FullTrace[fo.FieldName], ast.TraceEntry{
		Op: fo.Op, Source: sourceName, Pos: fo.Pos,
	})
	return nil
}

// mergeParentResults copies field values from parent resolved prompts into rp.
// For each field, if only one parent defines it it is used silently; if multiple
// parents define it, parent[0] wins and a warning is recorded.
func mergeParentResults(rp *ast.ResolvedPrompt, parents []*ast.ResolvedPrompt, parentNames []string) []string {
	var warnings []string

	for fieldName := range ast.ScalarFields {
		var setBy []int
		for i, pr := range parents {
			if getScalar(pr, fieldName) != "" {
				setBy = append(setBy, i)
			}
		}
		if len(setBy) == 0 {
			continue
		}
		w := setBy[0]
		setScalar(rp, fieldName, getScalar(parents[w], fieldName))
		rp.SourceTrace[fieldName] = parents[w].SourceTrace[fieldName]
		if src, ok := parents[w].ScalarSources[fieldName]; ok {
			rp.ScalarSources[fieldName] = src
		}
		if trace := parents[w].FullTrace[fieldName]; len(trace) > 0 {
			rp.FullTrace[fieldName] = append([]ast.TraceEntry(nil), trace...)
		}
		if len(setBy) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"field %q defined by parents %q and %q; using %q (parent[0])",
				fieldName, parentNames[setBy[0]], parentNames[setBy[1]], parentNames[0]))
		}
	}

	for fieldName := range ast.ListFields {
		var setBy []int
		for i, pr := range parents {
			if len(getList(pr, fieldName)) > 0 {
				setBy = append(setBy, i)
			}
		}
		if len(setBy) == 0 {
			continue
		}
		w := setBy[0]
		setList(rp, fieldName, append([]string(nil), getList(parents[w], fieldName)...))
		rp.SourceTrace[fieldName] = parents[w].SourceTrace[fieldName]
		if contribs := parents[w].ListSources[fieldName]; len(contribs) > 0 {
			rp.ListSources[fieldName] = append([]ast.SourceContribution(nil), contribs...)
		}
		if trace := parents[w].FullTrace[fieldName]; len(trace) > 0 {
			rp.FullTrace[fieldName] = append([]ast.TraceEntry(nil), trace...)
		}
		if len(setBy) > 1 {
			warnings = append(warnings, fmt.Sprintf(
				"field %q defined by parents %q and %q; using %q (parent[0])",
				fieldName, parentNames[setBy[0]], parentNames[setBy[1]], parentNames[0]))
		}
	}

	return warnings
}

// resolveParentIndices converts a subscript into a list of parent indices.
func resolveParentIndices(sub ast.Subscript, numParents int) ([]int, error) {
	switch sub.Kind {
	case ast.SubAll:
		out := make([]int, numParents)
		for i := range out {
			out[i] = i
		}
		return out, nil
	case ast.SubIndex:
		if sub.N >= numParents {
			return nil, fmt.Errorf("index %d out of range (have %d parents)", sub.N, numParents)
		}
		return []int{sub.N}, nil
	case ast.SubRange:
		var out []int
		for i := sub.N; i < sub.M && i < numParents; i++ {
			out = append(out, i)
		}
		return out, nil
	}
	return nil, nil
}

// applySubscriptToList slices a list according to a subscript expression.
func applySubscriptToList(items []string, sub ast.Subscript) ([]string, error) {
	switch sub.Kind {
	case ast.SubAll:
		return append([]string(nil), items...), nil
	case ast.SubIndex:
		if sub.N >= len(items) {
			return nil, fmt.Errorf("index %d out of range (list has %d items)", sub.N, len(items))
		}
		return []string{items[sub.N]}, nil
	case ast.SubRange:
		if sub.N >= len(items) {
			return nil, nil
		}
		end := sub.M
		if end > len(items) {
			end = len(items)
		}
		return append([]string(nil), items[sub.N:end]...), nil
	}
	return nil, nil
}

// findParentByName returns the index of the parent whose declaration name
// matches ref (bare or slug.Name). Returns error if not found.
func findParentByName(ref string, parentNames []string) (int, error) {
	for i, pn := range parentNames {
		if pn == ref {
			return i, nil
		}
		// Also accept matching the local part of "slug.Name".
		if dot := strings.LastIndexByte(pn, '.'); dot >= 0 && pn[dot+1:] == ref {
			return i, nil
		}
	}
	return -1, fmt.Errorf("from() reference %q is not a declared parent (declared: %v)", ref, parentNames)
}

// deduplicateLists removes exact-string-match duplicates from all list fields,
// preserving the first occurrence.
func deduplicateLists(rp *ast.ResolvedPrompt) {
	for fieldName := range ast.ListFields {
		items := getList(rp, fieldName)
		if len(items) <= 1 {
			continue
		}
		seen := make(map[string]bool, len(items))
		deduped := make([]string, 0, len(items))
		for _, item := range items {
			key := strings.TrimSpace(item)
			if !seen[key] {
				seen[key] = true
				deduped = append(deduped, item)
			}
		}
		if len(deduped) != len(items) {
			setList(rp, fieldName, deduped)
		}
	}
}

func buildInheritsChain(nodeName string, parents []*ast.ResolvedPrompt) []string {
	seen := map[string]bool{}
	var chain []string
	for _, pr := range parents {
		for _, n := range pr.InheritsChain {
			if !seen[n] {
				seen[n] = true
				chain = append(chain, n)
			}
		}
	}
	if !seen[nodeName] {
		chain = append(chain, nodeName)
	}
	return chain
}

func buildUsedBlocks(node *ast.Node, parents []*ast.ResolvedPrompt) []string {
	seen := map[string]bool{}
	var out []string
	for _, pr := range parents {
		for _, b := range pr.UsedBlocks {
			if !seen[b] {
				seen[b] = true
				out = append(out, b)
			}
		}
	}
	for _, b := range node.Uses {
		if !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	return out
}

func nodeBlockNames(node *ast.Node) []string {
	if len(node.Uses) == 0 {
		return nil
	}
	out := make([]string, len(node.Uses))
	copy(out, node.Uses)
	return out
}

// lookupVariantFromSlice finds a variant by name or kebab-case form.
func lookupVariantFromSlice(variants []ast.VariantBlock, ref string) (*ast.VariantBlock, bool) {
	for _, v := range variants {
		if v.Name == ref || toKebab(v.Name) == ref {
			vc := v
			return &vc, true
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
