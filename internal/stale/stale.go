// Package stale detects version mentions in prompt text that don't match the
// versions declared in project dependency files (go.mod, package.json, pom.xml,
// Cargo.toml, pyproject.toml, requirements.txt).
package stale

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

// Finding describes a version mismatch between a dependency file and prompt text.
type Finding struct {
	Dep     string // dependency name (e.g. "spring-boot", "react")
	Version string // declared version in dependency file
	Mention string // version text found in prompt
	Prompt  string // prompt name where the mention was found
	Field   string // field name where the mention was found
}

func (f Finding) String() string {
	return fmt.Sprintf("[stale] %s.%s: %q mentions version %q but %s declares %s",
		f.Prompt, f.Field, f.Prompt, f.Mention, f.Dep, f.Version)
}

// DepVersion is a (name, version) pair from a dependency file.
type DepVersion struct {
	Name    string
	Version string
	Source  string // file the version came from
}

// Scan reads dependency files from cwd and checks all prompts for stale versions.
func Scan(prompts []*ast.ResolvedPrompt, cwd string) ([]Finding, error) {
	deps, err := ScanDeps(cwd)
	if err != nil {
		return nil, err
	}
	if len(deps) == 0 {
		return nil, nil
	}

	var findings []Finding
	for _, rp := range prompts {
		findings = append(findings, checkPrompt(rp, deps)...)
	}
	return findings, nil
}

// ScanDeps reads all supported dependency files from cwd and returns version pairs.
func ScanDeps(cwd string) ([]DepVersion, error) {
	var deps []DepVersion

	parsers := []struct {
		file string
		fn   func([]byte) ([]DepVersion, error)
	}{
		{"go.mod", parseGoMod},
		{"package.json", parsePackageJSON},
		{"pom.xml", parsePomXML},
		{"Cargo.toml", parseCargoToml},
		{"pyproject.toml", parsePyprojectToml},
		{"requirements.txt", parseRequirementsTxt},
	}

	for _, p := range parsers {
		path := filepath.Join(cwd, p.file)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // file not present — skip
		}
		parsed, err := p.fn(data)
		if err != nil {
			continue // malformed file — skip gracefully
		}
		for i := range parsed {
			parsed[i].Source = p.file
		}
		deps = append(deps, parsed...)
	}
	return deps, nil
}

// ---- prompt checker ----

// versionRE matches common semver-style version strings like "3.2.1" or "17".
var versionRE = regexp.MustCompile(`\b(\d+(?:\.\d+){0,2})\b`)

func checkPrompt(rp *ast.ResolvedPrompt, deps []DepVersion) []Finding {
	var findings []Finding
	fields := map[string]string{
		"summary":      rp.Summary,
		"persona":      rp.Persona,
		"context":      rp.Context,
		"objective":    rp.Objective,
		"notes":        rp.Notes,
	}
	listFields := map[string][]string{
		"instructions":   rp.Instructions,
		"constraints":    rp.Constraints,
		"examples":       rp.Examples,
		"format":         rp.Format,
	}

	for field, text := range fields {
		if text == "" {
			continue
		}
		findings = append(findings, checkText(rp.Name, field, text, deps)...)
	}
	for field, items := range listFields {
		for _, item := range items {
			findings = append(findings, checkText(rp.Name, field, item, deps)...)
		}
	}
	return findings
}

func checkText(prompt, field, text string, deps []DepVersion) []Finding {
	var findings []Finding
	textLower := strings.ToLower(text)
	for _, dep := range deps {
		nameLower := strings.ToLower(dep.Name)
		if !strings.Contains(textLower, nameLower) {
			continue
		}
		// Find version mentions that appear near the dep name.
		matches := versionRE.FindAllString(text, -1)
		for _, mention := range matches {
			if mention == dep.Version {
				continue // matches — not stale
			}
			// Only flag if the mentioned version is plausibly different.
			if !isCompatibleVersion(mention, dep.Version) {
				findings = append(findings, Finding{
					Dep:     dep.Name,
					Version: dep.Version,
					Mention: mention,
					Prompt:  prompt,
					Field:   field,
				})
			}
		}
	}
	return findings
}

// isCompatibleVersion returns true if mention is a prefix of declared
// (e.g. "17" is compatible with "17.0.1") so we don't over-report.
func isCompatibleVersion(mention, declared string) bool {
	return strings.HasPrefix(declared, mention) || strings.HasPrefix(mention, declared)
}

// ---- dependency file parsers ----

func parseGoMod(data []byte) ([]DepVersion, error) {
	var deps []DepVersion
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		// "module" and "go" lines
		if strings.HasPrefix(line, "go ") {
			parts := strings.Fields(line)
			if len(parts) == 2 {
				deps = append(deps, DepVersion{Name: "go", Version: parts[1]})
			}
			continue
		}
		// require lines: "  github.com/foo/bar v1.2.3"
		if strings.Contains(line, " v") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				ver := strings.TrimPrefix(parts[1], "v")
				name := moduleName(parts[0])
				deps = append(deps, DepVersion{Name: name, Version: ver})
			}
		}
	}
	return deps, nil
}

