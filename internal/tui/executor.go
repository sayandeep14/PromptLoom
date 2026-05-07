package tui

// executor.go — command logic that returns styled strings instead of printing.
// CLI handlers call these and print the result; the REPL also calls them and
// appends the result to its output buffer.

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/BurntSushi/toml"
	"github.com/atotto/clipboard"
	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	icontext "github.com/sayandeepgiri/promptloom/internal/context"
	icontracts "github.com/sayandeepgiri/promptloom/internal/contract"
	idiff "github.com/sayandeepgiri/promptloom/internal/diff"
	idoctor "github.com/sayandeepgiri/promptloom/internal/doctor"
	ilock "github.com/sayandeepgiri/promptloom/internal/lock"
	iformat "github.com/sayandeepgiri/promptloom/internal/format"
	igraph "github.com/sayandeepgiri/promptloom/internal/graph"
	"github.com/sayandeepgiri/promptloom/internal/loader"
	ipack "github.com/sayandeepgiri/promptloom/internal/pack"
	iparser "github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/render"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/semantic"
	"github.com/sayandeepgiri/promptloom/internal/sourcemap"
	itokens "github.com/sayandeepgiri/promptloom/internal/tokens"
	itestrunner "github.com/sayandeepgiri/promptloom/internal/testrunner"
	iaudit "github.com/sayandeepgiri/promptloom/internal/audit"
	"github.com/sayandeepgiri/promptloom/internal/validate"
)

// RunInspect validates the library at cwd and returns styled output.
// The bool is true when there are hard errors.
func RunInspect(cwd string) (string, bool) {
	var b strings.Builder

	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	diags := validate.Validate(reg, cfg)
	errors, warnings := 0, 0

	for _, d := range diags {
		if d.Sev == validate.Error {
			errors++
			icon := ErrorStyle.Render("✗")
			b.WriteString(fmt.Sprintf("  %s  %s\n", icon, ErrorStyle.Render(d.String())))
		} else {
			warnings++
			icon := WarningStyle.Render("⚠")
			b.WriteString(fmt.Sprintf("  %s  %s\n", icon, WarningStyle.Render(d.String())))
		}
	}

	if errors == 0 && warnings == 0 {
		b.WriteString("  " + SuccessStyle.Render("✓ Library is clean") + "\n")
	}

	b.WriteByte('\n')

	// Summary box.
	summary := fmt.Sprintf(
		"  Prompts checked  %s\n  Blocks checked   %s\n  Overlays checked %s\n  Errors           %s\n  Warnings         %s",
		PromptNameStyle.Render(fmt.Sprintf("%d", reg.PromptCount())),
		BlockNameStyle.Render(fmt.Sprintf("%d", reg.BlockCount())),
		BlockNameStyle.Render(fmt.Sprintf("%d", reg.OverlayCount())),
		fmtCount(errors, true),
		fmtCount(warnings, false),
	)
	b.WriteString(SummaryBox.Render(summary))
	b.WriteByte('\n')

	return b.String(), errors > 0
}

func fmtCount(n int, isErr bool) string {
	s := fmt.Sprintf("%d", n)
	if n == 0 {
		return SuccessStyle.Render(s)
	}
	if isErr {
		return ErrorStyle.Render(s)
	}
	return WarningStyle.Render(s)
}

// RunList lists prompts and blocks.
func RunList(cwd string, onlyPrompts, onlyBlocks bool) string {
	var b strings.Builder

	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n"
	}

	showPrompts := !onlyBlocks
	showBlocks := !onlyPrompts
	showOverlays := !onlyPrompts && !onlyBlocks

	if showPrompts {
		prompts := reg.Prompts()
		sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

		b.WriteString("  " + HeaderStyle.Render(fmt.Sprintf("Prompts (%d)", len(prompts))) + "\n")
		b.WriteString("  " + Divider(44) + "\n")
		if len(prompts) == 0 {
			b.WriteString("  " + MutedStyle.Render("(none)") + "\n")
		}
		for _, p := range prompts {
			name := PromptNameStyle.Render(fmt.Sprintf("%-30s", p.Name))
			var meta []string
			if p.Parent != "" {
				meta = append(meta, InheritsStyle.Render("inherits "+p.Parent))
			}
			if len(p.Variants) > 0 {
				var names []string
				for _, variant := range p.Variants {
					names = append(names, variant.Name)
				}
				meta = append(meta, MutedStyle.Render("variants: "+strings.Join(names, ", ")))
			}
			if len(meta) == 0 {
				b.WriteString(fmt.Sprintf("  %s\n", name))
				continue
			}
			b.WriteString(fmt.Sprintf("  %s  %s\n", name, strings.Join(meta, MutedStyle.Render("  ·  "))))
		}
	}

	if showPrompts && showBlocks {
		b.WriteByte('\n')
	}

	if showBlocks {
		blocks := reg.Blocks()
		sort.Slice(blocks, func(i, j int) bool { return blocks[i].Name < blocks[j].Name })

		b.WriteString("  " + HeaderStyle.Render(fmt.Sprintf("Blocks (%d)", len(blocks))) + "\n")
		b.WriteString("  " + Divider(44) + "\n")
		if len(blocks) == 0 {
			b.WriteString("  " + MutedStyle.Render("(none)") + "\n")
		}
		for _, bl := range blocks {
			bullet := BulletStyle.Render("●")
			b.WriteString(fmt.Sprintf("  %s  %s\n", bullet, BlockNameStyle.Render(bl.Name)))
		}
	}

	if showOverlays {
		b.WriteByte('\n')
		overlays := reg.Overlays()
		sort.Slice(overlays, func(i, j int) bool { return overlays[i].Name < overlays[j].Name })

		b.WriteString("  " + HeaderStyle.Render(fmt.Sprintf("Overlays (%d)", len(overlays))) + "\n")
		b.WriteString("  " + Divider(44) + "\n")
		if len(overlays) == 0 {
			b.WriteString("  " + MutedStyle.Render("(none)") + "\n")
		}
		for _, overlay := range overlays {
			bullet := BulletStyle.Render("◌")
			b.WriteString(fmt.Sprintf("  %s  %s\n", bullet, BlockNameStyle.Render(overlay.Name)))
		}
	}

	return b.String()
}

// TraceOptions selects one of the Milestone 9 trace views.
type TraceOptions struct {
	Field       string
	Instruction string
	Tree        bool
}

// RunTrace resolves a prompt and returns the requested trace view.
func RunTrace(name string, opts TraceOptions, cwd string) (string, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	if opts.Tree {
		return renderProjectTraceTree(reg, name)
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return "", err
	}
	if opts.Instruction != "" {
		return renderInstructionTrace(rp, opts.Instruction), nil
	}
	if opts.Field != "" {
		return renderFieldTrace(rp, opts.Field), nil
	}

	var b strings.Builder

	b.WriteString("\n  " + HeaderStyle.Render("Prompt") + "   " + PromptNameStyle.Render(rp.Name) + "\n\n")

	// Inheritance chain.
	b.WriteString("  " + SubHeaderStyle.Render("Inheritance Chain") + "\n")
	b.WriteString("  " + Divider(44) + "\n")
	for i, n := range rp.InheritsChain {
		num := MutedStyle.Render(fmt.Sprintf("%d", i+1))
		if n == rp.Name {
			b.WriteString(fmt.Sprintf("  %s  %s  %s\n", num,
				PromptNameStyle.Render(n),
				SuccessStyle.Render("← you are here")))
		} else {
			b.WriteString(fmt.Sprintf("  %s  %s\n", num, TextStyle.Render(n)))
		}
	}
	b.WriteByte('\n')

	// Used blocks.
	if len(rp.UsedBlocks) > 0 {
		b.WriteString("  " + SubHeaderStyle.Render("Used Blocks") + "\n")
		b.WriteString("  " + Divider(44) + "\n")
		for _, bl := range rp.UsedBlocks {
			b.WriteString(fmt.Sprintf("  %s  %s\n", BulletStyle.Render("●"), BlockNameStyle.Render(bl)))
		}
		b.WriteByte('\n')
	}

	// Resolved fields.
	b.WriteString("  " + SubHeaderStyle.Render("Resolved Fields") + "\n")
	b.WriteString("  " + Divider(44) + "\n")

	fieldOrder := []string{"summary", "persona", "context", "objective",
		"instructions", "constraints", "examples", "format", "notes"}
	maxLen := 0
	for _, f := range fieldOrder {
		if _, ok := rp.FullTrace[f]; ok && len(f) > maxLen {
			maxLen = len(f)
		}
	}

	for _, fieldName := range fieldOrder {
		entries, ok := rp.FullTrace[fieldName]
		if !ok {
			continue
		}
		pad := strings.Repeat(" ", maxLen-len(fieldName))
		chain := formatTraceEntries(entries)
		b.WriteString(fmt.Sprintf("  %s%s  %s\n",
			BrightStyle.Render(fieldName), pad, chain))
	}

	return b.String(), nil
}

func formatTraceEntries(entries []ast.TraceEntry) string {
	parts := make([]string, len(entries))
	for i, e := range entries {
		verb := traceVerbEntry(e, i == 0)
		src := PromptNameStyle.Render(e.Source)
		loc := ""
		if e.Pos.File != "" {
			loc = MutedStyle.Render(" @ " + e.Pos.String())
		}
		parts[i] = MutedStyle.Render(verb+" ") + src + loc
	}
	return strings.Join(parts, TraceArrow)
}

func traceVerb(op ast.Operator, isFirst bool) string {
	switch op {
	case ast.OpDefine:
		if isFirst {
			return "defined by"
		}
		return "overridden by"
	case ast.OpOverride:
		return "overridden by"
	case ast.OpAppend:
		return "appended by"
	case ast.OpRemove:
		return "removed by"
	}
	return "set by"
}

func traceVerbEntry(entry ast.TraceEntry, isFirst bool) string {
	if entry.FromBlock {
		if entry.Op == ast.OpRemove {
			return "removed by block"
		}
		return "contributed by block"
	}
	return traceVerb(entry.Op, isFirst)
}

func renderFieldTrace(rp *ast.ResolvedPrompt, fieldName string) string {
	var b strings.Builder
	entries := rp.FullTrace[fieldName]
	if len(entries) == 0 {
		return ErrorStyle.Render(fmt.Sprintf("Error: field %q is not present on %s", fieldName, rp.Name)) + "\n"
	}

	b.WriteString("\n  " + HeaderStyle.Render("Field Trace") + "   " + BrightStyle.Render(fieldName) + "\n\n")
	for i, entry := range entries {
		verb := traceVerbEntry(entry, i == 0)
		line := fmt.Sprintf("  %d. %s %s", i+1, verb, entry.Source)
		if entry.Pos.File != "" {
			line += fmt.Sprintf("  (%s)", entry.Pos)
		}
		if entry.FromBlock {
			line += " [block]"
		}
		b.WriteString(line + "\n")
	}

	if contribs := rp.ListSources[fieldName]; len(contribs) > 0 {
		b.WriteString("\n  " + SubHeaderStyle.Render("Final Items") + "\n")
		b.WriteString("  " + Divider(44) + "\n")
		for _, contrib := range contribs {
			b.WriteString(fmt.Sprintf("  %s %s\n", BulletStyle.Render("•"), TextStyle.Render(contrib.Value)))
			b.WriteString(fmt.Sprintf("    %s\n", MutedStyle.Render(fmt.Sprintf("%s at %s", contrib.Source, contrib.Pos))))
		}
	}

	return b.String()
}

