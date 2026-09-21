package locker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	icrypto "github.com/sayandeep14/PromptLoom/loomlocker/internal/crypto"
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
	key = strings.TrimSuffix(entry[idx+2:], "}")
	return file, key
}

// How locking works
//
// Locking is ALL-OR-NOTHING and byte-exact:
//
//  1. every file is read and the new contents are computed in memory; any problem
//     (missing file, missing key, unsupported value) aborts before anything is written
//  2. the optional beforeWrite hook runs (recoverable mode saves its encrypted journal here)
//  3. files are replaced atomically (temp file + rename, original permissions kept);
//     if a write fails, files already replaced are put back
//  4. only then is the State updated
//
// Each locked value is replaced by a random token; State.Mapping[file][locked] holds
// the exact original text. `locked` is the exact text placed in the file (so it includes
// any quotes), which makes restoring a plain string replacement that reproduces the
// original bytes — quotes, `export`, spacing, comments and line endings included.

type filePlan struct {
	path    string
	orig    []byte
	mode    os.FileMode
	text    string
	mapping map[string]string // locked text -> original text
}

func loadPlan(path string) (*filePlan, error) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(real)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(real)
	if err != nil {
		return nil, err
	}
	return &filePlan{path: real, orig: data, mode: fi.Mode().Perm(), text: string(data), mapping: map[string]string{}}, nil
}

// LockSecrets replaces the real values named by secrets with random tokens.
func LockSecrets(secrets []string, workDir string, state *State) error {
	return LockSecretsWith(secrets, workDir, state, nil)
}

// LockSecretsWith is LockSecrets with a hook that receives the complete mapping
// (previous locks plus this one) after all changes were computed and before any file
// is modified. If the hook fails, nothing is written.
func LockSecretsWith(secrets []string, workDir string, state *State, beforeWrite func(mapping map[string]map[string]string) error) error {
	plans := map[string]*filePlan{}
	var order []string

	for _, entry := range secrets {
		file, key := ParseSecretEntry(entry)
		abs := absPath(file, workDir)
		plan := plans[abs]
		if plan == nil {
			p, err := loadPlan(abs)
			if err != nil {
				return fmt.Errorf("lock %s: %w", file, err)
			}
			plans[abs] = p
			order = append(order, abs)
			plan = p
		}
		var err error
		switch {
		case key == "" && isJSON(file):
			err = fmt.Errorf("a JSON file needs the path of the value to lock, as in %s:{db.password}", file)
		case key == "":
			err = lockEnvText(plan, true, "")
		case isYAML(file):
			err = lockYAMLText(plan, key)
		case isJSON(file):
			err = lockJSONText(plan, key)
		default:
			err = lockEnvText(plan, false, key)
		}
		if err != nil {
			if key == "" {
				return fmt.Errorf("lock %s: %w", file, err)
			}
			return fmt.Errorf("lock %s:{%s}: %w", file, key, err)
		}
	}

	// Files that actually change.
	var changed []*filePlan
	for _, abs := range order {
		if p := plans[abs]; len(p.mapping) > 0 {
			changed = append(changed, p)
		}
	}
	if len(changed) == 0 {
		return nil
	}

	if beforeWrite != nil {
		merged := state.CopyMapping()
		for _, p := range changed {
			if merged[p.path] == nil {
				merged[p.path] = map[string]string{}
			}
			for locked, raw := range p.mapping {
				merged[p.path][locked] = raw
			}
		}
		if err := beforeWrite(merged); err != nil {
			return fmt.Errorf("prepare lock: %w", err)
		}
	}

	if err := writeAll(changed, func(p *filePlan) []byte { return []byte(p.text) }); err != nil {
		return err
	}
	for _, p := range changed {
		for locked, raw := range p.mapping {
			state.RecordOriginal(p.path, locked, raw)
		}
	}
	return nil
}

// UnlockSecrets restores the original values recorded in state. The secrets argument
// is accepted for symmetry but ignored: what to restore comes from the mapping, so
// changing the config while locked can never strand a locked file.
func UnlockSecrets(_ []string, workDir string, state *State) error {
	_, err := UnlockSecretsReport(workDir, state)
	return err
}

