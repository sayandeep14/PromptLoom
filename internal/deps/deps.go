// Package deps parses .dependency.loom files, which declare which prompt-packs
// this project depends on (like Python's requirements.txt).
package deps

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const Filename = ".dependency.loom"

// Constraint operators supported.
const (
	OpExact        = "==" // exact version
	OpGreaterEqual = ">=" // minimum version
	OpGreater      = ">"
	OpLessEqual    = "<="
	OpLess         = "<"
	OpCompatible   = "~=" // compatible release (same as >= but < next major)
)

// Dependency is one entry from .dependency.loom.
type Dependency struct {
	Name    string // vault slug, e.g. "go-backend"
	Op      string // "==", ">=", ">", "<=", "<", "~=", or "" (any version)
	Version string // e.g. "1.0.0"
	Alias   string // optional namespace alias declared with "as <alias>"
	Line    int
}

// String returns the canonical serialisation, e.g. "go-backend==1.0.0".
func (d Dependency) String() string {
	if d.Op == "" || d.Version == "" {
		return d.Name
	}
	return d.Name + d.Op + d.Version
}

var depRE = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*(==|>=|>|<=|<|~=)\s*([A-Za-z0-9._-]+)(?:\s+as\s+([A-Za-z0-9_-]+))?`)
var nameOnlyRE = regexp.MustCompile(`^([A-Za-z0-9_-]+)(?:\s+as\s+([A-Za-z0-9_-]+))?$`)

// ParseFile reads a .dependency.loom file and returns all dependencies.
func ParseFile(path string) ([]Dependency, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var deps []Dependency
	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		line := strings.TrimSpace(sc.Text())
		// Strip inline comments.
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		dep, err := parseLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", filepath.Base(path), lineNum, err)
		}
		deps = append(deps, dep)
	}
	return deps, sc.Err()
}

func parseLine(line string, lineNum int) (Dependency, error) {
	if m := depRE.FindStringSubmatch(line); m != nil {
		return Dependency{Name: m[1], Op: m[2], Version: m[3], Alias: m[4], Line: lineNum}, nil
	}
	if m := nameOnlyRE.FindStringSubmatch(line); m != nil {
		return Dependency{Name: m[1], Alias: m[2], Line: lineNum}, nil
	}
	return Dependency{}, fmt.Errorf("invalid dependency line: %q", line)
}

// ParseContent parses .dependency.loom content from a string (e.g. from a bundle file).
func ParseContent(content string) ([]Dependency, error) {
	var deps []Dependency
	lineNum := 0
	for _, raw := range strings.Split(content, "\n") {
		lineNum++
		line := strings.TrimSpace(raw)
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}
		dep, err := parseLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		deps = append(deps, dep)
	}
	return deps, nil
}

// Installed returns the set of vault slugs found in packDir.
// packDir is typically loom/loompack/ (new structure) or loompack/ (legacy).
func Installed(packDir string) map[string]string {
	installed := map[string]string{}
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return installed
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Read version from pack.json if present.
		meta := filepath.Join(packDir, e.Name(), "pack.json")
		if data, err := os.ReadFile(meta); err == nil {
			var m struct {
				Version string `json:"version"`
			}
			// simple scan without json.Unmarshal to avoid import
			for _, field := range strings.Split(string(data), "\n") {
				if strings.Contains(field, `"version"`) {
					parts := strings.SplitN(field, ":", 2)
					if len(parts) == 2 {
						v := strings.Trim(strings.TrimSpace(parts[1]), `"`)
						m.Version = strings.TrimSuffix(v, ",")
					}
				}
			}
			installed[e.Name()] = m.Version
		} else {
			installed[e.Name()] = ""
		}
	}
	return installed
}

// Missing returns the deps not satisfied by installed packs.
func Missing(deps []Dependency, installed map[string]string) []Dependency {
	var missing []Dependency
	for _, d := range deps {
		if _, ok := installed[d.Name]; !ok {
			missing = append(missing, d)
		}
	}
	return missing
}

// WriteDefault writes a skeleton .dependency.loom to path.
func WriteDefault(path string) error {
	content := `# .dependency.loom — prompt-pack dependencies (like requirements.txt)
# Format:  pack-slug==version   or   pack-slug>=min-version
# Example:
#   go-backend==1.0.0
#   python-data-science>=0.3.0
`
	return os.WriteFile(path, []byte(content), 0o644)
}
