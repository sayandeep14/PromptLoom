// Package eval scores a prompt's answers with a judge model. An eval suite (evals/<Name>.eval.toml)
// names a prompt and a set of cases; each case has an input and the criteria a good answer must
// meet. The prompt is rendered and sent to the model(s) under test, a judge model grades every
// criterion 0-100, and the case passes when its mean reaches a threshold (and the prompt's own
// contract holds). Scores can be recorded as a baseline and compared later, so a change to a
// prompt that makes answers worse is caught in CI.
package eval

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// DefaultThreshold is the score a case must reach when neither it nor its suite says otherwise.
const DefaultThreshold = 70

// SuiteSuffix is the file name suffix of an eval suite.
const SuiteSuffix = ".eval.toml"

// DefaultDir is where suites live, relative to the project.
const DefaultDir = "evals"

// Case is one input and what a good answer looks like.
type Case struct {
	Name      string            `toml:"name"`
	Input     string            `toml:"input"`      // the user message sent with the rendered prompt
	InputFile string            `toml:"input_file"` // alternative to input, relative to the suite file
	Criteria  []string          `toml:"criteria"`   // what the judge grades, each 0-100
	Reference string            `toml:"reference"`  // optional model answer the judge may compare against
	MinScore  *int              `toml:"min_score"`  // overrides the suite threshold for this case
	Vars      map[string]string `toml:"vars"`       // values for the prompt's variables and slots
}

// Suite is a parsed .eval.toml file.
type Suite struct {
	Name       string // file name without the suffix
	Path       string
	Prompt     string `toml:"prompt"`
	Threshold  *int   `toml:"threshold"`
	JudgeModel string `toml:"judge_model"` // "model" or "provider:model"
	Cases      []Case `toml:"case"`
}

// ThresholdFor returns the pass mark of a case.
func (s *Suite) ThresholdFor(c Case, override int) int {
	switch {
	case override > 0:
		return override
	case c.MinScore != nil:
		return *c.MinScore
	case s.Threshold != nil:
		return *s.Threshold
	}
	return DefaultThreshold
}

// LoadSuite parses and validates one suite file.
func LoadSuite(path string) (*Suite, error) {
	var s Suite
	md, err := toml.DecodeFile(path, &s)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if u := md.Undecoded(); len(u) > 0 {
		keys := make([]string, len(u))
		for i, k := range u {
			keys[i] = k.String()
		}
		return nil, fmt.Errorf("%s: unknown key(s) %s (a suite has prompt, threshold, judge_model and [[case]] entries with name, input, input_file, criteria, reference, min_score, vars)", path, strings.Join(keys, ", "))
	}
	s.Path = path
	s.Name = strings.TrimSuffix(filepath.Base(path), SuiteSuffix)
	if err := s.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// input_file is read now so a missing file fails at load time, not after the model was paid for
	for i := range s.Cases {
		c := &s.Cases[i]
		if c.InputFile != "" {
			p := c.InputFile
			if !filepath.IsAbs(p) {
				p = filepath.Join(filepath.Dir(path), p)
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("%s: case %q: cannot read input_file: %w", path, c.Name, err)
			}
			c.Input = string(data)
		}
	}
	return &s, nil
}

func (s *Suite) validate() error {
	if strings.TrimSpace(s.Prompt) == "" {
		return fmt.Errorf("missing prompt: name the prompt to evaluate (prompt = \"CodeReviewer\")")
	}
	if len(s.Cases) == 0 {
		return fmt.Errorf("no [[case]] entries")
	}
	if s.Threshold != nil && (*s.Threshold < 1 || *s.Threshold > 100) {
		return fmt.Errorf("threshold %d is outside 1-100", *s.Threshold)
	}
	seen := map[string]bool{}
	for i, c := range s.Cases {
		label := fmt.Sprintf("case %d", i+1)
		if c.Name != "" {
			label = fmt.Sprintf("case %q", c.Name)
		}
		switch {
		case strings.TrimSpace(c.Name) == "":
			return fmt.Errorf("case %d has no name", i+1)
		case seen[c.Name]:
			return fmt.Errorf("%s: name used twice (names identify the case in baselines)", label)
		case strings.TrimSpace(c.Input) == "" && c.InputFile == "":
			return fmt.Errorf("%s: needs input or input_file", label)
		case c.Input != "" && c.InputFile != "":
			return fmt.Errorf("%s: use input or input_file, not both", label)
		case len(nonEmpty(c.Criteria)) == 0:
			return fmt.Errorf("%s: needs at least one criterion (what must a good answer do?)", label)
		case c.MinScore != nil && (*c.MinScore < 1 || *c.MinScore > 100):
			return fmt.Errorf("%s: min_score %d is outside 1-100", label, *c.MinScore)
		}
		seen[c.Name] = true
	}
	return nil
}

func nonEmpty(in []string) []string {
	var out []string
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// FindSuites returns the suite files in dir, sorted. A missing directory means no suites.
func FindSuites(dir string) ([]string, error) {
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
