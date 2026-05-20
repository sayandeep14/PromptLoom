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
//   export `pkg`
//   export `pkg` match `glob`
//   export `pkg` except match `glob`
//   export `pkg` match `glob` except `specific-file.loom`
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

	r := Rule{Package: tokens[0][1], Line: lineNum}

	// Strip the package token from rest for further parsing.
	rest = strings.TrimSpace(backtickRE.ReplaceAllString(rest, "SLOT"))
	// Re-extract keywords by scanning remaining tokens.
	keywords := strings.Fields(rest)
	// Re-match full token list (index 0 = package, rest = match/except).
	allTokens := make([]string, len(tokens))
	for i, t := range tokens {
		allTokens[i] = t[1]
	}

	// Parse keyword pattern:
	//   [match `glob`] [except [match `glob` | `file`]]
	// We operate on the original line for simplicity.
	lower := strings.ToLower(line)
	matchIdx := strings.Index(lower, " match `")
	exceptMatchIdx := strings.Index(lower, " except match `")
	exceptFileIdx := strings.Index(lower, " except `")

	if matchIdx >= 0 && (exceptMatchIdx < 0 || matchIdx < exceptMatchIdx) {
		// There is a match clause before any except.
		// Find which backtick token it is.
		// The 'match' keyword precedes the second backtick token.
		if len(allTokens) >= 2 {
			r.MatchGlob = allTokens[1]
		}
	}

	if exceptMatchIdx >= 0 {
		// except match `glob`
		for i, kw := range keywords {
			if kw == "except" && i+1 < len(keywords) && keywords[i+1] == "match" {
				// Find the corresponding token.
				offset := 2 // after package + optional match glob
				if r.MatchGlob != "" {
					offset = 3
				}
				if offset < len(allTokens) {
					r.ExceptGlob = allTokens[offset-1]
					// Re-find: exceptGlob is last backtick token when using except match.
					r.ExceptGlob = allTokens[len(allTokens)-1]
				}
				break
			}
		}
		_ = exceptFileIdx
	} else if exceptFileIdx >= 0 {
		// except `specific-file`
		r.ExceptFile = allTokens[len(allTokens)-1]
	}

	return r, nil
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
#   export `+"`"+`package`+"`"+`
#   export `+"`"+`package.subpackage`+"`"+` match `+"`"+`*-final.prompt.loom`+"`"+`
#   export `+"`"+`package`+"`"+` except match `+"`"+`*-temp.prompt.loom`+"`"+`

export ` + "`prompts`" + `
`
	return os.WriteFile(path, []byte(content), 0o644)
}