func renderInstructionTrace(rp *ast.ResolvedPrompt, instruction string) string {
	type match struct {
		field   string
		contrib ast.SourceContribution
	}
	var matches []match
	needle := strings.TrimSpace(instruction)
	for fieldName, contribs := range rp.ListSources {
		for _, contrib := range contribs {
			if contrib.Value == needle || strings.Contains(strings.ToLower(contrib.Value), strings.ToLower(needle)) {
				matches = append(matches, match{field: fieldName, contrib: contrib})
			}
		}
	}

	if len(matches) == 0 {
		return ErrorStyle.Render(fmt.Sprintf("Error: no instruction matching %q found in %s", instruction, rp.Name)) + "\n"
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Instruction Trace") + "\n\n")
	for _, item := range matches {
		b.WriteString(fmt.Sprintf("  %s\n", TextStyle.Render(item.contrib.Value)))
		b.WriteString(fmt.Sprintf("    field: %s\n", BrightStyle.Render(item.field)))
		b.WriteString(fmt.Sprintf("    source: %s\n", PathStyle.Render(item.contrib.Pos.String())))
		b.WriteString(fmt.Sprintf("    via: %s\n\n", MutedStyle.Render(sourceOpName(item.contrib))))
	}
	return b.String()
}

func renderTraceTree(rp *ast.ResolvedPrompt) string {
	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Prompt Tree") + "\n\n")
	for i, name := range rp.InheritsChain {
		prefix := ""
		if i > 0 {
			prefix = strings.Repeat("  ", i-1) + "└─ "
		}
		rendered := TextStyle.Render(name)
		if i == len(rp.InheritsChain)-1 {
			rendered = PromptNameStyle.Render(name)
		}
		b.WriteString("  " + prefix + rendered + "\n")
	}
	if len(rp.UsedBlocks) > 0 {
		lastIndent := ""
		if len(rp.InheritsChain) > 1 {
			lastIndent = strings.Repeat("  ", len(rp.InheritsChain)-1)
		}
		for _, block := range rp.UsedBlocks {
			b.WriteString("  " + lastIndent + "└─ " + BlockNameStyle.Render("[block] "+block) + "\n")
		}
	}
	return b.String()
}

func renderProjectTraceTree(reg *registry.Registry, focus string) (string, error) {
	if focus != "" {
		if _, ok := reg.LookupPrompt(focus); !ok {
			return "", fmt.Errorf("prompt %q not found", focus)
		}
	}

	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

	children := map[string][]*ast.Node{}
	roots := []*ast.Node{}
	for _, prompt := range prompts {
		if prompt.Parent == "" {
			roots = append(roots, prompt)
			continue
		}
		children[prompt.Parent] = append(children[prompt.Parent], prompt)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].Name < roots[j].Name })
	for parent := range children {
		sort.Slice(children[parent], func(i, j int) bool { return children[parent][i].Name < children[parent][j].Name })
	}

	var b strings.Builder
	if focus == "" {
		b.WriteString("\n  " + HeaderStyle.Render("Project Tree") + "\n\n")
	} else {
		b.WriteString("\n  " + HeaderStyle.Render("Project Tree") + "   " + PromptNameStyle.Render(focus) + "\n\n")
	}

	for i, root := range roots {
		lastRoot := i == len(roots)-1
		renderPromptTreeNode(&b, root, children, focus, "", lastRoot, true)
		if !lastRoot {
			b.WriteByte('\n')
		}
	}

	return b.String(), nil
}

func renderPromptTreeNode(b *strings.Builder, node *ast.Node, children map[string][]*ast.Node, focus, prefix string, isLast bool, isRoot bool) {
	branch := ""
	nextPrefix := prefix
	if !isRoot {
		branch = "├─ "
		nextPrefix = prefix + "│  "
		if isLast {
			branch = "└─ "
			nextPrefix = prefix + "   "
		}
	}

	rendered := TextStyle.Render(node.Name)
	if node.Name == focus {
		rendered = FocusedPromptStyle.Render(node.Name)
	} else if node.Parent == "" {
		rendered = PromptNameStyle.Render(node.Name)
	}
	b.WriteString("  " + prefix + branch + rendered + "\n")

	var extras []string
	for _, use := range node.Uses {
		extras = append(extras, "[block] "+use)
	}

	childNodes := children[node.Name]
	total := len(extras) + len(childNodes)
	index := 0
	for _, extra := range extras {
		index++
		extraBranch := "├─ "
		if index == total {
			extraBranch = "└─ "
		}
		b.WriteString("  " + nextPrefix + extraBranch + BlockNameStyle.Render(extra) + "\n")
	}
	for _, child := range childNodes {
		index++
		renderPromptTreeNode(b, child, children, focus, nextPrefix, index == total, false)
	}
}

func sourceOpName(contrib ast.SourceContribution) string {
	if contrib.FromBlock {
		return "block"
	}
	switch contrib.Op {
	case ast.OpDefine:
		return "define"
	case ast.OpOverride:
		return "override"
	case ast.OpAppend:
		return "append"
	case ast.OpRemove:
		return "remove"
	}
	return "unknown"
}

var unravelOrder = []struct {
	name   string
	isList bool
}{
	{"summary", false}, {"persona", false}, {"context", false},
	{"objective", false}, {"instructions", true}, {"constraints", true},
	{"examples", true}, {"format", true}, {"notes", false},
}

// RunUnravel returns a prompt's fully resolved fields, optionally with sources.
func RunUnravel(name string, withSource bool, cwd string) (string, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}
	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Prompt") + "   " + PromptNameStyle.Render(rp.Name) + "\n\n")

	for _, sec := range unravelOrder {
		if sec.isList {
			items := getListField(rp, sec.name)
			if len(items) == 0 {
				continue
			}
			b.WriteString(unravelHeader(sec.name, rp, withSource))
			for _, item := range items {
				b.WriteString(fmt.Sprintf("    %s %s\n",
					BulletStyle.Render("–"), TextStyle.Render(item)))
			}
		} else {
			val := getScalarField(rp, sec.name)
			if val == "" {
				continue
			}
			b.WriteString(unravelHeader(sec.name, rp, withSource))
			for _, line := range strings.Split(val, "\n") {
				b.WriteString(fmt.Sprintf("    %s\n", TextStyle.Render(line)))
			}
		}
		b.WriteByte('\n')
	}

	return b.String(), nil
}

func unravelHeader(fieldName string, rp *ast.ResolvedPrompt, withSource bool) string {
	label := BrightStyle.Render("[" + fieldName + "]")
	if withSource {
		src := rp.SourceTrace[fieldName]
		return fmt.Sprintf("  %s  %s\n", label,
			MutedStyle.Render("(last set by: ")+PromptNameStyle.Render(src)+MutedStyle.Render(")"))
	}
	return "  " + label + "\n"
}

func getScalarField(rp *ast.ResolvedPrompt, name string) string {
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
	}
	return ""
}

func getListField(rp *ast.ResolvedPrompt, name string) []string {
	switch name {
	case "instructions":
		return rp.Instructions
	case "constraints":
		return rp.Constraints
	case "examples":
		return rp.Examples
	case "format":
		return rp.Format
	}
	return nil
}

// WeaveOptions controls render-time prompt adaptation for loom weave.
type WeaveOptions struct {
	OutPath          string
	Stdout           bool
	Format           string
	Variables        map[string]string
	Profile          string
	Variant          string
	Overlays         []string
	SourceMap        bool
	InteractiveSlots bool
	WithSources      []string // --with context sources
	ContextBundle    string   // --context bundle name
	Incremental      bool     // --incremental: skip prompts whose hash is unchanged
	Env              string   // --env: apply the named env block (e.g. "prod")
}

// CopyOptions wraps WeaveOptions for loom copy / loom cast.
type CopyOptions struct {
	WeaveOptions
	Destination string // "clipboard", "stdout", "file"
}

// DeployOptions controls loom deploy output and filtering behavior.
type DeployOptions struct {
	DryRun       bool
	Diff         bool
	TargetFormat string
}

// checkSecretSlots errors if any secret slots have been given plain-text values via --set.
func checkSecretSlots(name string, reg *registry.Registry, vars map[string]string) error {
	node, ok := reg.LookupPrompt(name)
	if !ok {
		return nil
	}
	for _, v := range node.Vars {
		if v.Secret && v.IsSlot {
			if _, provided := vars[v.Name]; provided {
				return fmt.Errorf("slot %q is marked secret and cannot be rendered as plain text.\nPass secrets through the target tool's secure environment instead.", v.Name)
			}
		}
	}
	return nil
}

