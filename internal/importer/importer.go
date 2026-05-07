// Package importer converts well-structured Markdown prompt files into PromptLoom DSL.
package importer

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
)

// Result holds the converted DSL source and any import warnings.
type Result struct {
	Name     string
	DSL      string
	Warnings []string
}

// knownFields maps normalised heading names to DSL field names.
var knownFields = map[string]string{
	"persona":       "persona",
	"summary":       "summary",
	"context":       "context",
	"objective":     "objective",
	"notes":         "notes",
	"instructions":  "instructions",
	"constraints":   "constraints",
	"examples":      "examples",
	"format":        "format",
	"output format": "format",
	"output":        "format",
}

// listFields are fields whose values should be rendered as bullet lists.
var listFields = map[string]bool{
	"instructions": true,
	"constraints":  true,
	"examples":     true,
	"format":       true,
}

type section struct {
	field   string // resolved DSL field name, or "" for unknown
	heading string // original heading text
	lines   []string
}

// Import parses a Markdown string and returns a PromptLoom DSL source.
// name is the prompt name to use (derived from filename or --name flag).
func Import(src, name string) Result {
	sections := parseSections(src)
	return renderDSL(name, sections)
}

// NameFromPath derives a prompt name from a file path.
func NameFromPath(path string) string {
	base := filepath.Base(path)
	// Strip common extensions.
	for _, ext := range []string{".md", ".markdown", ".txt"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
		}
	}
	// PascalCase: capitalise after spaces, dashes, underscores.
	return toPascal(base)
}

func parseSections(src string) []section {
	var sections []section
	var current *section

	scanner := bufio.NewScanner(strings.NewReader(src))
	for scanner.Scan() {
		line := scanner.Text()

		// H1 or H2 heading?
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(line[3:])
			field := resolveField(heading)
			sections = append(sections, section{field: field, heading: heading})
			current = &sections[len(sections)-1]
			continue
		}
		if strings.HasPrefix(line, "# ") {
			// H1 is the prompt title — skip it (name comes from flag/filename).
			current = nil
			continue
		}

		if current == nil {
			continue
		}
		current.lines = append(current.lines, line)
	}
	return sections
}

func resolveField(heading string) string {
	return knownFields[strings.ToLower(strings.TrimSpace(heading))]
}

func renderDSL(name string, sections []section) Result {
	var b strings.Builder
	var warnings []string

	fmt.Fprintf(&b, "prompt %s {\n", name)

	for _, sec := range sections {
		body := trimBlankEdges(sec.lines)
		if len(body) == 0 {
			continue
		}

		field := sec.field
		if field == "" {
			// Unknown heading — dump into notes with a comment.
			warnings = append(warnings, fmt.Sprintf("unrecognised section %q placed in notes", sec.heading))
			field = "notes"
		}

		fmt.Fprintf(&b, "  %s:\n", field)
		if listFields[field] {
			for _, line := range body {
				// Normalise bullet prefixes.
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "- ") && !strings.HasPrefix(line, "* ") {
					line = "- " + line
				} else if strings.HasPrefix(line, "* ") {
					line = "- " + line[2:]
				}
				fmt.Fprintf(&b, "    %s\n", line)
			}
		} else {
			for _, line := range body {
				fmt.Fprintf(&b, "    %s\n", line)
			}
		}
		b.WriteByte('\n')
	}

	b.WriteString("}\n")

	return Result{Name: name, DSL: b.String(), Warnings: warnings}
}

func trimBlankEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	return lines[start:end]
}

func toPascal(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		switch {
		case r == ' ' || r == '-' || r == '_':
			upper = true
		case upper:
			if r >= 'a' && r <= 'z' {
				b.WriteRune(r - 32)
			} else {
				b.WriteRune(r)
			}
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
