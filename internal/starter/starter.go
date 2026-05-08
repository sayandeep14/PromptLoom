// Package starter generates a PromptLoom starter library from workspace context.
package starter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/config"
)

// Tier controls how much LLM token budget to spend.
type Tier int

const (
	TierMinimal Tier = iota // 3-5 prompts, minimal blocks
	TierDefault             // 8-12 prompts + 2-4 blocks
	TierBest                // 15-20 prompts + 5-8 blocks, maximum quality
)

// File is one .loom file to be generated.
type File struct {
	Name        string // e.g., "CodeReviewer.prompt.loom"
	Type        string // "prompt" or "block"
	Description string
	Content     string
}

// Plan is the set of files to write.
type Plan struct {
	Files []File
}

// FormatPlan serialises the plan into the human-editable text format used when
// the user chooses to edit the plan before generation.
//
// Format:
//
//	## FileName.prompt.loom | Description text
//	<file content>
//	<blank line between files>
func FormatPlan(p *Plan) string {
	var sb strings.Builder
	sb.WriteString("# PromptLoom Starter Pack Plan\n")
	sb.WriteString("# Edit files below. Each section starts with:\n")
	sb.WriteString("#   ## Filename.type.loom | Short description\n")
	sb.WriteString("# followed by the file content. Lines starting with # are comments.\n\n")

	for i, f := range p.Files {
		if i > 0 {
			sb.WriteString("\n")
		}
		fmt.Fprintf(&sb, "## %s | %s\n", f.Name, f.Description)
		sb.WriteString(f.Content)
		if !strings.HasSuffix(f.Content, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ParsePlan parses the human-editable text format back into a Plan.
// Lines starting with # are ignored. Sections are delimited by ## headers.
func ParsePlan(text string) (*Plan, error) {
	lines := strings.Split(text, "\n")
	plan := &Plan{}
	var current *File
	var contentLines []string

	flush := func() {
		if current == nil {
			return
		}
		current.Content = strings.Join(contentLines, "\n")
		// trim leading blank lines
		current.Content = strings.TrimPrefix(current.Content, "\n")
		if current.Content != "" {
			plan.Files = append(plan.Files, *current)
		}
		current = nil
		contentLines = nil
	}

	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			flush()
			rest := strings.TrimPrefix(line, "## ")
			parts := strings.SplitN(rest, " | ", 2)
			name := strings.TrimSpace(parts[0])
			desc := ""
			if len(parts) == 2 {
				desc = strings.TrimSpace(parts[1])
			}
			typ := "prompt"
			if strings.Contains(name, ".block.") {
				typ = "block"
			} else if strings.Contains(name, ".overlay.") {
				typ = "overlay"
			}
			current = &File{Name: name, Type: typ, Description: desc}
			contentLines = nil
			continue
		}
		if current != nil {
			contentLines = append(contentLines, line)
		}
	}
	flush()

	if len(plan.Files) == 0 {
		return nil, fmt.Errorf("plan is empty — no ## sections found")
	}
	return plan, nil
}

// Write writes all files in the plan to the configured directories.
// All existing files are overwritten.
func Write(plan *Plan, cfg *config.Config, cwd string) ([]string, error) {
	var written []string
	for _, f := range plan.Files {
		dir := filepath.Join(cwd, cfg.Paths.Prompts)
		if f.Type == "block" {
			dir = filepath.Join(cwd, cfg.Paths.Blocks)
		} else if f.Type == "overlay" {
			dir = filepath.Join(cwd, cfg.Paths.Overlays)
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			return written, fmt.Errorf("mkdir %s: %w", dir, err)
		}
		dest := filepath.Join(dir, f.Name)
		if err := os.WriteFile(dest, []byte(f.Content), 0644); err != nil {
			return written, fmt.Errorf("write %s: %w", dest, err)
		}
		written = append(written, dest)
	}
	return written, nil
}
