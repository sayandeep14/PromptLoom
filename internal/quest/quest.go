// Package quest runs a small, fixed sequence of `loom run` steps: a "guided task" made of several
// prompts, each fed by the quest's own input and (optionally) the previous step's answer. It adds
// no capability `loom run` does not already have — no tool use, no branching, no autonomy — it is
// only a way to name and replay a short pipeline of prompts. See docs/AGENT_RUNTIME.md.
package quest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// SuiteSuffix is the file name suffix of a quest file.
const SuiteSuffix = ".quest.toml"

// DefaultDir is where quest files live, relative to the project.
const DefaultDir = "quests"

// Step is one leg of a quest: run one prompt with one message.
type Step struct {
	Name           string            `toml:"name"`
	Prompt         string            `toml:"prompt"`
	Input          string            `toml:"input"`
	InputFile      string            `toml:"input_file"`
	Vars           map[string]string `toml:"vars"`
	Variant        string            `toml:"variant"`
	Overlay        []string          `toml:"overlay"`
	Env            string            `toml:"env"`
	With           []string          `toml:"with"`
	ContinueOnFail bool              `toml:"continue_on_fail"`
}

// Quest is a parsed .quest.toml file.
type Quest struct {
	Name        string // file name without the suffix
	Path        string
	Description string `toml:"description"`
	Steps       []Step `toml:"step"`
}

// questTemplateTokens are the only substitutions a step's input may use.
var questTemplateTokens = []string{"{{quest.input}}", "{{quest.previous}}"}

// LoadQuest parses and validates one quest file.
func LoadQuest(path string) (*Quest, error) {
	var q Quest
	md, err := toml.DecodeFile(path, &q)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s) %s (a quest has description and [[step]] entries with "+
			"name, prompt, input, input_file, vars, variant, overlay, env, with, continue_on_fail)", path, strings.Join(keys, ", "))
	}
	q.Path = path
	q.Name = strings.TrimSuffix(filepath.Base(path), SuiteSuffix)
	if err := q.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range q.Steps {
		s := &q.Steps[i]
		if s.InputFile != "" {
			p := s.InputFile
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("%s: step %q: cannot read input_file: %w", path, s.Name, err)
			}
			s.Input = string(data)
		}
	}
	return &q, nil
}

func (q *Quest) validate() error {
	if len(q.Steps) == 0 {
		return fmt.Errorf("no [[step]] entries")
	}
	seen := map[string]bool{}
	for i, s := range q.Steps {
		label := fmt.Sprintf("step %d", i+1)
		if s.Name != "" {
			label = fmt.Sprintf("step %q", s.Name)
		}
		switch {
		case strings.TrimSpace(s.Name) == "":
			return fmt.Errorf("step %d has no name", i+1)
		case seen[s.Name]:
			return fmt.Errorf("%s: name used twice", label)
		case strings.TrimSpace(s.Prompt) == "":
			return fmt.Errorf("%s: no prompt", label)
		case strings.TrimSpace(s.Input) == "" && s.InputFile == "":
			return fmt.Errorf("%s: needs input or input_file", label)
		case s.Input != "" && s.InputFile != "":
			return fmt.Errorf("%s: use input or input_file, not both", label)
		case i == 0 && strings.Contains(s.Input, "{{quest.previous}}"):
			return fmt.Errorf("%s: {{quest.previous}} has no meaning in the first step", label)
		}
		if bad := unknownTokens(s.Input); len(bad) > 0 {
			return fmt.Errorf("%s: unknown template token(s) %s (only {{quest.input}} and {{quest.previous}} are substituted)", label, strings.Join(bad, ", "))
		}
		seen[s.Name] = true
	}
	return nil
}

// unknownTokens finds "{{...}}" occurrences in s that are not one of questTemplateTokens. A
// mistyped token would otherwise reach the model literally, unsubstituted, with no warning.
func unknownTokens(s string) []string {
	var bad []string
	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "}}")
		if end < 0 {
			break
		}
		tok := s[start : start+end+2]
		known := false
		for _, k := range questTemplateTokens {
			if tok == k {
				known = true
				break
			}
		}
		if !known {
			bad = append(bad, tok)
		}
		s = s[start+end+2:]
	}
	return bad
}

// substitute replaces the quest template tokens in a step's input.
func substitute(s, questInput, previous string) string {
	s = strings.ReplaceAll(s, "{{quest.input}}", questInput)
	s = strings.ReplaceAll(s, "{{quest.previous}}", previous)
	return s
}

// FindQuests returns the quest files in dir, sorted. A missing directory means no quests.
func FindQuests(dir string) ([]string, error) {
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
