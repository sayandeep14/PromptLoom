package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bundle is a parsed context bundle definition from a .context file.
type Bundle struct {
	Name    string
	Include []string
	Exclude []string
}

// LoadBundle reads a named bundle from contexts/<name>.context inside cwd.
func LoadBundle(name, cwd string) (*Bundle, error) {
	path := filepath.Join(cwd, "contexts", name+".context")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("context bundle %q not found (looked at %s)", name, path)
	}
	return parseBundle(name, string(data))
}

// ResolveBundle expands a Bundle into concrete file Sources.
func ResolveBundle(b *Bundle, cwd string) ([]Source, error) {
	var sources []Source
	for _, pattern := range b.Include {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(cwd, pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("bundle glob %q: %w", pattern, err)
		}
		for _, m := range matches {
			if isExcluded(m, b.Exclude, cwd) {
				continue
			}
			fi, err := os.Stat(m)
			if err != nil || fi.IsDir() {
				continue
			}
			data, err := os.ReadFile(m)
			if err != nil {
				continue
			}
			rel, _ := filepath.Rel(cwd, m)
			sources = append(sources, Source{Label: "File: " + rel, Content: string(data)})
		}
	}
	return sources, nil
}

func isExcluded(path string, excludes []string, cwd string) bool {
	rel, err := filepath.Rel(cwd, path)
	if err != nil {
		return false
	}
	for _, pattern := range excludes {
		matched, err := filepath.Match(pattern, rel)
		if err == nil && matched {
			return true
		}
		// Also check if any path component matches.
		matched, err = filepath.Match(pattern, filepath.Base(rel))
		if err == nil && matched {
			return true
		}
	}
	return false
}

// parseBundle is a minimal parser for the .context DSL:
//
//	context BundleName {
//	  include:
//	    - file1
//	  exclude:
//	    - file2
//	}
func parseBundle(name, src string) (*Bundle, error) {
	b := &Bundle{Name: name}
	lines := strings.Split(src, "\n")
	var section string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "context ") {
			continue // header line
		}
		if line == "}" {
			section = ""
			continue
		}
		if line == "include:" {
			section = "include"
			continue
		}
		if line == "exclude:" {
			section = "exclude"
			continue
		}
		if strings.HasPrefix(line, "- ") {
			item := strings.TrimPrefix(line, "- ")
			switch section {
			case "include":
				b.Include = append(b.Include, item)
			case "exclude":
				b.Exclude = append(b.Exclude, item)
			}
		}
	}
	return b, nil
}