// RunWeave resolves and renders one or all prompts.
func RunWeave(name string, all bool, opts WeaveOptions, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	outDir := filepath.Join(cwd, cfg.Paths.Out)

	var b strings.Builder
	baseVars, err := buildWeaveVariables(cfg, cwd, opts)
	if err != nil {
		return "", err
	}
	emitSourceMap := cfg.Render.IncludeSourceMap || opts.SourceMap

	if all {
		prompts := reg.Prompts()
		sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return "", err
		}

		// Load incremental cache if requested.
		var cache map[string]string
		if opts.Incremental {
			cache = LoadIncrementalCache(cwd)
		}

		woven, skipped := 0, 0
		for _, p := range prompts {
			if missing := requiredPromptVars(p, baseVars); len(missing) > 0 {
				b.WriteString(fmt.Sprintf("  %s  %s: missing values for %s\n",
					ErrorStyle.Render("✗"), PromptNameStyle.Render(p.Name), strings.Join(missing, ", ")))
				continue
			}
			rp, err := resolve.ResolveWithOptions(p.Name, reg, resolve.Options{
				Variables: baseVars,
				Variant:   opts.Variant,
				Overlays:  opts.Overlays,
				Env:       opts.Env,
			})
			if err != nil {
				b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
					ErrorStyle.Render("✗"), PromptNameStyle.Render(p.Name), err))
				continue
			}
			if len(rp.UnresolvedTokens) > 0 {
				b.WriteString(fmt.Sprintf("  %s  %s: unresolved variables: %s\n",
					ErrorStyle.Render("✗"), PromptNameStyle.Render(p.Name), strings.Join(rp.UnresolvedTokens, ", ")))
				continue
			}

			// Incremental: skip if hash unchanged.
			if opts.Incremental && cache[p.Name] == rp.Fingerprint {
				b.WriteString(fmt.Sprintf("  %s  skip  %s\n",
					MutedStyle.Render("·"), MutedStyle.Render(p.Name)))
				skipped++
				continue
			}

			body, format, err := render.RenderFormat(rp, cfg, opts.Format)
			if err != nil {
				b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
					ErrorStyle.Render("✗"), PromptNameStyle.Render(p.Name), err))
				continue
			}
			dest := filepath.Join(outDir, format.DefaultFileName(p.Name))
			if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
				return "", fmt.Errorf("writing %s: %w", dest, err)
			}
			if emitSourceMap {
				mapPath, err := writeSourceMapFile(dest, rp, cwd)
				if err != nil {
					return "", err
				}
				b.WriteString(fmt.Sprintf("  %s  map   %s\n",
					MutedStyle.Render("·"), PathStyle.Render(mapPath)))
			}
			b.WriteString(fmt.Sprintf("  %s  wove  %s\n",
				SuccessStyle.Render("✓"), PathStyle.Render(dest)))

			if opts.Incremental {
				cache[p.Name] = rp.Fingerprint
			}
			woven++
		}

		// Persist incremental cache.
		if opts.Incremental {
			_ = SaveIncrementalCache(cwd, cache)
		}

		b.WriteByte('\n')
		if opts.Incremental {
			b.WriteString(fmt.Sprintf("  %s woven, %s skipped (unchanged)\n",
				PromptNameStyle.Render(fmt.Sprintf("%d", woven)),
				MutedStyle.Render(fmt.Sprintf("%d", skipped))))
		} else {
			b.WriteString(fmt.Sprintf("  %s rendered to %s\n",
				PromptNameStyle.Render(fmt.Sprintf("%d prompts", len(prompts))),
				PathStyle.Render(outDir)))
		}
		return b.String(), nil
	}

	prompt, ok := reg.LookupPrompt(name)
	if !ok {
		return "", fmt.Errorf("prompt %q not found", name)
	}
	varValues := cloneMap(baseVars)
	if opts.InteractiveSlots {
		var err error
		varValues, err = fillMissingSlots(prompt, varValues)
		if err != nil {
			return "", err
		}
	}
	if missing := requiredPromptVars(prompt, varValues); len(missing) > 0 {
		return "", fmt.Errorf("missing required values for %s", strings.Join(missing, ", "))
	}

	// Enforce secret slot protection before resolution.
	if err := checkSecretSlots(name, reg, varValues); err != nil {
		return "", err
	}

	rp, err := resolve.ResolveWithOptions(name, reg, resolve.Options{
		Variables: varValues,
		Variant:   opts.Variant,
		Overlays:  opts.Overlays,
		Env:       opts.Env,
	})
	if err != nil {
		return "", err
	}
	if len(rp.UnresolvedTokens) > 0 {
		return "", fmt.Errorf("unresolved variables: %s", strings.Join(rp.UnresolvedTokens, ", "))
	}
	body, format, err := render.RenderFormat(rp, cfg, opts.Format)
	if err != nil {
		return "", err
	}

	ctxSources, err := resolveContextSources(opts, cwd)
	if err != nil {
		return "", err
	}
	body = icontext.AppendContextSection(body, ctxSources)

	if opts.Stdout && emitSourceMap {
		return "", fmt.Errorf("--sourcemap cannot be combined with --stdout")
	}
	if opts.Stdout {
		return body, nil
	}

	dest := opts.OutPath
	if dest == "" {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return "", err
		}
		dest = filepath.Join(outDir, format.DefaultFileName(name))
	} else {
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
		return "", err
	}
	if emitSourceMap {
		mapPath, err := writeSourceMapFile(dest, rp, cwd)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  %s  map   %s\n", MutedStyle.Render("·"), PathStyle.Render(mapPath)))
	}
	b.WriteString(fmt.Sprintf("  %s  wove  %s\n", SuccessStyle.Render("✓"), PathStyle.Render(dest)))
	if opts.Profile != "" || rp.AppliedVariant != "" || len(rp.AppliedOverlays) > 0 || len(rp.VarValues) > 0 || len(ctxSources) > 0 {
		var meta []string
		if opts.Format != "" {
			meta = append(meta, MutedStyle.Render("format ")+PromptNameStyle.Render(opts.Format))
		}
		if opts.Profile != "" {
			meta = append(meta, MutedStyle.Render("profile ")+PromptNameStyle.Render(opts.Profile))
		}
		if rp.AppliedVariant != "" {
			meta = append(meta, MutedStyle.Render("variant ")+PromptNameStyle.Render(rp.AppliedVariant))
		}
		if len(rp.AppliedOverlays) > 0 {
			meta = append(meta, MutedStyle.Render("overlays ")+BlockNameStyle.Render(strings.Join(rp.AppliedOverlays, ", ")))
		}
		if len(rp.VarValues) > 0 {
			meta = append(meta, MutedStyle.Render(fmt.Sprintf("%d vars", len(rp.VarValues))))
		}
		if len(ctxSources) > 0 {
			meta = append(meta, MutedStyle.Render(fmt.Sprintf("%d context source(s)", len(ctxSources))))
		}
		b.WriteString("  " + strings.Join(meta, MutedStyle.Render("  ·  ")) + "\n")
	}
	return b.String(), nil
}

// RunCopy renders a prompt and copies it to the clipboard (or another dest).
func RunCopy(name string, opts CopyOptions, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	prompt, ok := reg.LookupPrompt(name)
	if !ok {
		return "", fmt.Errorf("prompt %q not found", name)
	}

	baseVars, err := buildWeaveVariables(cfg, cwd, opts.WeaveOptions)
	if err != nil {
		return "", err
	}
	varValues := cloneMap(baseVars)
	if opts.InteractiveSlots {
		varValues, err = fillMissingSlots(prompt, varValues)
		if err != nil {
			return "", err
		}
	}
	if missing := requiredPromptVars(prompt, varValues); len(missing) > 0 {
		return "", fmt.Errorf("missing required values for %s", strings.Join(missing, ", "))
	}

	rp, err := resolve.ResolveWithOptions(name, reg, resolve.Options{
		Variables: varValues,
		Variant:   opts.Variant,
		Overlays:  opts.Overlays,
		Env:       opts.Env,
	})
	if err != nil {
		return "", err
	}
	if len(rp.UnresolvedTokens) > 0 {
		return "", fmt.Errorf("unresolved variables: %s", strings.Join(rp.UnresolvedTokens, ", "))
	}

	body, _, err := render.RenderFormat(rp, cfg, opts.Format)
	if err != nil {
		return "", err
	}

	ctxSources, err := resolveContextSources(opts.WeaveOptions, cwd)
	if err != nil {
		return "", err
	}
	body = icontext.AppendContextSection(body, ctxSources)

	dest := opts.Destination
	if dest == "" {
		dest = "clipboard"
	}

	var b strings.Builder
	switch dest {
	case "clipboard":
		if err := clipboard.WriteAll(body); err != nil {
			return "", fmt.Errorf("copying to clipboard: %w", err)
		}
		b.WriteString(fmt.Sprintf("  %s  Copied to clipboard\n", SuccessStyle.Render("✓")))
	case "stdout":
		return body, nil
	case "file":
		outDir := filepath.Join(cwd, cfg.Paths.Out)
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return "", err
		}
		dest := filepath.Join(outDir, name+".md")
		if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  %s  Wrote  %s\n", SuccessStyle.Render("✓"), PathStyle.Render(dest)))
	default:
		return "", fmt.Errorf("unknown destination %q — use clipboard, stdout, or file", dest)
	}

	b.WriteString(fmt.Sprintf("  %s  %s\n", MutedStyle.Render("Prompt"), PromptNameStyle.Render(name)))
	if rp.AppliedVariant != "" {
		b.WriteString(fmt.Sprintf("  %s  %s\n", MutedStyle.Render("Variant"), PromptNameStyle.Render(rp.AppliedVariant)))
	}
	if len(ctxSources) > 0 {
		var ctxLabels []string
		ctxTokens := 0
		for _, s := range ctxSources {
			ctxLabels = append(ctxLabels, s.Label)
			ctxTokens += icontext.EstimateTokens(s.Content)
		}
		b.WriteString(fmt.Sprintf("  %s  %s  (%d tokens appended)\n",
			MutedStyle.Render("Context"),
			BlockNameStyle.Render(strings.Join(ctxLabels, ", ")),
			ctxTokens,
		))
	}
	b.WriteString(fmt.Sprintf("  %s  %d tokens\n", MutedStyle.Render("Size"), icontext.EstimateTokens(body)))
	return b.String(), nil
}

// resolveContextSources collects all context from --with sources and --context bundle.
func resolveContextSources(opts WeaveOptions, cwd string) ([]icontext.Source, error) {
	var sources []icontext.Source

	if opts.ContextBundle != "" {
		bundle, err := icontext.LoadBundle(opts.ContextBundle, cwd)
		if err != nil {
			return nil, err
		}
		bundleSources, err := icontext.ResolveBundle(bundle, cwd)
		if err != nil {
			return nil, err
		}
		sources = append(sources, bundleSources...)
	}

	for _, spec := range opts.WithSources {
		src, err := icontext.Resolve(spec, cwd)
		if err != nil {
			return nil, err
		}
		sources = append(sources, src)
	}

	return sources, nil
}

func RunDeploy(opts DeployOptions, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}
	if len(cfg.Targets) == 0 {
		return "", fmt.Errorf("no [[targets]] configured in loom.toml")
	}

	var selected []config.Target
	for _, target := range cfg.Targets {
		if opts.TargetFormat != "" && target.Format != opts.TargetFormat {
			continue
		}
		selected = append(selected, target)
	}
	if len(selected) == 0 {
		if opts.TargetFormat != "" {
			return "", fmt.Errorf("no targets found for format %q", opts.TargetFormat)
		}
		return "", fmt.Errorf("no deploy targets selected")
	}

	var b strings.Builder
	written, unchanged, planned := 0, 0, 0

	for _, target := range selected {
		body, current, rp, dest, changed, err := renderDeployTarget(target, reg, cfg, cwd)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
				ErrorStyle.Render("✗"), PathStyle.Render(target.Dest), err))
			continue
		}

		if changed {
			if opts.Diff {
				diff := renderUnifiedDiff(current, body)
				if diff != "" {
					for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
						b.WriteString("     " + line + "\n")
					}
				}
			}
			if opts.DryRun {
				planned++
				b.WriteString(fmt.Sprintf("  %s  would write  %s\n",
					WarningStyle.Render("↺"), PathStyle.Render(dest)))
			} else {
				if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
					return "", err
				}
				if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
					return "", err
				}
				written++
				b.WriteString(fmt.Sprintf("  %s  wrote  %s\n",
					SuccessStyle.Render("✓"), PathStyle.Render(dest)))
			}
		} else {
			unchanged++
			b.WriteString(fmt.Sprintf("  %s  unchanged  %s\n",
				MutedStyle.Render("·"), PathStyle.Render(dest)))
		}

		b.WriteString("     " + MutedStyle.Render("prompt ") + PromptNameStyle.Render(target.Prompt))
		b.WriteString(MutedStyle.Render("  ·  format ") + PromptNameStyle.Render(target.Format))
		if rp.AppliedVariant != "" {
			b.WriteString(MutedStyle.Render("  ·  variant ") + PromptNameStyle.Render(rp.AppliedVariant))
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	switch {
	case opts.DryRun:
		b.WriteString(fmt.Sprintf("  %s planned  %s unchanged\n",
			WarningStyle.Render(fmt.Sprintf("%d targets", planned)),
			MutedStyle.Render(fmt.Sprintf("%d", unchanged))))
	default:
		b.WriteString(fmt.Sprintf("  %s written  %s unchanged\n",
			SuccessStyle.Render(fmt.Sprintf("%d targets", written)),
			MutedStyle.Render(fmt.Sprintf("%d", unchanged))))
	}

	return b.String(), nil
}

