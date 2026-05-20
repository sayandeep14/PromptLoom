package locker

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	icrypto "github.com/sayandeepgiri/promptloom/loomlocker/internal/crypto"
	"gopkg.in/yaml.v3"
)

// ParseSecretEntry splits an entry like "app.yaml:{kafka.consumer-id}" into
// (file, key). Bare filenames return key="".
func ParseSecretEntry(entry string) (file, key string) {
	idx := strings.Index(entry, ":{")
	if idx < 0 {
		return entry, ""
	}
	file = entry[:idx]
	key = strings.TrimSuffix(strings.TrimPrefix(entry[idx+2:], ""), "}")
	return file, key
}

// LockSecrets replaces real values with random tokens for all entries in cfg.Secret.
// Populates state.Mapping with the originals.
func LockSecrets(secrets []string, workDir string, state *State) error {
	for _, entry := range secrets {
		file, key := ParseSecretEntry(entry)
		absPath := absPath(file, workDir)

		if key == "" {
			if err := lockWholeFile(absPath, state); err != nil {
				return fmt.Errorf("lock %s: %w", file, err)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(file))
			var err error
			if ext == ".yaml" || ext == ".yml" {
				err = lockYAMLKey(absPath, key, state)
			} else {
				err = lockEnvKey(absPath, key, state)
			}
			if err != nil {
				return fmt.Errorf("lock %s:{%s}: %w", file, key, err)
			}
		}
	}
	return nil
}

// UnlockSecrets restores original values from state.Mapping.
func UnlockSecrets(secrets []string, workDir string, state *State) error {
	for _, entry := range secrets {
		file, key := ParseSecretEntry(entry)
		absPath := absPath(file, workDir)

		if key == "" {
			if err := unlockWholeFile(absPath, state); err != nil {
				return fmt.Errorf("unlock %s: %w", file, err)
			}
		} else {
			ext := strings.ToLower(filepath.Ext(file))
			var err error
			if ext == ".yaml" || ext == ".yml" {
				err = unlockYAMLKey(absPath, key, state)
			} else {
				err = unlockEnvKey(absPath, key, state)
			}
			if err != nil {
				return fmt.Errorf("unlock %s:{%s}: %w", file, key, err)
			}
		}
	}
	return nil
}

// ---- env / .loom.secret files ----

// lockWholeFile replaces ALL values in a KEY=VALUE file.
func lockWholeFile(path string, state *State) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		k, v, ok := parseEnvLine(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if icrypto.IsToken(v) {
			out = append(out, line) // already locked, idempotent
			continue
		}
		token := icrypto.RandomToken()
		state.RecordOriginal(path, k, v)
		out = append(out, k+"="+token)
	}
	return writeLines(path, out)
}

// unlockWholeFile restores values replaced by lockWholeFile.
func unlockWholeFile(path string, state *State) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		k, _, ok := parseEnvLine(line)
		if !ok {
			out = append(out, line)
			continue
		}
		if orig, found := state.Original(path, k); found {
			out = append(out, k+"="+orig)
		} else {
			out = append(out, line)
		}
	}
	return writeLines(path, out)
}

// lockEnvKey replaces the value of a specific key in a KEY=VALUE file.
func lockEnvKey(path, key string, state *State) error {
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	found := false
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		k, v, ok := parseEnvLine(line)
		if !ok || k != key {
			out = append(out, line)
			continue
		}
		found = true
		if icrypto.IsToken(v) {
			out = append(out, line)
			continue
		}
		token := icrypto.RandomToken()
		state.RecordOriginal(path, key, v)
		out = append(out, key+"="+token)
	}
	if !found {
		return fmt.Errorf("key %q not found in %s", key, path)
	}
	return writeLines(path, out)
}

// unlockEnvKey restores the value of a specific key.
func unlockEnvKey(path, key string, state *State) error {
	orig, found := state.Original(path, key)
	if !found {
		return nil // nothing to restore
	}
	lines, err := readLines(path)
	if err != nil {
		return err
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		k, _, ok := parseEnvLine(line)
		if ok && k == key {
			out = append(out, key+"="+orig)
		} else {
			out = append(out, line)
		}
	}
	return writeLines(path, out)
}

// ---- YAML files ----

// lockYAMLKey replaces the value at dotted path in a YAML file.
func lockYAMLKey(path, dotPath string, state *State) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("empty YAML document")
	}

	parts := strings.Split(dotPath, ".")
	node, err := findYAMLValueNode(root.Content[0], parts)
	if err != nil {
		return err
	}
	if icrypto.IsToken(node.Value) {
		return nil // already locked
	}
	original := node.Value
	token := icrypto.RandomToken()
	state.RecordOriginal(path, dotPath, original)
	node.Value = token

	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// unlockYAMLKey restores the value at dotted path in a YAML file.
func unlockYAMLKey(path, dotPath string, state *State) error {
	orig, found := state.Original(path, dotPath)
	if !found {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return err
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return nil
	}
	parts := strings.Split(dotPath, ".")
	node, err := findYAMLValueNode(root.Content[0], parts)
	if err != nil {
		return err
	}
	node.Value = orig
	out, err := yaml.Marshal(&root)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

// findYAMLValueNode navigates a yaml.MappingNode by dotted-path parts and
// returns the scalar value node.
func findYAMLValueNode(node *yaml.Node, parts []string) (*yaml.Node, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("expected mapping node, got %v", node.Kind)
	}
	// Mapping nodes interleave key and value nodes: [key0, val0, key1, val1, ...]
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Value != parts[0] {
			continue
		}
		if len(parts) == 1 {
			if valNode.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("path %q is not a scalar value", parts[0])
			}
			return valNode, nil
		}
		return findYAMLValueNode(valNode, parts[1:])
	}
	return nil, fmt.Errorf("YAML key %q not found", parts[0])
}

// ---- helpers ----

func absPath(file, workDir string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(workDir, file)
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, sc.Err()
}

func writeLines(path string, lines []string) error {
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

// parseEnvLine parses "KEY=VALUE", "KEY = VALUE", "export KEY=VALUE".
// Returns (key, value, ok).
func parseEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	// Skip comments and blanks.
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	// Strip "export " prefix.
	line = strings.TrimPrefix(line, "export ")
	idx := strings.IndexByte(line, '=')
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	// Strip surrounding quotes.
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	if key == "" {
		return "", "", false
	}
	return key, val, true
}