// UnlockSecretsReport is UnlockSecrets that also reports locked values it could not find
// in their file (for example because someone edited or deleted the line while locked).
func UnlockSecretsReport(_ string, state *State) (missing []string, err error) {
	var plans []*filePlan
	paths := make([]string, 0, len(state.Mapping))
	for p := range state.Mapping {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, path := range paths {
		plan, err := loadPlan(path)
		if err != nil {
			return nil, fmt.Errorf("unlock %s: %w", path, err)
		}
		// Deterministic order; each locked text is unique (it embeds a random token).
		lockeds := make([]string, 0, len(state.Mapping[path]))
		for l := range state.Mapping[path] {
			lockeds = append(lockeds, l)
		}
		sort.Strings(lockeds)
		for _, locked := range lockeds {
			raw := state.Mapping[path][locked]
			if !strings.Contains(plan.text, locked) {
				missing = append(missing, path)
				continue
			}
			plan.text = strings.Replace(plan.text, locked, raw, 1)
			plan.mapping[locked] = raw
		}
		if plan.text != string(plan.orig) {
			plans = append(plans, plan)
		}
	}

	if err := writeAll(plans, func(p *filePlan) []byte { return []byte(p.text) }); err != nil {
		return nil, err
	}
	return missing, nil
}

// RestoreFromMapping is the same restore driven by an explicit mapping (used by
// `loomlocker recover`). It returns the number of values restored.
func RestoreFromMapping(mapping map[string]map[string]string) (int, error) {
	st := NewState()
	st.Mapping = mapping
	restored := 0
	for _, m := range mapping {
		restored += len(m)
	}
	missing, err := UnlockSecretsReport("", st)
	if err != nil {
		return 0, err
	}
	return restored - len(missing), nil
}

// writeAll atomically replaces every file, restoring the ones already replaced if a
// later write fails.
func writeAll(plans []*filePlan, content func(*filePlan) []byte) error {
	var done []*filePlan
	for _, p := range plans {
		if err := writeFileAtomic(p.path, content(p), p.mode); err != nil {
			for _, d := range done { // best-effort rollback
				_ = writeFileAtomic(d.path, d.orig, d.mode)
			}
			return fmt.Errorf("write %s: %w", p.path, err)
		}
		done = append(done, p)
	}
	return nil
}

// writeFileAtomic writes via a temp file in the same directory and renames it over the
// target, keeping the permissions, so a crash can never leave a half-written file.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".loomlocker-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func isYAML(file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	return ext == ".yaml" || ext == ".yml"
}

func absPath(file, workDir string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(workDir, file)
}

// ---- KEY=VALUE files (.env, .loom.secret, ...) ----

// lockEnvText locks every assignment (all=true) or every assignment of key. Lines are
// kept exactly (including CRLF and a missing final newline); only the value text changes.
func lockEnvText(plan *filePlan, all bool, key string) error {
	lines := strings.SplitAfter(plan.text, "\n")
	found := false
	for i, line := range lines {
		body, eol := splitEOL(line)
		k, vs, ve, ok := parseEnvSpans(body)
		if !ok || (!all && k != key) {
			continue
		}
		found = true
		raw := body[vs:ve]
		quote, inner := unquote(raw)
		if inner == "" || icrypto.IsToken(inner) {
			continue // nothing to hide, or already locked
		}
		token := icrypto.RandomToken()
		locked := token
		if quote != 0 {
			locked = string(quote) + token + string(quote)
		}
		lines[i] = body[:vs] + locked + body[ve:] + eol
		plan.mapping[locked] = raw
	}
	if !all && !found {
		return fmt.Errorf("key %q not found in %s", key, filepath.Base(plan.path))
	}
	plan.text = strings.Join(lines, "")
	return nil
}

func splitEOL(line string) (body, eol string) {
	switch {
	case strings.HasSuffix(line, "\r\n"):
		return line[:len(line)-2], "\r\n"
	case strings.HasSuffix(line, "\n"):
		return line[:len(line)-1], "\n"
	}
	return line, ""
}

// unquote returns the quote character (0 if none) and the value without it.
func unquote(raw string) (quote byte, inner string) {
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') && raw[len(raw)-1] == raw[0] {
		return raw[0], raw[1 : len(raw)-1]
	}
	return 0, raw
}