func renderDeployTarget(target config.Target, reg *registry.Registry, cfg *config.Config, cwd string) (string, string, *ast.ResolvedPrompt, string, bool, error) {
	if strings.TrimSpace(target.Prompt) == "" {
		return "", "", nil, "", false, fmt.Errorf("target is missing prompt")
	}
	if strings.TrimSpace(target.Format) == "" {
		return "", "", nil, "", false, fmt.Errorf("target for %q is missing format", target.Prompt)
	}
	if strings.TrimSpace(target.Dest) == "" {
		return "", "", nil, "", false, fmt.Errorf("target for %q is missing dest", target.Prompt)
	}

	rp, err := resolve.Resolve(target.Prompt, reg)
	if err != nil {
		return "", "", nil, "", false, err
	}
	if len(rp.UnresolvedTokens) > 0 {
		return "", "", nil, "", false, fmt.Errorf("unresolved variables: %s", strings.Join(rp.UnresolvedTokens, ", "))
	}

	body, _, err := render.RenderFormat(rp, cfg, target.Format)
	if err != nil {
		return "", "", nil, "", false, err
	}
	dest := target.Dest
	if !filepath.IsAbs(dest) {
		dest = filepath.Join(cwd, dest)
	}
	currentBytes, err := os.ReadFile(dest)
	if err != nil && !os.IsNotExist(err) {
		return "", "", nil, "", false, err
	}
	current := string(currentBytes)
	changed := current != body
	return body, current, rp, dest, changed, nil
}

func writeSourceMapFile(renderDest string, rp *ast.ResolvedPrompt, cwd string) (string, error) {
	body, err := sourcemap.Build(rp, time.Now(), cwd)
	if err != nil {
		return "", err
	}
	mapPath := sourceMapPath(renderDest)
	if err := os.WriteFile(mapPath, body, 0644); err != nil {
		return "", fmt.Errorf("writing %s: %w", mapPath, err)
	}
	return mapPath, nil
}

func sourceMapPath(renderDest string) string {
	ext := filepath.Ext(renderDest)
	if ext == "" {
		return renderDest + ".loom.map.json"
	}
	return strings.TrimSuffix(renderDest, ext) + ".loom.map.json"
}

func buildWeaveVariables(cfg *config.Config, cwd string, opts WeaveOptions) (map[string]string, error) {
	values := map[string]string{}

	if opts.Profile != "" {
		profile, ok := cfg.Profiles[opts.Profile]
		if !ok {
			return nil, fmt.Errorf("profile %q not found", opts.Profile)
		}
		for key, value := range profile {
			values[key] = value
		}
	}

	for key, value := range opts.Variables {
		values[key] = value
	}

	return values, nil
}

func requiredPromptVars(prompt *ast.Node, values map[string]string) []string {
	var missing []string
	for _, decl := range prompt.Vars {
		if decl.Required && values[decl.Name] == "" {
			missing = append(missing, decl.Name)
		}
	}
	sort.Strings(missing)
	return missing
}

func fillMissingSlots(prompt *ast.Node, values map[string]string) (map[string]string, error) {
	reader := bufio.NewReader(os.Stdin)
	values = cloneMap(values)

	for _, decl := range prompt.Vars {
		if !decl.IsSlot || values[decl.Name] != "" {
			continue
		}
		if !decl.Required && decl.Default != "" {
			values[decl.Name] = decl.Default
			continue
		}

		label := InputPromptStyle.Render(decl.Name)
		if decl.Default != "" {
			fmt.Printf("  %s %s %s ",
				label,
				MutedStyle.Render("["+decl.Default+"]"),
				MutedStyle.Render("›"))
		} else {
			fmt.Printf("  %s %s ", label, MutedStyle.Render("›"))
		}
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		input = strings.TrimSpace(input)
		if input == "" {
			input = decl.Default
		}
		if input == "" && decl.Required {
			return nil, fmt.Errorf("slot %q is required", decl.Name)
		}
		values[decl.Name] = input
	}

	return values, nil
}

func cloneMap(src map[string]string) map[string]string {
	out := make(map[string]string, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func ParseKVArgs(items []string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("expected key=value, got %q", item)
		}
		key := strings.TrimSpace(parts[0])
		if key == "" {
			return nil, fmt.Errorf("empty key in %q", item)
		}
		out[key] = parts[1]
	}
	return out, nil
}

func LoadVarsFile(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}

	var raw map[string]interface{}
	if _, err := toml.DecodeFile(path, &raw); err != nil {
		return nil, err
	}

	out := map[string]string{}
	for key, value := range raw {
		out[key] = fmt.Sprint(value)
	}
	return out, nil
}

func RunFingerprint(name, cwd string) (string, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}
	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Fingerprint") + "   " + PromptNameStyle.Render(name) + "\n")
	b.WriteString("  " + BrightStyle.Render(rp.Fingerprint) + "\n")
	return b.String(), nil
}

// RunFmt formats .loom source files in cwd.
func RunFmt(checkOnly bool, cwd string) (string, error) {
	cfg, err := config.Load(cwd)
	if err != nil {
		return "", err
	}

	promptDir := filepath.Join(cwd, cfg.Paths.Prompts)
	blockDir := filepath.Join(cwd, cfg.Paths.Blocks)
	overlayDir := filepath.Join(cwd, cfg.Paths.Overlays)

	type scanTarget struct {
		dir  string
		exts []string
	}
	var dirs []scanTarget
	if fi, e := os.Stat(promptDir); e == nil && fi.IsDir() {
		dirs = append(dirs, scanTarget{dir: promptDir, exts: []string{".prompt.loom", ".loom"}})
	}
	if fi, e := os.Stat(blockDir); e == nil && fi.IsDir() {
		dirs = append(dirs, scanTarget{dir: blockDir, exts: []string{".block.loom", ".loom"}})
	}
	if fi, e := os.Stat(overlayDir); e == nil && fi.IsDir() {
		dirs = append(dirs, scanTarget{dir: overlayDir, exts: []string{".overlay.loom", ".loom"}})
	}

	var b strings.Builder
	changed, total := 0, 0

	for _, target := range dirs {
		entries, err := os.ReadDir(target.dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !matchesLoomExts(entry.Name(), target.exts) {
				continue
			}
			path := filepath.Join(target.dir, entry.Name())
			total++

			src, err := os.ReadFile(path)
			if err != nil {
				b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
					ErrorStyle.Render("✗"), PathStyle.Render(path), err))
				continue
			}

			nodes, err := iparser.Parse(path, string(src))
			if err != nil {
				b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
					ErrorStyle.Render("✗"), PathStyle.Render(filepath.Base(path)), err))
				continue
			}
			formatted := iformat.Nodes(nodes)

			if string(src) == formatted {
				b.WriteString(fmt.Sprintf("  %s  %s\n",
					MutedStyle.Render("·"), MutedStyle.Render(filepath.Base(path))))
				continue
			}

			changed++
			if checkOnly {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
					WarningStyle.Render("⚠"),
					PathStyle.Render(filepath.Base(path)),
					MutedStyle.Render("(needs formatting)")))
			} else {
				if err := os.WriteFile(path, []byte(formatted), 0644); err != nil {
					b.WriteString(fmt.Sprintf("  %s  %s: %v\n",
						ErrorStyle.Render("✗"), PathStyle.Render(path), err))
					continue
				}
				b.WriteString(fmt.Sprintf("  %s  %s\n",
					SuccessStyle.Render("✓"),
					PathStyle.Render(filepath.Base(path))))
			}
		}
	}

	b.WriteByte('\n')
	if checkOnly {
		if changed == 0 {
			b.WriteString("  " + SuccessStyle.Render(fmt.Sprintf("All %d files are formatted correctly.", total)) + "\n")
		} else {
			b.WriteString("  " + WarningStyle.Render(fmt.Sprintf("%d of %d files need formatting.", changed, total)) + "\n")
		}
	} else {
		if changed == 0 {
			b.WriteString("  " + MutedStyle.Render(fmt.Sprintf("All %d files already formatted.", total)) + "\n")
		} else {
			b.WriteString("  " + SuccessStyle.Render(fmt.Sprintf("Formatted %d of %d files.", changed, total)) + "\n")
		}
	}

	return b.String(), nil
}

// promptNamesInFolder parses every .loom source file in dir and returns the names of
// all prompt nodes found (not blocks).
func promptNamesInFolder(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	loomExts := []string{".prompt.loom", ".loom"}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !matchesLoomExts(e.Name(), loomExts) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		nodes, err := iparser.Parse(filepath.Join(dir, e.Name()), string(src))
		if err != nil {
			continue
		}
		for _, n := range nodes {
			if n.Kind == ast.KindPrompt {
				names = append(names, n.Name)
			}
		}
	}
	sort.Strings(names)
	return names, nil
}

// RunWeaveFolder renders all prompts found directly inside prompts/<folder>.
func RunWeaveFolder(folder string, opts WeaveOptions, cwd string) (string, error) {
	cfg, err := config.Load(cwd)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, cfg.Paths.Prompts, folder)
	names, err := promptNamesInFolder(dir)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return MutedStyle.Render(fmt.Sprintf("  No prompts found in %s/", folder)) + "\n", nil
	}

	reg, regCfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}
	baseVars, err := buildWeaveVariables(regCfg, cwd, opts)
	if err != nil {
		return "", err
	}
	outDir := filepath.Join(cwd, regCfg.Paths.Out)
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render(fmt.Sprintf("Weaving %s/ (%d prompts)", folder, len(names))) + "\n\n")
	for _, name := range names {
		prompt, ok := reg.LookupPrompt(name)
		if !ok {
			b.WriteString(fmt.Sprintf("  %s  %s: prompt not found\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name)))
			continue
		}
		if missing := requiredPromptVars(prompt, baseVars); len(missing) > 0 {
			b.WriteString(fmt.Sprintf("  %s  %s: missing values for %s\n",
				ErrorStyle.Render("✗"), PromptNameStyle.Render(name), strings.Join(missing, ", ")))
			continue
		}
		rp, err := resolve.ResolveWithOptions(name, reg, resolve.Options{
			Variables: baseVars,
			Variant:   opts.Variant,
			Overlays:  opts.Overlays,
			Env:       opts.Env,
		})
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name), err))
			continue
		}
		if len(rp.UnresolvedTokens) > 0 {
			b.WriteString(fmt.Sprintf("  %s  %s: unresolved variables: %s\n",
				ErrorStyle.Render("✗"), PromptNameStyle.Render(name), strings.Join(rp.UnresolvedTokens, ", ")))
			continue
		}
		body, format, err := render.RenderFormat(rp, regCfg, opts.Format)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name), err))
			continue
		}
		dest := filepath.Join(outDir, format.DefaultFileName(name))
		if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name), err))
			continue
		}
		b.WriteString(fmt.Sprintf("  %s  wove  %s\n", SuccessStyle.Render("✓"), PathStyle.Render(dest)))
	}
	return b.String(), nil
}

// RunTraceFolder runs trace for every prompt in prompts/<folder>.
func RunTraceFolder(folder, cwd string) (string, error) {
	cfg, err := config.Load(cwd)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, cfg.Paths.Prompts, folder)
	names, err := promptNamesInFolder(dir)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return MutedStyle.Render(fmt.Sprintf("  No prompts found in %s/", folder)) + "\n", nil
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render(fmt.Sprintf("Tracing %s/ (%d prompts)", folder, len(names))) + "\n")
	for _, name := range names {
		b.WriteString("\n" + DividerStyle.Render("  ──────────────────────────────────────────────") + "\n")
		out, err := RunTrace(name, TraceOptions{}, cwd)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name), err))
			continue
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// RunUnravelFolder runs unravel for every prompt in prompts/<folder>.
func RunUnravelFolder(folder string, withSource bool, cwd string) (string, error) {
	cfg, err := config.Load(cwd)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cwd, cfg.Paths.Prompts, folder)
	names, err := promptNamesInFolder(dir)
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return MutedStyle.Render(fmt.Sprintf("  No prompts found in %s/", folder)) + "\n", nil
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render(fmt.Sprintf("Unravelling %s/ (%d prompts)", folder, len(names))) + "\n")
	for _, name := range names {
		b.WriteString("\n" + DividerStyle.Render("  ──────────────────────────────────────────────") + "\n")
		out, err := RunUnravel(name, withSource, cwd)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(name), err))
			continue
		}
		b.WriteString(out)
	}
	return b.String(), nil
}

