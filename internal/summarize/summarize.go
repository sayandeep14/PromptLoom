// Package summarize generates a structured summary of files or directories
// using an LLM. The workspace mode builds an architecture-level summary.
package summarize

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sayandeep14/PromptLoom/internal/config"
	loomctx "github.com/sayandeep14/PromptLoom/internal/context"
	"github.com/sayandeep14/PromptLoom/internal/llm"
)

// Result is the output of a summarize run.
type Result struct {
	// Markdown content of the summary.
	Content string
	// Path where the summary was saved (empty if not saved to disk).
	SavedTo string
}

// Options controls summarize behaviour.
type Options struct {
	// Save writes the output to .loom/context/ instead of stdout-only.
	Save bool
	// OutputPath overrides the save location.
	OutputPath string
}

// skipDirs are directory names that are never walked.
var skipDirs = map[string]bool{
	".git":          true,
	".hg":           true,
	"node_modules":  true,
	"vendor":        true,
	".loom":         true,
	"dist":          true,
	"__pycache__":   true,
	".pytest_cache": true,
	"target":        true, // maven/cargo
	"build":         true,
	".gradle":       true,
}

// keyFiles are high-priority files to always include in context.
var keyFileNames = []string{
	"CLAUDE.md", "README.md", "README.rst",
	"go.mod", "package.json", "Cargo.toml",
	"pyproject.toml", "requirements.txt",
	"pom.xml", "build.gradle",
	"Makefile", "Dockerfile", ".github",
	"loom.toml",
}

// SummarizeWorkspace generates an architecture summary for the whole project.
func SummarizeWorkspace(cwd string, cfg *config.Config, opts Options) (*Result, error) {
	tree := buildFileTree(cwd)
	context := buildWorkspaceContext(cwd, tree)
	summary, err := callLLM(workspaceSystemPrompt(), workspaceUserPrompt(context), cfg, 90*time.Second)
	if err != nil {
		return nil, err
	}

	result := &Result{Content: summary}

	outPath := opts.OutputPath
	if outPath == "" && opts.Save {
		outPath = filepath.Join(cwd, ".loom", "context", "architecture-summary.md")
	}
	if outPath != "" {
		if err := writeFile(outPath, summary); err != nil {
			return nil, fmt.Errorf("could not save summary: %w", err)
		}
		result.SavedTo = outPath
	}

	return result, nil
}

// SummarizePaths generates a summary of specific files or directories.
func SummarizePaths(paths []string, cwd string, cfg *config.Config, opts Options) (*Result, error) {
	ctx, err := buildPathContext(paths, cwd)
	if err != nil {
		return nil, err
	}

	summary, err := callLLM(pathSystemPrompt(), pathUserPrompt(ctx), cfg, 60*time.Second)
	if err != nil {
		return nil, err
	}

	result := &Result{Content: summary}

	outPath := opts.OutputPath
	if outPath == "" && opts.Save {
		slug := pathSlug(paths)
		outPath = filepath.Join(cwd, ".loom", "context", slug+"-summary.md")
	}
	if outPath != "" {
		if err := writeFile(outPath, summary); err != nil {
			return nil, fmt.Errorf("could not save summary: %w", err)
		}
		result.SavedTo = outPath
	}

	return result, nil
}

// ---- file tree ----

type fileEntry struct {
	rel   string
	size  int64
	isDir bool
	isKey bool
}

func buildFileTree(root string) []fileEntry {
	keySet := map[string]bool{}
	for _, k := range keyFileNames {
		keySet[strings.ToLower(k)] = true
	}

	var entries []fileEntry
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		// Skip hidden and noise dirs.
		if d.IsDir() {
			base := d.Name()
			if skipDirs[base] || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			entries = append(entries, fileEntry{rel: rel + "/", isDir: true})
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // vanished or unreadable while walking
		}
		isKey := keySet[strings.ToLower(d.Name())] && !loomctx.IsSensitiveName(d.Name())
		entries = append(entries, fileEntry{
			rel:   rel,
			size:  info.Size(),
			isKey: isKey,
		})
		return nil
	})
	return entries
}

func buildWorkspaceContext(root string, tree []fileEntry) string {
	var sb strings.Builder

	// File tree (directories only + key files).
	sb.WriteString("=== Project File Tree (directories + key files) ===\n")
	for _, e := range tree {
		if e.isDir {
			fmt.Fprintf(&sb, "  %s\n", e.rel)
		} else if e.isKey {
			fmt.Fprintf(&sb, "  %s  (%d bytes)\n", e.rel, e.size)
		}
	}

	// File counts by top-level directory.
	dirCounts := map[string]int{}
	for _, e := range tree {
		if e.isDir {
			continue
		}
		parts := strings.SplitN(e.rel, string(os.PathSeparator), 2)
		if len(parts) == 2 {
			dirCounts[parts[0]]++
		}
	}
	if len(dirCounts) > 0 {
		sb.WriteString("\n=== File counts by directory ===\n")
		var dirs []string
		for d := range dirCounts {
			dirs = append(dirs, d)
		}
		sort.Strings(dirs)
		for _, d := range dirs {
			fmt.Fprintf(&sb, "  %-30s %d files\n", d+"/", dirCounts[d])
		}
	}

	// Key file contents.
	sb.WriteString("\n=== Key File Contents ===\n")
	const maxPerFile = 3000
	const maxTotal = 40000
	total := 0
	for _, e := range tree {
		if !e.isKey || e.isDir {
			continue
		}
		if total >= maxTotal {
			sb.WriteString("\n[context limit reached — remaining key files omitted]\n")
			break
		}
		data, err := os.ReadFile(filepath.Join(root, e.rel))
		if err != nil {
			continue
		}
		content := clip(string(data), maxPerFile)
		fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", e.rel, content)
		total += len(content)
	}

	return sb.String()
}