func moduleName(path string) string {
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

type packageJSON struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func parsePackageJSON(data []byte) ([]DepVersion, error) {
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	var deps []DepVersion
	for name, ver := range pkg.Dependencies {
		deps = append(deps, DepVersion{Name: name, Version: cleanSemver(ver)})
	}
	for name, ver := range pkg.DevDependencies {
		deps = append(deps, DepVersion{Name: name, Version: cleanSemver(ver)})
	}
	return deps, nil
}

func cleanSemver(v string) string {
	v = strings.TrimPrefix(v, "^")
	v = strings.TrimPrefix(v, "~")
	v = strings.TrimPrefix(v, ">=")
	v = strings.TrimPrefix(v, ">")
	v = strings.TrimPrefix(v, "=")
	if idx := strings.Index(v, " "); idx != -1 {
		v = v[:idx]
	}
	return v
}

// pom.xml partial model.
type mavenProject struct {
	Parent struct {
		GroupID    string `xml:"groupId"`
		ArtifactID string `xml:"artifactId"`
		Version    string `xml:"version"`
	} `xml:"parent"`
	Properties struct {
		Inner []struct {
			XMLName xml.Name
			Value   string `xml:",chardata"`
		} `xml:",any"`
	} `xml:"properties"`
	Dependencies struct {
		Deps []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
			Version    string `xml:"version"`
		} `xml:"dependency"`
	} `xml:"dependencies"`
}

func parsePomXML(data []byte) ([]DepVersion, error) {
	var proj mavenProject
	if err := xml.Unmarshal(data, &proj); err != nil {
		return nil, err
	}
	var deps []DepVersion
	if proj.Parent.Version != "" {
		deps = append(deps, DepVersion{
			Name:    proj.Parent.ArtifactID,
			Version: proj.Parent.Version,
		})
	}
	for _, d := range proj.Dependencies.Deps {
		if d.Version != "" {
			deps = append(deps, DepVersion{Name: d.ArtifactID, Version: d.Version})
		}
	}
	for _, prop := range proj.Properties.Inner {
		name := prop.XMLName.Local
		if strings.HasSuffix(name, ".version") || strings.HasSuffix(name, "Version") {
			base := strings.TrimSuffix(strings.TrimSuffix(name, ".version"), "Version")
			deps = append(deps, DepVersion{Name: base, Version: strings.TrimSpace(prop.Value)})
		}
	}
	return deps, nil
}

func parseCargoToml(data []byte) ([]DepVersion, error) {
	// Minimal TOML line-by-line parser for [dependencies] section.
	var deps []DepVersion
	inDeps := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "[dependencies]" || line == "[dev-dependencies]" {
			inDeps = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inDeps = false
			continue
		}
		if !inDeps || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		name := strings.TrimSpace(parts[0])
		ver := extractCargoVersion(strings.TrimSpace(parts[1]))
		if ver != "" {
			deps = append(deps, DepVersion{Name: name, Version: ver})
		}
	}
	return deps, nil
}

func extractCargoVersion(s string) string {
	// e.g. `"1.2.3"` or `{ version = "1.2.3", ... }`
	s = strings.Trim(s, `"' `)
	if strings.HasPrefix(s, "{") {
		re := regexp.MustCompile(`version\s*=\s*"([^"]+)"`)
		if m := re.FindStringSubmatch(s); len(m) > 1 {
			return m[1]
		}
		return ""
	}
	return versionRE.FindString(s)
}

func parsePyprojectToml(data []byte) ([]DepVersion, error) {
	var deps []DepVersion
	inDeps := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "[tool.poetry.dependencies]" || line == "[project]" {
			inDeps = true
			continue
		}
		if strings.HasPrefix(line, "[") {
			inDeps = false
			continue
		}
		if !inDeps || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		name := strings.TrimSpace(parts[0])
		ver := cleanSemver(strings.Trim(strings.TrimSpace(parts[1]), `"'`))
		if ver != "" && name != "python" {
			deps = append(deps, DepVersion{Name: name, Version: ver})
		}
	}
	return deps, nil
}

func parseRequirementsTxt(data []byte) ([]DepVersion, error) {
	var deps []DepVersion
	re := regexp.MustCompile(`^([A-Za-z0-9_.-]+)[=~><!]+([0-9][^\s,;]*)`)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := re.FindStringSubmatch(line); len(m) > 2 {
			deps = append(deps, DepVersion{Name: m[1], Version: m[2]})
		}
	}
	return deps, nil
}