// DiffOptions controls loom diff behavior.
type DiffOptions struct {
	AgainstDist bool
	All         bool
	ExitCode    bool
	Semantic    bool
}

// RunDiff performs a field-aware diff between two prompts or a prompt vs its dist file.
// Returns (output string, hasChanges bool, error).
func RunDiff(a, b string, opts DiffOptions, cwd string) (string, bool, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", false, err
	}

	if opts.All && opts.AgainstDist {
		return runDiffAllAgainstDist(reg, cfg, opts, cwd)
	}

	if opts.AgainstDist {
		return runDiffOneAgainstDist(a, reg, cfg, opts, cwd)
	}

	// Both names given: field-aware diff between two resolved prompts.
	rpA, err := resolve.Resolve(a, reg)
	if err != nil {
		return "", false, fmt.Errorf("resolving %q: %w", a, err)
	}
	rpB, err := resolve.Resolve(b, reg)
	if err != nil {
		return "", false, fmt.Errorf("resolving %q: %w", b, err)
	}

	diffs := idiff.DiffPrompts(rpA, rpB)
	hasChanges := false
	for _, d := range diffs {
		if d.Changed {
			hasChanges = true
			break
		}
	}

	var out string
	if opts.Semantic {
		out = renderSemanticDiff(rpA.Name, diffs)
	} else {
		out = renderFieldDiff(a+" vs "+b, diffs)
	}
	return out, hasChanges, nil
}

// runDiffOneAgainstDist resolves `name` fresh and diffs against its dist file.
func runDiffOneAgainstDist(name string, reg *registry.Registry, cfg *config.Config, opts DiffOptions, cwd string) (string, bool, error) {
	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return "", false, fmt.Errorf("resolving %q: %w", name, err)
	}
	current, _, err := render.RenderFormat(rp, cfg, "")
	if err != nil {
		return "", false, err
	}

	distPath := filepath.Join(cwd, cfg.Paths.Out, name+".md")
	distBytes, err := os.ReadFile(distPath)
	if err != nil && !os.IsNotExist(err) {
		return "", false, fmt.Errorf("reading dist file: %w", err)
	}
	distContent := string(distBytes)

	hasChanges := current != distContent
	label := fmt.Sprintf("%s (current) vs %s", name, distPath)

	var out string
	if opts.Semantic {
		// Parse old markdown into pseudo-prompt; copy InheritsChain from live rp to
		// avoid a false-positive inheritance-changed (markdown doesn't encode chain).
		rpOld := parseMarkdownIntoResolved(name, distContent)
		rpOld.InheritsChain = rp.InheritsChain
		diffs := idiff.DiffPrompts(rpOld, rp)
		out = renderSemanticDiff(name, diffs)
	} else {
		out = renderRawDiff(label, distContent, current)
	}
	return out, hasChanges, nil
}

// hasRequiredUnfilledSlots returns true if the prompt node declares any slot
// with Required=true and no default, meaning it cannot be woven without --set.
func hasRequiredUnfilledSlots(reg *registry.Registry, name string) bool {
	node, ok := reg.LookupPrompt(name)
	if !ok {
		return false
	}
	for _, v := range node.Vars {
		if v.IsSlot && v.Required && v.Default == "" {
			return true
		}
	}
	return false
}

// runDiffAllAgainstDist diffs all prompts against their dist files.
func runDiffAllAgainstDist(reg *registry.Registry, cfg *config.Config, opts DiffOptions, cwd string) (string, bool, error) {
	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

	var b strings.Builder
	anyChanges := false

	for _, p := range prompts {
		// Skip prompts that require interactive slot values — they can never have a
		// dist file produced by loom weave --all, so a missing dist is expected.
		distPath := filepath.Join(cwd, cfg.Paths.Out, p.Name+".md")
		if _, statErr := os.Stat(distPath); os.IsNotExist(statErr) {
			if hasRequiredUnfilledSlots(reg, p.Name) {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
					MutedStyle.Render("—"),
					PromptNameStyle.Render(p.Name),
					MutedStyle.Render("(skipped — requires slot values)")))
				continue
			}
		}

		out, changed, err := runDiffOneAgainstDist(p.Name, reg, cfg, opts, cwd)
		if err != nil {
			b.WriteString(fmt.Sprintf("  %s  %s: %v\n", ErrorStyle.Render("✗"), PromptNameStyle.Render(p.Name), err))
			continue
		}
		if changed {
			anyChanges = true
		}
		b.WriteString(out)
	}
	return b.String(), anyChanges, nil
}

// renderFieldDiff formats a field-aware diff with headings and +/- lines.
func renderFieldDiff(label string, diffs []idiff.FieldDiff) string {
	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 47))

	b.WriteString("\n  " + BrightStyle.Render("Diff: "+label) + "\n\n")

	anyChanged := false
	for _, d := range diffs {
		if !d.Changed {
			continue
		}
		anyChanged = true

		b.WriteString("  " + BrightStyle.Render(d.Heading) + "\n")
		b.WriteString("  " + sep + "\n")

		if d.IsList {
			removedSet := make(map[string]bool)
			for _, r := range d.Removed {
				removedSet[r] = true
			}
			addedSet := make(map[string]bool)
			for _, a := range d.Added {
				addedSet[a] = true
			}
			// Show removed lines.
			for _, r := range d.Removed {
				b.WriteString(ErrorStyle.Render("  - "+r) + "\n")
			}
			// Show added lines.
			for _, a := range d.Added {
				b.WriteString(SuccessStyle.Render("  + "+a) + "\n")
			}
		} else {
			if d.Before != "" {
				b.WriteString(ErrorStyle.Render("  - "+d.Before) + "\n")
			}
			if d.After != "" {
				b.WriteString(SuccessStyle.Render("  + "+d.After) + "\n")
			}
		}
		b.WriteByte('\n')
	}

	if !anyChanged {
		b.WriteString("  " + MutedStyle.Render("(no changes)") + "\n")
	}
	return b.String()
}

// renderRawDiff renders a raw line-by-line diff with a label header.
func renderRawDiff(label, before, after string) string {
	var b strings.Builder
	b.WriteString("\n  " + BrightStyle.Render("Diff: "+label) + "\n\n")
	if before == after {
		b.WriteString("  " + MutedStyle.Render("(no changes)") + "\n")
		return b.String()
	}
	diff := renderUnifiedDiff(before, after)
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

// renderSemanticDiff formats classified semantic changes.
func renderSemanticDiff(name string, diffs []idiff.FieldDiff) string {
	classes := semantic.Classify(diffs)
	sep := MutedStyle.Render(strings.Repeat("─", 47))

	var b strings.Builder
	b.WriteString("\n  " + BrightStyle.Render("Semantic diff: "+name) + "\n")
	b.WriteString("  " + sep + "\n\n")

	if len(classes) == 0 {
		b.WriteString("  " + MutedStyle.Render("(no semantic changes)") + "\n")
		return b.String()
	}

	for _, cls := range classes {
		riskColor := MutedStyle
		switch cls.Risk {
		case semantic.RiskHigh:
			riskColor = ErrorStyle
		case semantic.RiskMedium:
			riskColor = WarningStyle
		case semantic.RiskLow:
			riskColor = SuccessStyle
		}
		label := BrightStyle.Render(cls.Label)
		risk := riskColor.Render(fmt.Sprintf("(%s risk)", cls.Risk))
		b.WriteString(fmt.Sprintf("  %s  %s\n", label, risk))
		for _, item := range cls.Items {
			if item == "" {
				continue
			}
			if strings.HasPrefix(item, "-") || (len(cls.Items) == 2 && cls.Items[0] == item) {
				b.WriteString(ErrorStyle.Render("    - "+item) + "\n")
			} else {
				b.WriteString(SuccessStyle.Render("    + "+item) + "\n")
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// parseMarkdownIntoResolved parses a rendered markdown document back into a ResolvedPrompt
// by extracting section contents. Used for --against-dist semantic diff.
func parseMarkdownIntoResolved(name, md string) *ast.ResolvedPrompt {
	rp := &ast.ResolvedPrompt{Name: name}
	lines := strings.Split(md, "\n")

	sectionMap := map[string]string{
		"Summary":       "summary",
		"Persona":       "persona",
		"Context":       "context",
		"Objective":     "objective",
		"Notes":         "notes",
		"Instructions":  "instructions",
		"Constraints":   "constraints",
		"Examples":      "examples",
		"Output Format": "format",
	}

	current := ""
	var scalarBuf strings.Builder
	var listBuf []string

	flush := func() {
		if current == "" {
			return
		}
		switch current {
		case "summary":
			rp.Summary = strings.TrimSpace(scalarBuf.String())
		case "persona":
			rp.Persona = strings.TrimSpace(scalarBuf.String())
		case "context":
			rp.Context = strings.TrimSpace(scalarBuf.String())
		case "objective":
			rp.Objective = strings.TrimSpace(scalarBuf.String())
		case "notes":
			rp.Notes = strings.TrimSpace(scalarBuf.String())
		case "instructions":
			rp.Instructions = listBuf
		case "constraints":
			rp.Constraints = listBuf
		case "examples":
			rp.Examples = listBuf
		case "format":
			rp.Format = listBuf
		}
		scalarBuf.Reset()
		listBuf = nil
	}

	listFields := map[string]bool{
		"instructions": true, "constraints": true,
		"examples": true, "format": true,
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "## ") {
			flush()
			heading := strings.TrimPrefix(line, "## ")
			if f, ok := sectionMap[heading]; ok {
				current = f
			} else {
				current = ""
			}
			continue
		}
		if current == "" {
			continue
		}
		if listFields[current] {
			item := strings.TrimPrefix(line, "- ")
			if item != line && item != "" {
				listBuf = append(listBuf, item)
			}
		} else {
			if scalarBuf.Len() > 0 && line != "" {
				scalarBuf.WriteByte('\n')
			}
			if line != "" {
				scalarBuf.WriteString(line)
			}
		}
	}
	flush()
	return rp
}

type promptReview struct {
	name    string
	classes []semantic.ChangeClass
}

// RunReview generates a Markdown PR summary of all prompt changes.
// If since is non-empty it compares current renders against the git ref.
func RunReview(since string, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

	var reviews []promptReview

	for _, p := range prompts {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			continue
		}
		current, _, err := render.RenderFormat(rp, cfg, "")
		if err != nil {
			continue
		}

		var oldContent string
		if since != "" {
			// git show <ref>:dist/prompts/<Name>.md
			distRelPath := filepath.Join(cfg.Paths.Out, p.Name+".md")
			gitRef := since + ":" + distRelPath
			out, gitErr := runGitShow(gitRef, cwd)
			if gitErr != nil {
				// Not in old ref — skip (new prompt).
				continue
			}
			oldContent = out
		} else {
			distPath := filepath.Join(cwd, cfg.Paths.Out, p.Name+".md")
			distBytes, readErr := os.ReadFile(distPath)
			if readErr != nil {
				continue
			}
			oldContent = string(distBytes)
		}

		if oldContent == current {
			continue
		}

		rpOld := parseMarkdownIntoResolved(p.Name, oldContent)
		rpOld.InheritsChain = rp.InheritsChain // markdown doesn't encode chain
		diffs := idiff.DiffPrompts(rpOld, rp)
		classes := semantic.Classify(diffs)
		if len(classes) > 0 {
			reviews = append(reviews, promptReview{name: p.Name, classes: classes})
		}
	}

	return buildReviewMarkdown(reviews), nil
}

func runGitShow(ref, cwd string) (string, error) {
	cmd := exec.Command("git", "show", ref)
	cmd.Dir = cwd
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git show %s: %w", ref, err)
	}
	return out.String(), nil
}

func buildReviewMarkdown(reviews []promptReview) string {
	if len(reviews) == 0 {
		return "## PromptLoom Prompt Review\n\nNo prompt changes detected.\n"
	}

	var b strings.Builder
	b.WriteString("## PromptLoom Prompt Review\n\n")

	var names []string
	for _, r := range reviews {
		names = append(names, r.name)
	}
	b.WriteString("**Changed prompts:** " + strings.Join(names, ", ") + "\n\n")

	highRiskCount := 0
	for _, r := range reviews {
		b.WriteString("**" + r.name + "**\n")
		for _, cls := range r.classes {
			summary := summariseClass(cls)
			b.WriteString(fmt.Sprintf("- %s: %s\n", cls.Label, summary))
			if cls.Risk == semantic.RiskHigh {
				highRiskCount++
			}
		}
		b.WriteByte('\n')
	}

	b.WriteString("**Risk summary:** ")
	if highRiskCount > 0 {
		b.WriteString(fmt.Sprintf("%d high-risk change(s). Review carefully before merging.\n", highRiskCount))
	} else {
		b.WriteString("No high-risk changes detected.\n")
	}

	return b.String()
}

func summariseClass(cls semantic.ChangeClass) string {
	count := len(cls.Items)
	switch cls.Label {
	case "constraint-added":
		return fmt.Sprintf("%d new constraint(s) added", count)
	case "constraint-removed":
		if count > 0 {
			return fmt.Sprintf("%s removed (high risk)", cls.Items[0])
		}
		return "constraint removed (high risk)"
	case "capability-added":
		return fmt.Sprintf("%d new instruction(s) added", count)
	case "capability-removed":
		return fmt.Sprintf("%d instruction(s) removed", count)
	case "format-changed":
		return "output format modified"
	case "objective-changed":
		return "objective text changed"
	case "persona-changed":
		return "persona text changed"
	case "notes-updated":
		return "minor wording change"
	case "examples-changed":
		return "examples modified"
	case "inheritance-changed":
		return "inheritance chain changed (high risk)"
	}
	return fmt.Sprintf("%d item(s) changed", count)
}

// RunDoctor runs health checks on one prompt or all prompts.
func RunDoctor(name string, all bool, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	var b strings.Builder

	if all {
		reports, err := idoctor.CheckAll(reg, cfg, cwd)
		if err != nil {
			return "", err
		}
		unused := idoctor.UnusedBlocks(reg)

		for _, r := range reports {
			b.WriteString(renderDoctorReport(r, cfg))
			b.WriteByte('\n')
		}

		if len(unused) > 0 {
			b.WriteString("  " + SubHeaderStyle.Render("Unused Blocks") + "\n")
			b.WriteString("  " + Divider(44) + "\n")
			for _, name := range unused {
				b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
					WarningStyle.Render("⚠"),
					BlockNameStyle.Render(name),
					MutedStyle.Render("(not used by any prompt)")))
			}
			b.WriteByte('\n')
		}

		total := len(reports)
		passing := 0
		for _, r := range reports {
			if r.Score >= 75 {
				passing++
			}
		}
		b.WriteString(fmt.Sprintf("  %s prompts checked — %s healthy, %s need attention\n",
			PromptNameStyle.Render(fmt.Sprintf("%d", total)),
			SuccessStyle.Render(fmt.Sprintf("%d", passing)),
			fmtCount(total-passing, false),
		))
		return b.String(), nil
	}

	report, err := idoctor.CheckPrompt(name, reg, cfg, cwd)
	if err != nil {
		return "", err
	}
	b.WriteString(renderDoctorReport(report, cfg))
	return b.String(), nil
}