func buildPathContext(paths []string, cwd string) (string, error) {
	var sb strings.Builder
	const maxPerFile = 8000
	const maxTotal = 50000
	total := 0

	for _, p := range paths {
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(cwd, p)
		}

		info, err := os.Stat(abs)
		if err != nil {
			return "", fmt.Errorf("cannot access %q: %w", p, err)
		}

		if info.IsDir() {
			fmt.Fprintf(&sb, "\n=== Directory: %s ===\n", p)
			_ = filepath.WalkDir(abs, func(fpath string, d fs.DirEntry, werr error) error {
				if werr != nil || d.IsDir() {
					if d != nil && d.IsDir() && skipDirs[d.Name()] {
						return filepath.SkipDir
					}
					return nil
				}
				rel, _ := filepath.Rel(cwd, fpath)
				if total >= maxTotal {
					return filepath.SkipAll
				}
				// Credentials never go to the model, and neither do binaries or links out of the tree.
				if loomctx.IsSensitiveName(fpath) || d.Type()&fs.ModeSymlink != 0 {
					return nil
				}
				data, err := os.ReadFile(fpath)
				if err != nil || bytes.IndexByte(data, 0) >= 0 {
					return nil
				}
				content := clip(string(data), maxPerFile)
				fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", rel, content)
				total += len(content)
				return nil
			})
		} else {
			if total >= maxTotal {
				sb.WriteString("\n[context limit reached — remaining files omitted]\n")
				break
			}
			if loomctx.IsSensitiveName(abs) {
				return "", fmt.Errorf("refusing to summarize %q: it looks like a credentials file and would be sent to the model provider", p)
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("cannot read %q: %w", p, err)
			}
			content := clip(string(data), maxPerFile)
			fmt.Fprintf(&sb, "\n=== File: %s ===\n%s\n", p, content)
			total += len(content)
		}
	}

	return sb.String(), nil
}

// clip cuts s to at most max bytes without splitting a UTF-8 character.
func clip(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n... [truncated]"
}

// ---- prompts ----

func workspaceSystemPrompt() string {
	return `You are an expert software architect. Analyse the given project context and produce a concise, well-structured architecture summary in Markdown.

The summary will be saved as .loom/context/architecture-summary.md and attached to AI prompts to give assistants project context.

Write clearly, factually, and briefly. Do not pad with generic advice. If you are uncertain, say so.`
}

func workspaceUserPrompt(ctx string) string {
	return ctx + `

Generate an architecture summary with these sections (only include sections relevant to the project):

## Overview
One to two sentences describing what this project is and what it does.

## Tech Stack
Language, framework, build tool, test framework, notable libraries.

## Key Directories
What each top-level directory contains.

## Entry Points
Main files, package entry points, or key starting files.

## Testing
How tests are organised and run.

## Configuration
Key configuration files and what they control.

## Notable Patterns
Any interesting architectural patterns, conventions, or constraints worth noting.

---
*Generated by loom summarize workspace*`
}

func pathSystemPrompt() string {
	return `You are an expert software engineer. Analyse the given files or directories and produce a concise, structured summary in Markdown.

Be factual, specific, and brief. Focus on: what the code does, key types/functions/patterns, important dependencies, and anything noteworthy for a developer working in this area.`
}

func pathUserPrompt(ctx string) string {
	return ctx + `

Generate a structured summary covering:
- **Purpose** — what this code does
- **Key components** — important types, functions, or classes
- **Dependencies** — what it relies on internally and externally
- **Patterns** — any notable architectural patterns or conventions
- **Gotchas** — anything surprising or important to know

Keep it concise but specific.`
}

// ---- LLM call ----

// callLLM sends one prompt to the model configured in [testing] of loom.toml, through the shared
// client (internal/llm), which owns provider selection, the API key and its safe handling.
func callLLM(sysPrompt, userMsg string, cfg *config.Config, timeout time.Duration) (string, error) {
	client, err := llm.FromConfig(cfg)
	if err != nil {
		return "", err
	}
	client.Timeout = timeout
	return client.Complete(context.Background(), llm.Request{System: sysPrompt, User: userMsg})
}

// ---- helpers ----

func writeFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func pathSlug(paths []string) string {
	if len(paths) == 0 {
		return "summary"
	}
	base := filepath.Base(paths[0])
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, base)
	base = strings.Trim(strings.ToLower(base), "-")
	if base == "" {
		return "summary"
	}
	return base
}
