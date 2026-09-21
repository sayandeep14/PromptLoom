// Package importer converts well-structured Markdown prompt files into PromptLoom DSL.
package importer

import (
	"bufio"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
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
	// PascalCase: capitalise after spaces, dashes, underscores; drop anything that is not
	// valid in a prompt name (dots, brackets, ...).
	return sanitizeName(toPascal(base))
}

func parseSections(src string) []section {
	var sections []section
	var current *section

	scanner := bufio.NewScanner(strings.NewReader(src))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	inFence := false
	for scanner.Scan() {
		line := scanner.Text()

		// A "## x" inside a fenced code block is example text, not a section heading.
		if strings.HasPrefix(strings.TrimSpace(line), "```") || strings.HasPrefix(strings.TrimSpace(line), "~~~") {
			inFence = !inFence
		}
		if inFence {
			if current != nil {
				current.lines = append(current.lines, line)
			}
			continue
		}

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

	name = sanitizeName(name)
	fmt.Fprintf(&b, "prompt %s {\n", name)

	// A field can only be written once, so sections that map to the same field (two
	// "## Instructions", or several unrecognised headings that all go to notes) are merged
	// in order instead of overwriting one another.
	type merged struct {
		field string
		parts [][]string
	}
	var order []string
	byField := map[string]*merged{}

	for _, sec := range sections {
		body := trimBlankEdges(sec.lines)
		if len(body) == 0 {
			continue
		}
		field := sec.field
		if field == "" {
			warnings = append(warnings, fmt.Sprintf("unrecognised section %q placed in notes", sec.heading))
			field = "notes"
			// keep the heading so the merged notes still say what each part was
			body = append([]string{sec.heading + ":"}, body...)
		}
		m := byField[field]
		if m == nil {
			m = &merged{field: field}
			byField[field] = m
			order = append(order, field)
		}
		m.parts = append(m.parts, body)
	}

	for _, field := range order {
		m := byField[field]
		fmt.Fprintf(&b, "  %s :=\n", field)
		if listFields[field] {
			for _, part := range m.parts {
				for _, line := range part {
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
			}
		} else {
			// A field ends at the first blank line, so paragraphs cannot be separated by one:
			// blank lines inside a text field are dropped (the paragraphs stay on separate lines).
			dropped := false
			for _, part := range m.parts {
				for _, line := range part {
					if strings.TrimSpace(line) == "" {
						dropped = true
						continue
					}
					fmt.Fprintf(&b, "    %s\n", line)
				}
			}
			if dropped {
				warnings = append(warnings, fmt.Sprintf("blank lines inside %q were removed (a field cannot contain blank lines)", field))
			}
		}
		b.WriteByte('\n')
	}

	b.WriteString("}\n")

	return Result{Name: name, DSL: b.String(), Warnings: warnings}
}

// sanitizeName turns any string into a valid prompt identifier.
func sanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "ImportedPrompt"
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "P" + out
	}
	return out
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
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			upper = true // any separator (space, dash, dot, bracket, ...) starts a new word
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