func renderDoctorReport(r *idoctor.HealthReport, cfg *config.Config) string {
	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 44))

	b.WriteString("\n  " + HeaderStyle.Render("loom doctor") + " — " + PromptNameStyle.Render(r.Name) + "\n\n")

	b.WriteString("  " + SubHeaderStyle.Render("Structural") + "\n")
	b.WriteString("  " + sep + "\n")
	for _, c := range r.Structurals {
		if c.Pass {
			icon := SuccessStyle.Render("✓")
			detail := ""
			if c.Detail != "" {
				detail = "  " + MutedStyle.Render(c.Detail)
			}
			b.WriteString(fmt.Sprintf("  %s  %s%s\n", icon, TextStyle.Render(c.Label), detail))
		} else if c.IsWarn {
			icon := WarningStyle.Render("⚠")
			detail := ""
			if c.Detail != "" {
				detail = "  " + MutedStyle.Render(c.Detail)
			}
			b.WriteString(fmt.Sprintf("  %s  %s%s\n", icon, WarningStyle.Render(c.Label), detail))
		} else {
			icon := ErrorStyle.Render("✗")
			detail := ""
			if c.Detail != "" {
				detail = "  " + ErrorStyle.Render(c.Detail)
			}
			b.WriteString(fmt.Sprintf("  %s  %s%s\n", icon, ErrorStyle.Render(c.Label), detail))
		}
	}
	b.WriteByte('\n')

	scoreColor := SuccessStyle
	switch {
	case r.Score < 40:
		scoreColor = ErrorStyle
	case r.Score < 60:
		scoreColor = WarningStyle
	case r.Score < 75:
		scoreColor = WarningStyle
	}

	b.WriteString(fmt.Sprintf("  %s  %s\n",
		SubHeaderStyle.Render("Prompt Health"),
		scoreColor.Render(fmt.Sprintf("%d/100  %s", r.Score, r.Band))))
	b.WriteString("  " + sep + "\n")

	if len(r.Smells) == 0 {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			SuccessStyle.Render("✓"),
			TextStyle.Render("No smells detected")))
	} else {
		for _, s := range r.Smells {
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				WarningStyle.Render("⚠"),
				WarningStyle.Render(s.Name)))
			b.WriteString(fmt.Sprintf("      %s\n", MutedStyle.Render(s.Detail)))
		}
	}

	b.WriteByte('\n')
	smellCount := len(r.Smells)
	if smellCount == 0 {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			SuccessStyle.Render("✓"),
			MutedStyle.Render("Smells: none")))
	} else {
		b.WriteString(fmt.Sprintf("  %s\n",
			WarningStyle.Render(fmt.Sprintf("  Smells: %d warning(s) — run 'loom smells %s' for details", smellCount, r.Name))))
	}

	return b.String()
}

// RunSmells reports only the smell analysis for one or all prompts.
func RunSmells(name string, all bool, cwd string) (string, error) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 44))

	if all {
		reports, err := idoctor.CheckAll(reg, cfg, cwd)
		if err != nil {
			return "", err
		}
		total := 0
		for _, r := range reports {
			if len(r.Smells) == 0 {
				continue
			}
			b.WriteString("\n  " + PromptNameStyle.Render(r.Name) + "\n")
			b.WriteString("  " + sep + "\n")
			for _, s := range r.Smells {
				b.WriteString(fmt.Sprintf("  %s  %s\n",
					WarningStyle.Render("⚠"),
					WarningStyle.Render(s.Name)))
				b.WriteString(fmt.Sprintf("      %s\n", MutedStyle.Render(s.Detail)))
			}
			total += len(r.Smells)
		}
		if total == 0 {
			b.WriteString("  " + SuccessStyle.Render("✓ No smells detected across the library") + "\n")
		} else {
			b.WriteByte('\n')
			b.WriteString(fmt.Sprintf("  %s across all prompts\n",
				WarningStyle.Render(fmt.Sprintf("%d smell(s)", total))))
		}
		return b.String(), nil
	}

	report, err := idoctor.CheckPrompt(name, reg, cfg, cwd)
	if err != nil {
		return "", err
	}

	b.WriteString("\n  " + HeaderStyle.Render("Smells") + " — " + PromptNameStyle.Render(name) + "\n\n")
	if len(report.Smells) == 0 {
		b.WriteString("  " + SuccessStyle.Render("✓ No smells detected") + "\n")
		return b.String(), nil
	}

	b.WriteString("  " + sep + "\n")
	for _, s := range report.Smells {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			WarningStyle.Render("⚠"), WarningStyle.Render(s.Name)))
		b.WriteString(fmt.Sprintf("      %s\n", MutedStyle.Render(s.Detail)))
	}
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  %s\n",
		MutedStyle.Render(fmt.Sprintf("%d smell(s) found", len(report.Smells)))))
	return b.String(), nil
}

// RunContract prints the declared contract for a named prompt.
func RunContract(name string, cwd string) (string, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}
	node, ok := reg.LookupPrompt(name)
	if !ok {
		return "", fmt.Errorf("prompt %q not found", name)
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("Contract") + " — " + PromptNameStyle.Render(name) + "\n\n")

	if node.Contract == nil && node.Capabilities == nil {
		b.WriteString("  " + MutedStyle.Render("No contract or capabilities declared.") + "\n")
		b.WriteString("  " + MutedStyle.Render("Add a contract { ... } block to the prompt file.") + "\n")
		return b.String(), nil
	}

	sep := MutedStyle.Render(strings.Repeat("─", 44))

	if node.Contract != nil {
		c := node.Contract
		b.WriteString("  " + SubHeaderStyle.Render("Output Contract") + "\n")
		b.WriteString("  " + sep + "\n")
		renderContractList(&b, "Required sections", c.RequiredSections)
		renderContractList(&b, "Forbidden sections", c.ForbiddenSections)
		renderContractList(&b, "Must include", c.MustInclude)
		renderContractList(&b, "Must not include", c.MustNotInclude)
		b.WriteByte('\n')
	}

	if node.Capabilities != nil {
		caps := node.Capabilities
		b.WriteString("  " + SubHeaderStyle.Render("Capabilities") + "\n")
		b.WriteString("  " + sep + "\n")
		renderContractList(&b, "Allowed", caps.Allowed)
		renderContractList(&b, "Forbidden", caps.Forbidden)
		b.WriteByte('\n')
	}

	return b.String(), nil
}

func renderContractList(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(fmt.Sprintf("  %s\n", BrightStyle.Render(label+":")))
	for _, item := range items {
		b.WriteString(fmt.Sprintf("    %s %s\n",
			BulletStyle.Render("–"), TextStyle.Render(item)))
	}
}

