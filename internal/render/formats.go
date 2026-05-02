package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
)

// Formatter renders a resolved prompt into one deployable artifact format.
type Formatter struct {
	ID              string
	DefaultFileName func(promptName string) string
	Render          func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error)
}

// FormatRegistry holds all supported render targets keyed by format id.
type FormatRegistry struct {
	formats map[string]Formatter
}

func NewFormatRegistry() *FormatRegistry {
	reg := &FormatRegistry{formats: map[string]Formatter{}}
	reg.register(Formatter{
		ID:              "markdown",
		DefaultFileName: func(promptName string) string { return promptName + ".md" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			return Render(rp, cfg), nil
		},
	})
	reg.register(Formatter{
		ID:              "plain",
		DefaultFileName: func(promptName string) string { return promptName + ".txt" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			return renderPlain(rp), nil
		},
	})
	reg.register(Formatter{
		ID:              "json-anthropic",
		DefaultFileName: func(promptName string) string { return promptName + ".json" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			body, err := json.MarshalIndent(map[string]string{
				"system": Render(rp, cfg),
			}, "", "  ")
			if err != nil {
				return "", err
			}
			return string(body) + "\n", nil
		},
	})
	reg.register(Formatter{
		ID:              "json-openai",
		DefaultFileName: func(promptName string) string { return promptName + ".json" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			body, err := json.MarshalIndent([]map[string]string{{
				"role":    "system",
				"content": Render(rp, cfg),
			}}, "", "  ")
			if err != nil {
				return "", err
			}
			return string(body) + "\n", nil
		},
	})
	reg.register(Formatter{
		ID:              "cursor-rule",
		DefaultFileName: func(promptName string) string { return promptName + ".mdc" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			return renderCursorRule(rp, cfg), nil
		},
	})
	reg.register(Formatter{
		ID:              "copilot",
		DefaultFileName: func(promptName string) string { return promptName + ".md" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			return Render(rp, cfg), nil
		},
	})
	reg.register(Formatter{
		ID:              "claude-code",
		DefaultFileName: func(promptName string) string { return promptName + ".CLAUDE.md" },
		Render: func(rp *ast.ResolvedPrompt, cfg *config.Config) (string, error) {
			return Render(rp, cfg), nil
		},
	})
	return reg
}

func (r *FormatRegistry) register(format Formatter) {
	r.formats[format.ID] = format
}

func (r *FormatRegistry) Lookup(id string) (Formatter, bool) {
	format, ok := r.formats[id]
	return format, ok
}

var defaultRegistry = NewFormatRegistry()

func DefaultRegistry() *FormatRegistry {
	return defaultRegistry
}

// ResolveFormat returns the formatter requested by id, or cfg.Render.DefaultFormat when blank.
func ResolveFormat(id string, cfg *config.Config) (Formatter, error) {
	if id == "" {
		id = cfg.Render.DefaultFormat
	}
	if id == "" {
		id = "markdown"
	}
	format, ok := DefaultRegistry().Lookup(id)
	if !ok {
		return Formatter{}, fmt.Errorf("unknown format %q", id)
	}
	return format, nil
}

// RenderFormat resolves and renders a prompt in the requested format.
func RenderFormat(rp *ast.ResolvedPrompt, cfg *config.Config, id string) (string, Formatter, error) {
	format, err := ResolveFormat(id, cfg)
	if err != nil {
		return "", Formatter{}, err
	}
	body, err := format.Render(rp, cfg)
	if err != nil {
		return "", Formatter{}, err
	}
	return body, format, nil
}

// renderCursorRule produces a Cursor .mdc file with proper front-matter.
// See https://docs.cursor.com/context/rules-for-ai for the expected format.
func renderCursorRule(rp *ast.ResolvedPrompt, cfg *config.Config) string {
	var sb strings.Builder
	desc := rp.Summary
	if desc == "" {
		desc = rp.Name
	}
	// Cursor MDC front-matter: description, globs, alwaysApply.
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "description: %s\n", desc)
	sb.WriteString("globs: []\n")
	sb.WriteString("alwaysApply: false\n")
	sb.WriteString("---\n\n")

	// Render the prompt body without the # title heading (Cursor rules are
	// referenced by globs, not by heading) but keep all sections.
	noMetaCfg := *cfg
	noMetaCfg.Render.IncludeMetadata = false
	noMetaCfg.Render.IncludeFingerprint = false

	// Write sections directly (skip the # Name heading).
	for _, sec := range fieldOrder {
		if sec.isList {
			items := getList(rp, sec.fieldName)
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "## %s\n", sec.heading)
			for _, item := range items {
				fmt.Fprintf(&sb, "- %s\n", item)
			}
			sb.WriteByte('\n')
		} else {
			val := getScalar(rp, sec.fieldName)
			if val == "" {
				continue
			}
			fmt.Fprintf(&sb, "## %s\n", sec.heading)
			fmt.Fprintf(&sb, "%s\n\n", val)
		}
	}
	return strings.TrimRight(sb.String(), "\n") + "\n"
}

func renderPlain(rp *ast.ResolvedPrompt) string {
	var sb strings.Builder
	sb.WriteString(rp.Name)
	sb.WriteString("\n")

	for _, sec := range fieldOrder {
		if sec.isList {
			items := getList(rp, sec.fieldName)
			if len(items) == 0 {
				continue
			}
			fmt.Fprintf(&sb, "\n%s:\n", sec.heading)
			for _, item := range items {
				fmt.Fprintf(&sb, "- %s\n", item)
			}
			continue
		}

		val := getScalar(rp, sec.fieldName)
		if val == "" {
			continue
		}
		fmt.Fprintf(&sb, "\n%s:\n", sec.heading)
		sb.WriteString(val)
		sb.WriteString("\n")
	}

	return sb.String()
}
