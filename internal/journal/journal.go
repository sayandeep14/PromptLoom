// Package journal provides a lightweight change journal for prompt libraries.
// Entries are stored in `.loom/journal/YYYY-MM-DD_<slug>.md` files and
// can be tagged with a prompt name, author, and free-form markdown body.
package journal

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const journalDir = ".loom/journal"

// Entry is a single journal record.
type Entry struct {
	Date    time.Time
	Slug    string
	Prompt  string // optional: which prompt this entry is about
	Author  string
	Message string // first line of the file body (title)
	Body    string // full markdown body
	File    string // absolute path
}

// Add writes a new journal entry file. Author, prompt, and body are optional.
func Add(cwd, message, prompt, author, body string) (string, error) {
	dir := filepath.Join(cwd, journalDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create journal dir: %w", err)
	}

	now := time.Now()
	slug := makeSlug(message)
	filename := fmt.Sprintf("%s_%s.md", now.Format("2006-01-02"), slug)
	path := filepath.Join(dir, filename)

	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("date: %s\n", now.Format(time.RFC3339)))
	if prompt != "" {
		b.WriteString(fmt.Sprintf("prompt: %s\n", prompt))
	}
	if author != "" {
		b.WriteString(fmt.Sprintf("author: %s\n", author))
	}
	b.WriteString("---\n\n")
	b.WriteString("# " + message + "\n\n")
	if body != "" {
		b.WriteString(body + "\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", fmt.Errorf("write journal entry: %w", err)
	}
	return path, nil
}

// List returns all journal entries from the journal directory, newest first.
func List(cwd string) ([]Entry, error) {
	dir := filepath.Join(cwd, journalDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read journal dir: %w", err)
	}

	var out []Entry
	for _, de := range entries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		entry := parseEntry(string(data), path)
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Date.After(out[j].Date)
	})
	return out, nil
}

// ForPrompt returns entries whose Prompt field matches the given name.
func ForPrompt(cwd, name string) ([]Entry, error) {
	all, err := List(cwd)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if strings.EqualFold(e.Prompt, name) {
			out = append(out, e)
		}
	}
	return out, nil
}

// ---- helpers ----

func parseEntry(content, path string) Entry {
	e := Entry{File: path}

	// Parse front matter between the first two "---" lines.
	content = strings.TrimPrefix(content, "---\n")
	idx := strings.Index(content, "\n---\n")
	var fm, body string
	if idx != -1 {
		fm = content[:idx]
		body = strings.TrimPrefix(content[idx:], "\n---\n")
	} else {
		body = content
	}

	for _, line := range strings.Split(fm, "\n") {
		if k, v, ok := strings.Cut(line, ": "); ok {
			switch k {
			case "date":
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					e.Date = t
				}
			case "prompt":
				e.Prompt = v
			case "author":
				e.Author = v
			}
		}
	}

	e.Body = strings.TrimSpace(body)
	// Extract first heading as message.
	for _, line := range strings.Split(e.Body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			e.Message = strings.TrimPrefix(line, "# ")
			break
		}
	}
	if e.Message == "" {
		// Fall back to filename slug.
		base := filepath.Base(path)
		base = strings.TrimSuffix(base, ".md")
		if len(base) > 10 {
			base = base[11:] // strip "YYYY-MM-DD_"
		}
		e.Message = strings.ReplaceAll(base, "-", " ")
	}

	// Derive slug from filename.
	base := filepath.Base(path)
	if len(base) > 11 {
		e.Slug = strings.TrimSuffix(base[11:], ".md")
	}
	return e
}

func makeSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		case !prevDash:
			b.WriteByte('-')
			prevDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		slug = "entry"
	}
	return slug
}