// RunCheckOutput validates a rendered output file against a prompt's contract.
// Returns (output string, passed bool, error).
func RunCheckOutput(promptName, outputPath string, cwd string) (string, bool, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", false, err
	}
	node, ok := reg.LookupPrompt(promptName)
	if !ok {
		return "", false, fmt.Errorf("prompt %q not found", promptName)
	}
	if node.Contract == nil {
		return ErrorStyle.Render(fmt.Sprintf("Error: prompt %q has no contract block — nothing to check", promptName)) + "\n", false, nil
	}

	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(cwd, outputPath)
	}
	outputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return "", false, fmt.Errorf("reading output file: %w", err)
	}
	outputText := string(outputBytes)

	failures := icontracts.Check(node.Contract, outputText)

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("check-output") + " — " + PromptNameStyle.Render(promptName) + "\n")
	b.WriteString("  " + MutedStyle.Render(outputPath) + "\n\n")

	if len(failures) == 0 {
		b.WriteString("  " + SuccessStyle.Render("✓ Output satisfies all contract requirements") + "\n")
		return b.String(), true, nil
	}

	for _, f := range failures {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			ErrorStyle.Render("✗"),
			ErrorStyle.Render(f.Detail)))
	}
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  %s\n",
		ErrorStyle.Render(fmt.Sprintf("%d contract violation(s) found", len(failures)))))
	return b.String(), false, nil
}

// WatchWeave runs a blocking watch loop: re-renders prompts whenever a source
// file changes. It returns only when the process receives SIGINT/SIGTERM.
func WatchWeave(opts WeaveOptions, cwd string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("creating watcher: %w", err)
	}
	defer watcher.Close()

	cfg, err := config.Load(cwd)
	if err != nil {
		return err
	}

	for _, rel := range []string{cfg.Paths.Prompts, cfg.Paths.Blocks, cfg.Paths.Overlays} {
		dir := filepath.Join(cwd, rel)
		if fi, e := os.Stat(dir); e == nil && fi.IsDir() {
			if e := watcher.Add(dir); e != nil {
				return fmt.Errorf("watching %s: %w", dir, e)
			}
		}
	}

	fmt.Printf("  %s  watching for changes in %s  (%s to stop)\n\n",
		SuccessStyle.Render("◈"),
		PathStyle.Render(filepath.Base(cwd)),
		MutedStyle.Render("Ctrl+C"))

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	// Debounce: collect events for 80ms before rebuilding.
	debounce := time.NewTimer(0)
	<-debounce.C // drain the initial zero-duration fire

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) {
				if !isPromptFile(event.Name) {
					continue
				}
				debounce.Reset(80 * time.Millisecond)
			}

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			fmt.Printf("  %s  %v\n", ErrorStyle.Render("✗"), watchErr)

		case <-debounce.C:
			start := time.Now()
			out, rebuildErr := RunWeave("", true, opts, cwd)
			elapsed := time.Since(start)
			ts := MutedStyle.Render("[" + time.Now().Format("15:04:05") + "]")
			if rebuildErr != nil {
				fmt.Printf("  %s  %s  rebuild failed: %v\n", ts, ErrorStyle.Render("✗"), rebuildErr)
			} else {
				fmt.Printf("  %s  rebuilt in %dms\n%s\n", ts, elapsed.Milliseconds(), out)
			}

		case <-sigCh:
			fmt.Printf("\n  %s  stopped.\n", MutedStyle.Render("◈"))
			return nil
		}
	}
}

// matchesLoomExts reports whether filename ends with any of the given suffixes.
func matchesLoomExts(name string, exts []string) bool {
	for _, ext := range exts {
		if strings.HasSuffix(name, ext) {
			return true
		}
	}
	return false
}

func isPromptFile(path string) bool {
	base := filepath.Base(path)
	return matchesLoomExts(base, []string{
		".prompt.loom", ".block.loom", ".overlay.loom", ".vars.loom", ".loom", ".context",
	})
}

// RunLock generates and writes loom.lock from the current registry state.
func RunLock(cwd string) (string, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", err
	}

	lf, err := ilock.Generate(reg, cwd)
	if err != nil {
		return "", err
	}
	if err := ilock.Write(lf, cwd); err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("\n  " + HeaderStyle.Render("loom.lock") + "\n\n")
	for _, p := range lf.Prompts {
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			SuccessStyle.Render("✓"),
			PromptNameStyle.Render(fmt.Sprintf("%-28s", p.Name)),
			MutedStyle.Render(shortHash(p.Hash))))
	}
	for _, bl := range lf.Blocks {
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			MutedStyle.Render("·"),
			BlockNameStyle.Render(fmt.Sprintf("%-28s", bl.Name)),
			MutedStyle.Render(shortHash(bl.Hash))))
	}
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  %s written to %s\n",
		SuccessStyle.Render("✓"),
		PathStyle.Render(ilock.Path(cwd))))
	return b.String(), nil
}

// RunCheckLock verifies current fingerprints match loom.lock.
// Returns (output, hasMismatch, error).
func RunCheckLock(cwd string) (string, bool, error) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return "", false, err
	}

	mismatches, err := ilock.Check(reg, cwd)
	if err != nil {
		out := ErrorStyle.Render("Error: "+err.Error()) + "\n"
		return out, true, nil
	}

	var b strings.Builder
	if len(mismatches) == 0 {
		b.WriteString("  " + SuccessStyle.Render("✓ Lockfile matches current state") + "\n")
		return b.String(), false, nil
	}

	b.WriteString("\n  " + HeaderStyle.Render("check-lock — mismatches") + "\n\n")
	for _, m := range mismatches {
		icon := WarningStyle.Render("⚠")
		label := PromptNameStyle.Render(m.Name)
		if m.Kind == "block" {
			label = BlockNameStyle.Render(m.Name)
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s → %s\n",
			icon,
			label,
			MutedStyle.Render(shortHash(m.Locked)),
			ErrorStyle.Render(shortHash(m.Current))))
	}
	b.WriteByte('\n')
	b.WriteString(fmt.Sprintf("  %s — run %s to update\n",
		ErrorStyle.Render(fmt.Sprintf("%d mismatch(es)", len(mismatches))),
		CommandStyle.Render("loom lock")))
	return b.String(), true, nil
}

func shortHash(h string) string {
	if len(h) > 15 {
		return h[:15] + "…"
	}
	return h
}

// CIResult holds one CI gate result.
type CIResult struct {
	Name   string
	Passed bool
	Detail string
	Skip   bool
}

// RunCI runs all CI gates sequentially and returns a structured report.
func RunCI(cwd string) (string, bool, error) {
	var results []CIResult
	failed := false

	// Gate 1: inspect — run the validator directly for a clean summary.
	reg, cfg, regErr := loader.Load(cwd)
	var inspectPassed bool
	var inspectDetail string
	if regErr != nil {
		inspectPassed = false
		inspectDetail = regErr.Error()
	} else {
		diags := validate.Validate(reg, cfg)
		errors, warnings := 0, 0
		for _, d := range diags {
			if d.Sev == validate.Error {
				errors++
			} else {
				warnings++
			}
		}
		inspectPassed = errors == 0
		inspectDetail = fmt.Sprintf("%d error(s), %d warning(s)", errors, warnings)
	}
	if !inspectPassed {
		failed = true
	}
	results = append(results, CIResult{Name: "inspect", Passed: inspectPassed, Detail: inspectDetail})

	// Gate 2: doctor
	doctorOut, doctorErr := RunDoctor("", true, cwd)
	doctorPassed := doctorErr == nil
	if !doctorPassed {
		failed = true
	}
	doctorDetail := extractDoctorSummary(doctorOut)
	results = append(results, CIResult{Name: "doctor", Passed: doctorPassed, Detail: doctorDetail})

	// Gate 3: check-lock
	lockOut, lockMismatch, lockErr := RunCheckLock(cwd)
	lockPassed := lockErr == nil && !lockMismatch
	if !lockPassed {
		failed = true
	}
	lockDetail := extractSummaryLine(lockOut)
	if lockErr != nil {
		lockDetail = lockErr.Error()
	}
	results = append(results, CIResult{Name: "check-lock", Passed: lockPassed, Detail: lockDetail})

	// Gate 4: diff --all --against-dist --exit-code (skip if lock failed)
	if !lockPassed {
		results = append(results, CIResult{Name: "diff", Skip: true, Detail: "(skipped — check-lock failed)"})
	} else {
		_, hasChanges, diffErr := RunDiff("", "", DiffOptions{All: true, AgainstDist: true, ExitCode: true}, cwd)
		diffPassed := diffErr == nil && !hasChanges
		if !diffPassed {
			failed = true
		}
		diffDetail := "all dist files are up-to-date"
		if hasChanges {
			diffDetail = "stale dist files detected — run loom weave"
		}
		if diffErr != nil {
			diffDetail = diffErr.Error()
		}
		results = append(results, CIResult{Name: "diff", Passed: diffPassed, Detail: diffDetail})
	}

	// Gate 5: test — only run if API key is available; skip gracefully otherwise.
	if reg != nil {
		apiKeyEnv := cfg.Testing.APIKeyEnv
		if apiKeyEnv == "" {
			switch cfg.Testing.Provider {
			case "anthropic":
				apiKeyEnv = "ANTHROPIC_API_KEY"
			default:
				apiKeyEnv = "GEMINI_API_KEY"
			}
		}
		if os.Getenv(apiKeyEnv) == "" {
			results = append(results, CIResult{Name: "test", Skip: true, Detail: fmt.Sprintf("(skipped — $%s not set)", apiKeyEnv)})
		} else {
			testResults := itestrunner.RunAll(reg, cfg, cwd, itestrunner.Options{})
			testPassed := true
			testTotal, testPass, testFail := 0, 0, 0
			for _, tr := range testResults {
				if tr.Skipped {
					continue
				}
				testTotal++
				if tr.Err != nil || !tr.Passed {
					testPassed = false
					testFail++
				} else {
					testPass++
				}
			}
			if !testPassed {
				failed = true
			}
			testDetail := fmt.Sprintf("%d/%d passed", testPass, testTotal)
			if testTotal == 0 {
				testDetail = "no contracts declared"
			}
			results = append(results, CIResult{Name: "test", Passed: testPassed || testTotal == 0, Detail: testDetail})
		}
	}

	// Gate 6: audit — scan for dangerous instructions (HIGH findings fail CI).
	if reg != nil {
		auditPassed := true
		highCount, medCount := 0, 0
		for _, node := range reg.Prompts() {
			rp, err := resolve.ResolveWithOptions(node.Name, reg, resolve.Options{Env: "prod"})
			if err != nil {
				// env "prod" may not be declared on all prompts — skip gracefully.
				rp, err = resolve.ResolveWithOptions(node.Name, reg, resolve.Options{})
				if err != nil {
					continue
				}
			}
			findings := iaudit.Audit(rp)
			for _, f := range findings {
				switch f.Risk {
				case iaudit.High:
					highCount++
					auditPassed = false
				case iaudit.Medium:
					medCount++
				}
			}
		}
		if !auditPassed {
			failed = true
		}
		auditDetail := fmt.Sprintf("%d high, %d medium", highCount, medCount)
		if highCount == 0 && medCount == 0 {
			auditDetail = "no findings"
		}
		results = append(results, CIResult{Name: "audit", Passed: auditPassed, Detail: auditDetail})
	}

	var b strings.Builder
	sep := MutedStyle.Render(strings.Repeat("─", 50))

	b.WriteString("\n  " + HeaderStyle.Render("PromptLoom CI") + "\n\n")
	b.WriteString("  " + sep + "\n\n")

	for _, r := range results {
		var icon, nameStr, detailStr string
		if r.Skip {
			icon = MutedStyle.Render("—")
			nameStr = MutedStyle.Render(fmt.Sprintf("%-12s", r.Name))
			detailStr = MutedStyle.Render(r.Detail)
		} else if r.Passed {
			icon = SuccessStyle.Render("✓")
			nameStr = SuccessStyle.Render(fmt.Sprintf("%-12s", r.Name))
			detailStr = MutedStyle.Render(r.Detail)
		} else {
			icon = ErrorStyle.Render("✗")
			nameStr = ErrorStyle.Render(fmt.Sprintf("%-12s", r.Name))
			detailStr = ErrorStyle.Render(r.Detail)
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", icon, nameStr, detailStr))
	}

	b.WriteByte('\n')
	b.WriteString("  " + sep + "\n")
	if failed {
		b.WriteString("  " + ErrorStyle.Render("Status: FAILED") + "\n")
	} else {
		b.WriteString("  " + SuccessStyle.Render("Status: PASSED") + "\n")
	}

	return b.String(), failed, nil
}

// extractSummaryLine pulls the last meaningful non-empty line from styled output,
// skipping lines that consist only of box-drawing or decorative characters.
func extractSummaryLine(out string) string {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		clean := strings.TrimSpace(stripANSI(lines[i]))
		if clean == "" || isDecorativeLine(clean) {
			continue
		}
		return clean
	}
	return ""
}

