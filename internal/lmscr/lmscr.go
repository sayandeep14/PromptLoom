// Package lmscr runs .lmscr files: a script is a named, ordered list of loom CLI invocations
// (weave, eval, optimize, quest run, deploy, ...), each run exactly as if it had been typed by
// hand. A script adds no capability beyond what those commands already have on their own — it
// only saves re-typing a sequence of them, and lets one be checked in, reviewed and replayed. See
// docs/AGENT_RUNTIME.md, "`loom script`: not a new capability".
package lmscr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SuiteSuffix is the file extension of a loom script.
const SuiteSuffix = ".lmscr"

// DefaultDir is where scripts live, relative to the project.
const DefaultDir = "scripts"

// validWhen are the values a step's "when" may take.
var validWhen = map[string]bool{"": true, "on_success": true, "on_failure": true, "always": true}

// Step is one command a script runs: `loom <Run> <Args...>`, with {{vars.NAME}} substituted in
// Args from the script's own vars and any passed in at run time.
type Step struct {
	Name           string   `toml:"name"`
	Run            string   `toml:"run"`  // a loom (sub)command, e.g. "weave", "quest run", "eval"
	Args           []string `toml:"args"` // its arguments, exactly as they'd follow "loom <Run>"
	When           string   `toml:"when"` // "on_success" (default), "on_failure", "always"
	ContinueOnFail bool     `toml:"continue_on_fail"`
}

// Effective returns the "when" a step runs on, defaulting to "on_success".
func (s Step) Effective() string {
	if s.When == "" {
		return "on_success"
	}
	return s.When
}

// Script is a parsed .lmscr file.
type Script struct {
	Name        string // file name without the suffix
	Path        string
	Description string            `toml:"description"`
	Vars        map[string]string `toml:"vars"`
	Steps       []Step            `toml:"step"`
}

var varTokenRe = regexp.MustCompile(`\{\{vars\.([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// LoadScript parses and validates one .lmscr file.
func LoadScript(path string) (*Script, error) {
	var s Script
	md, err := toml.DecodeFile(path, &s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s) %s (a script has description, vars and [[step]] entries with "+
			"name, run, args, when, continue_on_fail)", path, strings.Join(keys, ", "))
	}
	s.Path = path
	s.Name = strings.TrimSuffix(filepath.Base(path), SuiteSuffix)
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

func (s *Script) validate() error {
	if len(s.Steps) == 0 {
		return fmt.Errorf("no [[step]] entries")
	}
	seen := map[string]bool{}
	for i, st := range s.Steps {
		label := fmt.Sprintf("step %d", i+1)
		if st.Name != "" {
			label = fmt.Sprintf("step %q", st.Name)
		}
		switch {
		case strings.TrimSpace(st.Name) == "":
			return fmt.Errorf("step %d has no name", i+1)
		case seen[st.Name]:
			return fmt.Errorf("%s: name used twice", label)
		case strings.TrimSpace(st.Run) == "":
			return fmt.Errorf("%s: no run (which loom command to run, e.g. \"weave\" or \"quest run\")", label)
		case !validWhen[st.When]:
			return fmt.Errorf("%s: unknown when %q (use on_success, on_failure or always)", label, st.When)
		}
		seen[st.Name] = true
	}
	return nil
}

// referencedVars returns every {{vars.NAME}} token used anywhere in the script's step arguments.
func (s *Script) referencedVars() []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range s.Steps {
		for _, a := range st.Args {
			for _, m := range varTokenRe.FindAllStringSubmatch(a, -1) {
				if !seen[m[1]] {
					seen[m[1]] = true
					out = append(out, m[1])
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// Substitute replaces every {{vars.NAME}} in s with vars[NAME]. A token whose name is not in vars
// is left as-is (visible, not silently dropped) — used for previews; Run itself checks every
// referenced name is present before anything runs, so it never actually hits that case.
func Substitute(s string, vars map[string]string) string {
	return varTokenRe.ReplaceAllStringFunc(s, func(tok string) string {
		name := varTokenRe.FindStringSubmatch(tok)[1]
		if v, ok := vars[name]; ok {
			return v
		}
		return tok
	})
}

// FindScripts returns the .lmscr files in dir, sorted. A missing directory means no scripts.
func FindScripts(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), SuiteSuffix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}