// parseEnvSpans parses "KEY=VALUE", "KEY = VALUE" and "export KEY=VALUE" and returns the
// key plus the [vs,ve) byte span of the value within body.
func parseEnvSpans(body string) (key string, vs, ve int, ok bool) {
	trimmed := strings.TrimLeft(body, " \t")
	if trimmed == "" || trimmed[0] == '#' {
		return "", 0, 0, false
	}
	pos := len(body) - len(trimmed)
	if strings.HasPrefix(trimmed, "export ") || strings.HasPrefix(trimmed, "export\t") {
		pos += len("export")
		for pos < len(body) && (body[pos] == ' ' || body[pos] == '\t') {
			pos++
		}
	}
	eq := strings.IndexByte(body[pos:], '=')
	if eq < 0 {
		return "", 0, 0, false
	}
	key = strings.TrimSpace(body[pos : pos+eq])
	if key == "" {
		return "", 0, 0, false
	}
	vs = pos + eq + 1
	for vs < len(body) && (body[vs] == ' ' || body[vs] == '\t') {
		vs++
	}
	ve = len(strings.TrimRight(body, " \t"))
	if ve < vs {
		ve = vs
	}
	return key, vs, ve, true
}

// ---- YAML ----

// lockYAMLText locks the scalar at the dotted path. The file is edited as TEXT at the
// value's exact position, so comments, quoting, indentation and key order are untouched.
// Multi-line values (block scalars, folded plain scalars) are refused rather than mangled.
func lockYAMLText(plan *filePlan, dotPath string) error {
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(plan.text), &root); err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return fmt.Errorf("empty YAML document")
	}
	node, err := findYAMLValueNode(root.Content[0], strings.Split(dotPath, "."))
	if err != nil {
		return err
	}
	if node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return fmt.Errorf("value at %q is a multi-line block scalar, which is not supported (use a single-line value)", dotPath)
	}
	if node.Value == "" || icrypto.IsToken(node.Value) {
		return nil // nothing to hide, or already locked
	}

	start, err := byteOffset(plan.text, node.Line, node.Column)
	if err != nil {
		return err
	}
	end, err := scalarEnd(plan.text, start, node)
	if err != nil {
		return fmt.Errorf("value at %q: %w", dotPath, err)
	}
	raw := plan.text[start:end]
	token := icrypto.RandomToken()
	locked := token
	if raw != "" && (raw[0] == '"' || raw[0] == '\'') {
		locked = string(raw[0]) + token + string(raw[0])
	}
	plan.text = plan.text[:start] + locked + plan.text[end:]
	plan.mapping[locked] = raw
	return nil
}

// byteOffset converts a 1-based (line, column) — the column counts characters — into a byte offset.
func byteOffset(text string, line, col int) (int, error) {
	off := 0
	for l := 1; l < line; l++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return 0, fmt.Errorf("position %d:%d is outside the file", line, col)
		}
		off += i + 1
	}
	rest := text[off:]
	n := 0
	for i := range rest {
		if n == col-1 {
			return off + i, nil
		}
		n++
	}
	if n == col-1 {
		return off + len(rest), nil
	}
	return 0, fmt.Errorf("position %d:%d is outside the file", line, col)
}

// scalarEnd finds where the scalar that starts at start ends, on the same line.
func scalarEnd(text string, start int, node *yaml.Node) (int, error) {
	lineEnd := strings.IndexByte(text[start:], '\n')
	if lineEnd < 0 {
		lineEnd = len(text) - start
	}
	line := text[start : start+lineEnd]

	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		if line == "" || line[0] != '"' {
			return 0, fmt.Errorf("cannot locate the quoted value")
		}
		for i := 1; i < len(line); i++ {
			if line[i] == '\\' {
				i++
			} else if line[i] == '"' {
				return start + i + 1, nil
			}
		}
		return 0, fmt.Errorf("multi-line quoted scalars are not supported")
	case node.Style&yaml.SingleQuotedStyle != 0:
		if line == "" || line[0] != '\'' {
			return 0, fmt.Errorf("cannot locate the quoted value")
		}
		for i := 1; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				return start + i + 1, nil
			}
		}
		return 0, fmt.Errorf("multi-line quoted scalars are not supported")
	}
	// plain scalar: it is exactly node.Value when it fits on one line
	if strings.HasPrefix(line, node.Value) && !strings.Contains(node.Value, "\n") {
		return start + len(node.Value), nil
	}
	return 0, fmt.Errorf("multi-line plain scalars are not supported")
}

// findYAMLValueNode navigates mapping nodes by dotted-path parts and returns the scalar.
func findYAMLValueNode(node *yaml.Node, parts []string) (*yaml.Node, error) {
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty path")
	}
	if node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("YAML key %q not found (its parent is not a mapping)", parts[0])
	}
	// Mapping nodes interleave key and value nodes: [key0, val0, key1, val1, ...]
	for i := 0; i+1 < len(node.Content); i += 2 {
		keyNode, valNode := node.Content[i], node.Content[i+1]
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
