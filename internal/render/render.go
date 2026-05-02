// Package render converts a ResolvedPrompt into a Markdown string.
package render

import (
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
)

// section describes one field's Markdown heading and how to retrieve its value.
type section struct {
	fieldName string
	heading   string
	isList    bool
}

// fieldOrder defines the canonical output order and headings.
var fieldOrder = []section{
	{fieldName: "summary", heading: "Summary", isList: false},
	{fieldName: "persona", heading: "Persona", isList: false},
	{fieldName: "context", heading: "Context", isList: false},
	{fieldName: "objective", heading: "Objective", isList: false},
	{fieldName: "instructions", heading: "Instructions", isList: true},
	{fieldName: "constraints", heading: "Constraints", isList: true},
	{fieldName: "examples", heading: "Examples", isList: true},
	{fieldName: "format", heading: "Output Format", isList: true}, // "format" → "Output Format"
	{fieldName: "notes", heading: "Notes", isList: false},
}

// Render converts a ResolvedPrompt to a Markdown string using the supplied config.
func Render(rp *ast.ResolvedPrompt, cfg *config.Config) string {
	var sb strings.Builder

	if cfg.Render.IncludeMetadata {
		writeFrontMatter(&sb, rp, cfg)
	}

	fmt.Fprintf(&sb, "# %s\n", rp.Name)

	for _, sec := range fieldOrder {
		if sec.isList {
			items := getList(rp, sec.fieldName)
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "\n## %s\n", sec.heading)
			for _, item := range items {
				fmt.Fprintf(&sb, "- %s\n", item)
			}
		} else {
			val := getScalar(rp, sec.fieldName)
			if val == "" {
				continue
			}
			fmt.Fprintf(&sb, "\n## %s\n", sec.heading)
			fmt.Fprintf(&sb, "%s\n", val)
		}
	}

	return sb.String()
}

// writeFrontMatter writes a YAML front matter block with prompt metadata.
func writeFrontMatter(sb *strings.Builder, rp *ast.ResolvedPrompt, cfg *config.Config) {
	sb.WriteString("---\n")
	fmt.Fprintf(sb, "name: %s\n", rp.Name)

	// Ancestors (chain minus self).
	ancestors := rp.InheritsChain
	if len(ancestors) > 1 {
		sb.WriteString("inherits:\n")
		for _, a := range ancestors[:len(ancestors)-1] {
			fmt.Fprintf(sb, "  - %s\n", a)
		}
	}

	if len(rp.UsedBlocks) > 0 {
		sb.WriteString("blocks:\n")
		for _, b := range rp.UsedBlocks {
			fmt.Fprintf(sb, "  - %s\n", b)
		}
	}

	if rp.AppliedVariant != "" {
		fmt.Fprintf(sb, "variant: %s\n", rp.AppliedVariant)
	}

	if len(rp.AppliedOverlays) > 0 {
		sb.WriteString("overlays:\n")
		for _, name := range rp.AppliedOverlays {
			fmt.Fprintf(sb, "  - %s\n", name)
		}
	}

	if cfg.Render.IncludeFingerprint && rp.Fingerprint != "" && rp.Fingerprint != "sha256:" {
		fmt.Fprintf(sb, "fingerprint: %s\n", rp.Fingerprint)
	}

	sb.WriteString("---\n\n")
}

// ---- field accessors ----

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
	}
	return ""
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
	}
	return nil
}
