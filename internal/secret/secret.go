// Package secret loads the .loomsecret file from the project root into the process environment.
// .loomsecret uses KEY=VALUE syntax (one per line); blank lines and # comments are ignored.
// Values are only set when the env var is not already set, so shell-exported vars always win.
package secret

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

const Filename = ".loom.secret"

// Load reads .loomsecret from dir and sets any missing environment variables.
// It is safe to call even when the file does not exist — nothing happens.
func Load(dir string) {
	path := filepath.Join(dir, Filename)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line[:idx]), "export "))
		val := unquote(strings.TrimSpace(line[idx+1:]))
		if key == "" {
			continue
		}
		// Only set if not already present in the environment.
		if os.Getenv(key) == "" {
			os.Setenv(key, val)
		}
	}
}

// unquote removes one pair of matching surrounding quotes, as dotenv files commonly have
// (KEY="value"); without this an API key would be sent with the quote characters in it.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// TemplateContent is the default .loomsecret written by `loom init`.
const TemplateContent = `# .loomsecret — API keys for loom commands (never commit this file)
# Format: KEY=value  (one per line; # lines are comments)

GEMINI_API_KEY=
# ANTHROPIC_API_KEY=
`
