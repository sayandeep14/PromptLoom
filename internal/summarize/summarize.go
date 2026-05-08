// Package summarize generates a structured summary of files or directories
// using an LLM. The workspace mode builds an architecture-level summary.
package summarize

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/config"
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
	".git":         true,
	".hg":          true,
	"node_modules": true,
	"vendor":       true,
	".loom":        true,
	"dist":         true,
	"__pycache__":  true,
	".pytest_cache": true,
	"target":       true, // maven/cargo
	"build":        true,
	".gradle":      true,
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
	rel      string
	size     int64
	isDir    bool
	isKey    bool
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
		info, _ := d.Info()
		isKey := keySet[strings.ToLower(d.Name())]
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
		content := string(data)
		if len(content) > maxPerFile {
			content = content[:maxPerFile] + "\n... [truncated]"
		}
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
				data, err := os.ReadFile(fpath)
				if err != nil {
					return nil
				}
				content := string(data)
				if len(content) > maxPerFile {
					content = content[:maxPerFile] + "\n... [truncated]"
				}
				fmt.Fprintf(&sb, "\n--- %s ---\n%s\n", rel, content)
				total += len(content)
				return nil
			})
		} else {
			if total >= maxTotal {
				sb.WriteString("\n[context limit reached — remaining files omitted]\n")
				break
			}
			data, err := os.ReadFile(abs)
			if err != nil {
				return "", fmt.Errorf("cannot read %q: %w", p, err)
			}
			content := string(data)
			if len(content) > maxPerFile {
				content = content[:maxPerFile] + "\n... [truncated]"
			}
			fmt.Fprintf(&sb, "\n=== File: %s ===\n%s\n", p, content)
			total += len(content)
		}
	}

	return sb.String(), nil
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

func callLLM(sysPrompt, userMsg string, cfg *config.Config, timeout time.Duration) (string, error) {
	provider := cfg.Testing.Provider
	if provider == "" {
		provider = "gemini"
	}
	model := cfg.Testing.DefaultModel
	if model == "" {
		model = "gemini-2.5-flash"
	}
	envVar := cfg.Testing.APIKeyEnv
	if envVar == "" {
		if strings.ToLower(provider) == "anthropic" {
			envVar = "ANTHROPIC_API_KEY"
		} else {
			envVar = "GEMINI_API_KEY"
		}
	}
	apiKey := os.Getenv(envVar)
	if apiKey == "" {
		return "", fmt.Errorf("API key not set: $%s is empty\nAdd it to .loom.secret or export it in your shell", envVar)
	}

	switch strings.ToLower(provider) {
	case "anthropic":
		return callAnthropic(model, apiKey, sysPrompt, userMsg, timeout)
	default:
		return callGemini(model, apiKey, sysPrompt, userMsg, timeout)
	}
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
	return strings.ToLower(base)
}

// ---- Gemini client ----

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}
type geminiContent struct {
	Parts []geminiPart `json:"parts"`
	Role  string       `json:"role,omitempty"`
}
type geminiPart struct {
	Text string `json:"text"`
}
type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

func callGemini(model, apiKey, sysPrompt, userIn string, timeout time.Duration) (string, error) {
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, apiKey,
	)
	req := geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: sysPrompt}}},
		Contents: []geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: userIn}}},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}
	if gr.Error != nil {
		return "", fmt.Errorf("Gemini API error %d: %s", gr.Error.Code, gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("Gemini returned no content")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// ---- Anthropic client ----

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func callAnthropic(model, apiKey, sysPrompt, userIn string, timeout time.Duration) (string, error) {
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	req := anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    sysPrompt,
		Messages:  []anthropicMessage{{Role: "user", Content: userIn}},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var ar anthropicResponse
	if err := json.Unmarshal(respBody, &ar); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}
	if ar.Error != nil {
		return "", fmt.Errorf("Anthropic API error (%s): %s", ar.Error.Type, ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return "", fmt.Errorf("Anthropic returned no content")
	}
	return ar.Content[0].Text, nil
}
