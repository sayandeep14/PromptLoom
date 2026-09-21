// Package export parses .export.loom files, which declare which prompts
// from the loom/src/ tree should be exported (compiled and distributed).
package export

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const Filename = ".export.loom"

// Rule is one export declaration from .export.loom.
type Rule struct {
	// Package is the dot-separated path, e.g. "package2.subpackage1".
	// Maps to a directory path by replacing '.' with filepath.Separator.
	Package string

	// MatchGlob, if set, restricts to files matching this glob (e.g. "*-final.prompt.loom").
	MatchGlob string

	// ExceptGlob, if set, excludes files matching this glob.
	ExceptGlob string

	// ExceptFile, if set, excludes this specific filename.
	ExceptFile string

	// Line is the source line number for error messages.
	Line int
}

// ParseFile reads an .export.loom file and returns all export rules.
// Lines starting with '#' or blank lines are ignored.
//
// Supported syntax (backtick-delimited tokens):
//
//	export `pkg`
//	export `pkg` match `glob`
//	export `pkg` except match `glob`
//	export `pkg` match `glob` except `specific-file.loom`
func ParseFile(path string) ([]Rule, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rules []Rule
	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule, err := parseLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filepath.Base(path), lineNum, err)
		}
		rules = append(rules, rule)
	}
	return rules, sc.Err()
}

// backtick captures a backtick-delimited token, e.g. `foo.bar`
var backtickRE = regexp.MustCompile("`([^`]+)`")

func parseLine(line string, lineNum int) (Rule, error) {
	if !strings.HasPrefix(line, "export") {
		return Rule{}, fmt.Errorf("expected 'export', got %q", line)
	}
	rest := strings.TrimSpace(line[len("export"):])

	tokens := backtickRE.FindAllStringSubmatch(rest, -1)
	if len(tokens) == 0 {
		return Rule{}, fmt.Errorf("no backtick-delimited token found")
	}

	// Replace every `token` by a placeholder, then walk the words:
	//   `pkg`  [match `glob`]  [except match `glob` | except `file`]
	words := strings.Fields(backtickRE.ReplaceAllString(rest, "SLOT"))
	next := 0 // index of the next backtick token
	take := func() string { v := tokens[next][1]; next++; return v }

	if len(words) == 0 || words[0] != "SLOT" {
		return Rule{}, fmt.Errorf("expected a backtick-quoted package after 'export'")
	}
	r := Rule{Package: take(), Line: lineNum}

	for i := 1; i < len(words); i++ {
		switch strings.ToLower(words[i]) {
		case "match":
			if i+1 >= len(words) || words[i+1] != "SLOT" {
				return Rule{}, fmt.Errorf("`match` must be followed by a backtick-quoted glob")
			}
			if r.MatchGlob != "" {
				return Rule{}, fmt.Errorf("more than one `match` clause")
			}
			r.MatchGlob = take()
			i++
		case "except":
			if i+1 < len(words) && strings.ToLower(words[i+1]) == "match" {
				if i+2 >= len(words) || words[i+2] != "SLOT" {
					return Rule{}, fmt.Errorf("`except match` must be followed by a backtick-quoted glob")
				}
				if r.ExceptGlob != "" {
					return Rule{}, fmt.Errorf("more than one `except match` clause")
				}
				r.ExceptGlob = take()
				i += 2
			} else {
				if i+1 >= len(words) || words[i+1] != "SLOT" {
					return Rule{}, fmt.Errorf("`except` must be followed by a backtick-quoted file name or `match`")
				}
				if r.ExceptFile != "" {
					return Rule{}, fmt.Errorf("more than one `except` clause")
				}
				r.ExceptFile = take()
				i++
			}
		default:
			// A typo ("mach", "excpet") or a stray value: silently ignoring it would export
			// MORE than the author meant.
			w := words[i]
			if w == "SLOT" {
				w = "`" + tokens[next][1] + "`"
			}
			return Rule{}, fmt.Errorf("unexpected word %s (expected `match` or `except` followed by a backtick-quoted value)", quoteWord(w))
		}
	}
	return r, nil
}

func quoteWord(w string) string {
	if strings.HasPrefix(w, "`") {
		return w
	}
	return fmt.Sprintf("%q", w)
}

// Match returns the list of .loom file paths in baseDir that satisfy rule r.
// baseDir is the root directory (e.g. loom/src/prompts).
func (r *Rule) Match(baseDir string) ([]string, error) {
	// Convert package dot-path to directory path.
	relDir := filepath.Join(strings.Split(r.Package, ".")...)
	dir := filepath.Join(baseDir, relDir)

	var matches []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip missing dirs
		}
		if d.IsDir() || !isLoomFile(d.Name()) {
			return nil
		}
		name := d.Name()

		// Apply match glob filter.
		if r.MatchGlob != "" {
			ok, _ := filepath.Match(r.MatchGlob, name)
			if !ok {
				return nil
			}
		}

		// Apply except glob filter.
		if r.ExceptGlob != "" {
			skip, _ := filepath.Match(r.ExceptGlob, name)
			if skip {
				return nil
			}
		}

		// Apply except specific file filter.
		if r.ExceptFile != "" && name == r.ExceptFile {
			return nil
		}

		matches = append(matches, path)
		return nil
	})
	return matches, err
}

func isLoomFile(name string) bool {
	return strings.HasSuffix(name, ".prompt.loom") ||
		strings.HasSuffix(name, ".block.loom") ||
		strings.HasSuffix(name, ".overlay.loom") ||
		strings.HasSuffix(name, ".loom")
}

// WriteDefault writes a skeleton .export.loom to path.
func WriteDefault(path string) error {
	content := `# .export.loom — declare which prompts to export
# Syntax:
#   export ` + "`" + `package` + "`" + `
#   export ` + "`" + `package.subpackage` + "`" + ` match ` + "`" + `*-final.prompt.loom` + "`" + `
#   export ` + "`" + `package` + "`" + ` except match ` + "`" + `*-temp.prompt.loom` + "`" + `

export ` + "`prompts`" + `
`
	return os.WriteFile(path, []byte(content), 0o644)
}
