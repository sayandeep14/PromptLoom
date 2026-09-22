package optimize

import (
	"context"
	"fmt"

	"github.com/sayandeep14/PromptLoom/internal/eval"
)

// Score is a prompt's quality as one number: the mean of every case score its eval suite(s)
// produced, across every model tested.
type Score struct {
	Prompt  string
	Mean    float64 // 0-100; 0 when nothing could be scored
	Cases   int     // cases that produced a score (errors are not counted)
	Passed  int
	Errored int
	Suites  []string
	Outcome *eval.Outcome // full per-case detail
}

// OK reports whether every scored case passed (errored cases are not "OK").
func (s *Score) OK() bool { return s.Cases > 0 && s.Passed == s.Cases && s.Errored == 0 }

// ComputeScore runs the eval suite(s) that target `name` and reduces them to one number. It is an
// error for a prompt to have no eval suite: there is nothing honest to score it against. Suite
// selection, models and judge follow the same rules as `loom eval` (eval.Params, with Names
// forced to just this prompt).
func ComputeScore(ctx context.Context, cwd, name string, p eval.Params) (*Score, error) {
	p.Names = []string{name}
	p.Compare = false
	p.Record = false
	out, err := eval.RunProject(ctx, cwd, p)
	if err != nil {
		return nil, err
	}
	s := &Score{Prompt: name, Outcome: out}
	sum := 0
	for _, so := range out.Suites {
		s.Suites = append(s.Suites, so.Suite.Name)
		for _, r := range so.Results {
			if r.Err != nil {
				s.Errored++
				continue
			}
			s.Cases++
			sum += r.Score
			if r.Passed {
				s.Passed++
			}
		}
	}
	if s.Cases > 0 {
		s.Mean = float64(sum) / float64(s.Cases)
	}
	return s, nil
}

// Text renders a compact human summary.
func (s *Score) Text() string {
	if s.Cases == 0 && s.Errored == 0 {
		return fmt.Sprintf("%s: no cases to score\n", s.Prompt)
	}
	verdict := "FAIL"
	if s.OK() {
		verdict = "PASS"
	}
	out := fmt.Sprintf("%s  score %.1f/100  (%d/%d passed", s.Prompt, s.Mean, s.Passed, s.Cases)
	if s.Errored > 0 {
		out += fmt.Sprintf(", %d errored", s.Errored)
	}
	return out + fmt.Sprintf(")  [%s]\n", verdict)
}
