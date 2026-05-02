// Package context resolves --with context sources and --context bundle files.
package context

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Source is a resolved piece of context with a display label and raw content.
type Source struct {
	Label   string
	Content string
}

// Resolve resolves a single source spec (e.g. "file:src/main.go", "git:diff", "stdin")
// relative to cwd. It returns the display label and the raw content.
func Resolve(spec, cwd string) (Source, error) {
	switch {
	case spec == "stdin":
		return resolveStdin()
	case spec == "git:diff":
		return resolveGitCmd(cwd, "diff", "git diff")
	case spec == "git:staged":
		return resolveGitCmd(cwd, "staged", "git diff --staged")
	case strings.HasPrefix(spec, "file:") || strings.HasPrefix(spec, "text:"):
		var path string
		if strings.HasPrefix(spec, "file:") {
			path = strings.TrimPrefix(spec, "file:")
		} else {
			path = strings.TrimPrefix(spec, "text:")
		}
		return resolveFile(path, cwd)
	case strings.HasPrefix(spec, "dir:"):
		path := strings.TrimPrefix(spec, "dir:")
		return resolveDir(path, cwd)
	default:
		return Source{}, fmt.Errorf("unknown context source %q — use file:, dir:, git:diff, git:staged, or stdin", spec)
	}
}

func resolveStdin() (Source, error) {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return Source{}, fmt.Errorf("cannot stat stdin: %w", err)
	}
	if (fi.Mode() & os.ModeCharDevice) != 0 {
		return Source{}, fmt.Errorf("stdin source requested but no data piped")
	}
	var sb strings.Builder
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		sb.WriteString(sc.Text())
		sb.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return Source{}, fmt.Errorf("reading stdin: %w", err)
	}
	return Source{Label: "stdin", Content: sb.String()}, nil
}

func resolveGitCmd(cwd, label, cmdLine string) (Source, error) {
	parts := strings.Fields(cmdLine)
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return Source{}, fmt.Errorf("running %q: %w", cmdLine, err)
	}
	return Source{Label: label, Content: string(out)}, nil
}

func resolveFile(path, cwd string) (Source, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Source{}, fmt.Errorf("reading file %q: %w", path, err)
	}
	return Source{Label: "File: " + path, Content: string(data)}, nil
}

func resolveDir(path, cwd string) (Source, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Source{}, fmt.Errorf("reading dir %q: %w", path, err)
	}
	var sb strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		fpath := filepath.Join(path, e.Name())
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}
		fmt.Fprintf(&sb, "### %s\n%s\n", e.Name(), string(data))
	}
	return Source{Label: "Dir: " + path, Content: sb.String()}, nil
}

// AppendContextSection appends an "## Attached Context" block to body if
// there are any resolved sources.
func AppendContextSection(body string, sources []Source) string {
	if len(sources) == 0 {
		return body
	}
	var sb strings.Builder
	sb.WriteString(body)
	sb.WriteString("\n## Attached Context\n")
	for _, s := range sources {
		sb.WriteString("\n### ")
		sb.WriteString(s.Label)
		sb.WriteString("\n")
		// Detect language from label for fenced code blocks.
		lang := guessLang(s.Label)
		if lang != "" {
			sb.WriteString("```" + lang + "\n")
			sb.WriteString(s.Content)
			if len(s.Content) > 0 && s.Content[len(s.Content)-1] != '\n' {
				sb.WriteByte('\n')
			}
			sb.WriteString("```\n")
		} else {
			sb.WriteString(s.Content)
			if len(s.Content) > 0 && s.Content[len(s.Content)-1] != '\n' {
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

func guessLang(label string) string {
	ext := strings.ToLower(filepath.Ext(label))
	langs := map[string]string{
		".go":   "go",
		".java": "java",
		".ts":   "typescript",
		".tsx":  "tsx",
		".js":   "javascript",
		".py":   "python",
		".rb":   "ruby",
		".rs":   "rust",
		".sh":   "bash",
		".yaml": "yaml",
		".yml":  "yaml",
		".json": "json",
		".toml": "toml",
		".md":   "markdown",
		".xml":  "xml",
		".sql":  "sql",
	}
	if l, ok := langs[ext]; ok {
		return l
	}
	// For git diff / staged output, use diff lang.
	if strings.Contains(strings.ToLower(label), "diff") || strings.Contains(strings.ToLower(label), "staged") {
		return "diff"
	}
	return ""
}

// EstimateTokens returns a rough token count (characters / 4).
func EstimateTokens(s string) int {
	return len(s) / 4
}