// isDecorativeLine returns true for lines that are only box-drawing / separator chars.
func isDecorativeLine(s string) bool {
	for _, r := range s {
		switch r {
		case '─', '│', '╭', '╮', '╯', '╰', '┤', '├', '┬', '┴', '┼', ' ', '·', '—':
			// decorative
		default:
			return false
		}
	}
	return true
}

func extractDoctorSummary(out string) string {
	clean := stripANSI(out)
	for _, line := range strings.Split(clean, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "prompts checked") {
			return line
		}
	}
	return extractSummaryLine(out)
}

// stripANSI removes ANSI escape sequences for plain-text extraction.
func stripANSI(s string) string {
	var out strings.Builder
	inSeq := false
	for _, r := range s {
		if r == '\x1b' {
			inSeq = true
			continue
		}
		if inSeq {
			if r == 'm' {
				inSeq = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

// LoadIncrementalCache reads .loom-cache from cwd.
func LoadIncrementalCache(cwd string) map[string]string {
	path := filepath.Join(cwd, ".loom-cache")
	var raw struct {
		Hashes map[string]string `toml:"hashes"`
	}
	if _, err := toml.DecodeFile(path, &raw); err != nil || raw.Hashes == nil {
		return map[string]string{}
	}
	return raw.Hashes
}

// SaveIncrementalCache writes the hash map to .loom-cache in cwd.
func SaveIncrementalCache(cwd string, hashes map[string]string) error {
	type cacheFile struct {
		Hashes map[string]string `toml:"hashes"`
	}
	var buf strings.Builder
	buf.WriteString("# PromptLoom incremental render cache — do not edit by hand\n\n")
	buf.WriteString("[hashes]\n")
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		buf.WriteString(fmt.Sprintf("  %s = %q\n", k, hashes[k]))
	}
	return os.WriteFile(filepath.Join(cwd, ".loom-cache"), []byte(buf.String()), 0644)
}

// PromptNames returns a sorted list of prompt names for tab completion.
func PromptNames(cwd string) []string {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return nil
	}
	prompts := reg.Prompts()
	names := make([]string, len(prompts))
	for i, p := range prompts {
		names[i] = p.Name
	}
	sort.Strings(names)
	return names
}

// LibraryStats returns prompt count, block count, error count for the banner.
func LibraryStats(cwd string) (prompts, blocks, errs int) {
	reg, cfg, err := loader.Load(cwd)
	if err != nil {
		return -1, -1, -1
	}
	diags := validate.Validate(reg, cfg)
	for _, d := range diags {
		if d.Sev == validate.Error {
			errs++
		}
	}
	return reg.PromptCount(), reg.BlockCount(), errs
}

// ── Graph ─────────────────────────────────────────────────────────────────

// RunGraph renders the prompt dependency graph. format is "ascii", "mermaid",
// or "dot". If name is non-empty, only the subgraph rooted at that prompt is
// shown. If unused is true, blocks not referenced by any prompt are listed.
func RunGraph(name, format string, unused bool, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	g := igraph.Build(reg)

	if unused {
		blocks := g.Unused()
		var b strings.Builder
		b.WriteString("  " + HeaderStyle.Render("Unused Blocks") + "\n")
		b.WriteString("  " + Divider(40) + "\n")
		if len(blocks) == 0 {
			b.WriteString("  " + SuccessStyle.Render("✓ All blocks are in use") + "\n")
		} else {
			for _, bl := range blocks {
				b.WriteString(fmt.Sprintf("  %s  %s\n",
					WarningStyle.Render("⚠"),
					BlockNameStyle.Render(bl)))
			}
		}
		return b.String(), false
	}

	switch format {
	case "mermaid":
		return g.Mermaid(), false
	case "dot":
		return g.DOT(), false
	default: // ascii
		var b strings.Builder
		b.WriteString("  " + HeaderStyle.Render("Dependency Graph") + "\n")
		b.WriteString("  " + Divider(60) + "\n")
		if name != "" {
			b.WriteString(g.ASCIISubgraph(name))
		} else {
			b.WriteString(g.ASCII())
		}
		return b.String(), false
	}
}

// ── Stats ─────────────────────────────────────────────────────────────────

type fieldStat struct {
	name   string
	tokens int
}

// RunStats renders per-field token estimates for one or all prompts.
func RunStats(name string, all bool, limit int, cwd string) (string, bool) {
	reg, _, err := loader.Load(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}

	if all {
		return runStatsAll(reg, limit), false
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return renderStats(rp, limit), false
}

func runStatsAll(reg *registry.Registry, limit int) string {
	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

	type row struct {
		name   string
		tokens int
	}
	var rows []row
	for _, p := range prompts {
		rp, err := resolve.Resolve(p.Name, reg)
		if err != nil {
			continue
		}
		rows = append(rows, row{p.Name, totalTokens(rp)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].tokens > rows[j].tokens })

	var b strings.Builder
	b.WriteString("  " + HeaderStyle.Render("Token Estimates") + "\n")
	b.WriteString("  " + Divider(50) + "\n")
	for _, r := range rows {
		warn := ""
		if limit > 0 && r.tokens > limit {
			warn = "  " + WarningStyle.Render(fmt.Sprintf("⚠ exceeds %d", limit))
		}
		b.WriteString(fmt.Sprintf("  %-36s  %s%s\n",
			PromptNameStyle.Render(r.name),
			MutedStyle.Render(fmt.Sprintf("%d tokens", r.tokens)),
			warn))
	}
	return b.String()
}

func renderStats(rp *ast.ResolvedPrompt, limit int) string {
	fields := []fieldStat{
		{"summary", itokens.Estimate(rp.Summary)},
		{"persona", itokens.Estimate(rp.Persona)},
		{"context", itokens.Estimate(rp.Context)},
		{"objective", itokens.Estimate(rp.Objective)},
		{"instructions", itokens.Estimate(strings.Join(rp.Instructions, "\n"))},
		{"constraints", itokens.Estimate(strings.Join(rp.Constraints, "\n"))},
		{"examples", itokens.Estimate(strings.Join(rp.Examples, "\n"))},
		{"format", itokens.Estimate(strings.Join(rp.Format, "\n"))},
		{"notes", itokens.Estimate(rp.Notes)},
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].tokens > fields[j].tokens })

	total := 0
	for _, f := range fields {
		total += f.tokens
	}

	var b strings.Builder
	b.WriteString("\n  " + PromptNameStyle.Render(rp.Name) + " — token estimate\n\n")
	b.WriteString(fmt.Sprintf("  %-18s  %6s   %s\n",
		SubHeaderStyle.Render("Field"),
		SubHeaderStyle.Render("Tokens"),
		SubHeaderStyle.Render("%")))
	b.WriteString("  " + Divider(34) + "\n")
	for _, f := range fields {
		if f.tokens == 0 {
			continue
		}
		pct := 0
		if total > 0 {
			pct = f.tokens * 100 / total
		}
		b.WriteString(fmt.Sprintf("  %-18s  %6d  %3d%%\n",
			CommandStyle.Render(f.name),
			f.tokens,
			pct))
	}
	b.WriteString("  " + Divider(34) + "\n")
	b.WriteString(fmt.Sprintf("  %-18s  %6d\n\n",
		BrightStyle.Render("Total"),
		total))

	if limit > 0 {
		pct := total * 100 / limit
		msg := fmt.Sprintf("  %d tokens (~%d%% of a %d-token context window)\n", total, pct, limit)
		if total > limit {
			b.WriteString("  " + WarningStyle.Render("⚠") + " " + WarningStyle.Render(msg))
		} else {
			b.WriteString("  " + MutedStyle.Render(msg))
		}
	}
	return b.String()
}

func totalTokens(rp *ast.ResolvedPrompt) int {
	t := itokens.Estimate(rp.Summary) +
		itokens.Estimate(rp.Persona) +
		itokens.Estimate(rp.Context) +
		itokens.Estimate(rp.Objective) +
		itokens.Estimate(strings.Join(rp.Instructions, "\n")) +
		itokens.Estimate(strings.Join(rp.Constraints, "\n")) +
		itokens.Estimate(strings.Join(rp.Examples, "\n")) +
		itokens.Estimate(strings.Join(rp.Format, "\n")) +
		itokens.Estimate(rp.Notes)
	return t
}

// ── Pack ──────────────────────────────────────────────────────────────────

// RunPackInit creates a pack.toml scaffold in cwd.
func RunPackInit(cwd string) (string, bool) {
	if err := ipack.Init(cwd); err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return "  " + SuccessStyle.Render("✓") + "  " + PathStyle.Render("pack.toml") +
		MutedStyle.Render(" created") + "\n", false
}

// RunPackBuild bundles the project into a .lpack archive.
func RunPackBuild(cwd string) (string, bool) {
	archivePath, err := ipack.Build(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	rel, _ := filepath.Rel(cwd, archivePath)
	return "  " + SuccessStyle.Render("✓") + "  " + PathStyle.Render(rel) + "\n", false
}

// RunPackInstall unpacks an .lpack archive into the project.
func RunPackInstall(archivePath, cwd string) (string, bool) {
	if !filepath.IsAbs(archivePath) {
		archivePath = filepath.Join(cwd, archivePath)
	}
	if err := ipack.Install(archivePath, cwd); err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return "  " + SuccessStyle.Render("✓") + "  pack installed" + "\n", false
}

// RunPackList lists installed packs.
func RunPackList(cwd string) string {
	packs, err := ipack.List(cwd)
	if err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n"
	}
	var b strings.Builder
	b.WriteString("  " + HeaderStyle.Render(fmt.Sprintf("Installed Packs (%d)", len(packs))) + "\n")
	b.WriteString("  " + Divider(44) + "\n")
	if len(packs) == 0 {
		b.WriteString("  " + MutedStyle.Render("(none)") + "\n")
		return b.String()
	}
	for _, p := range packs {
		b.WriteString(fmt.Sprintf("  %s  %s\n",
			PromptNameStyle.Render(fmt.Sprintf("%-28s", p.Name)),
			MutedStyle.Render("v"+p.Version)))
	}
	return b.String()
}

// RunPackRemove removes an installed pack.
func RunPackRemove(name, cwd string) (string, bool) {
	if err := ipack.Remove(name, cwd); err != nil {
		return ErrorStyle.Render("Error: "+err.Error()) + "\n", true
	}
	return "  " + SuccessStyle.Render("✓") + "  " + PromptNameStyle.Render(name) +
		MutedStyle.Render(" removed") + "\n", false
}
