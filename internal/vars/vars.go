package vars

import (
	"regexp"
	"sort"
	"strings"
)

var tokenPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_-]+)\s*\}\}`)

func Tokens(input string) []string {
	matches := tokenPattern.FindAllStringSubmatch(input, -1)
	seen := map[string]bool{}
	var out []string
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func SubstituteString(input string, values map[string]string) (string, []string) {
	var unresolved []string
	seen := map[string]bool{}

	output := tokenPattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := tokenPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		name := strings.TrimSpace(parts[1])
		value, ok := values[name]
		if !ok || value == "" {
			if !seen[name] {
				seen[name] = true
				unresolved = append(unresolved, name)
			}
			return match
		}
		return value
	})

	sort.Strings(unresolved)
	return output, unresolved
}
