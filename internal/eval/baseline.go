package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// BaselineDir is where recorded scores live, inside the evals directory.
const BaselineDir = ".baseline"

// DefaultTolerance is how many points a score may drop before it counts as a regression. Judge
// scores wobble a little from run to run; a small tolerance keeps the gate from flapping.
const DefaultTolerance = 5

// BaselineEntry is the recorded outcome of one case on one model.
type BaselineEntry struct {
	Case     string `json:"case"`
	Model    string `json:"model"`
	Score    int    `json:"score"`
	Criteria []int  `json:"criteria"`
	Response string `json:"response"`
}

// Baseline is a recorded run of a suite.
type Baseline struct {
	Version int             `json:"version"`
	Suite   string          `json:"suite"`
	Prompt  string          `json:"prompt"`
	Entries []BaselineEntry `json:"entries"`
}

// BaselinePath is the file that holds the baseline of a suite.
func BaselinePath(dir, suite string) string {
	return filepath.Join(dir, BaselineDir, suite+".json")
}

// Record stores the scores of the results that completed (errors carry no score). It returns how
// many results were recorded. Entries are sorted so the file diffs cleanly in git.
func Record(dir string, s *Suite, results []CaseResult) (int, error) {
	b := Baseline{Version: 1, Suite: s.Name, Prompt: s.Prompt}
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		e := BaselineEntry{Case: r.Case, Model: r.Model, Score: r.Score, Response: r.Response}
		for _, c := range r.Criteria {
			e.Criteria = append(e.Criteria, c.Score)
		}
		b.Entries = append(b.Entries, e)
	}
	sort.Slice(b.Entries, func(i, j int) bool {
		if b.Entries[i].Case != b.Entries[j].Case {
			return b.Entries[i].Case < b.Entries[j].Case
		}
		return b.Entries[i].Model < b.Entries[j].Model
	})
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return 0, err
	}
	path := BaselinePath(dir, s.Name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".baseline-*")
	if err != nil {
		return 0, err
	}
	name := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(name)
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return 0, err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return 0, err
	}
	return len(b.Entries), nil
}

// LoadBaseline reads the baseline of a suite; (nil, nil) when none was recorded.
func LoadBaseline(dir, suite string) (*Baseline, error) {
	data, err := os.ReadFile(BaselinePath(dir, suite))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("%s: %w", BaselinePath(dir, suite), err)
	}
	return &b, nil
}

// Status of a case compared with its baseline.
type Status string

const (
	StatusSame      Status = "same"
	StatusImproved  Status = "improved"
	StatusRegressed Status = "regressed"
	StatusNew       Status = "new" // no baseline entry: nothing to compare with
)

// Comparison is one case-on-model score against its baseline.
type Comparison struct {
	Case, Model string
	Old, New    int
	Status      Status
}

// Delta is New - Old.
func (c Comparison) Delta() int { return c.New - c.Old }

// Compare matches results with baseline entries. A score that dropped by more than tolerance
// points is a regression; a gain of more than tolerance an improvement. Results that failed to
// run are skipped (they are reported as errors on their own).
func Compare(b *Baseline, results []CaseResult, tolerance int) []Comparison {
	old := map[string]int{}
	if b != nil {
		for _, e := range b.Entries {
			old[e.Case+"\x00"+e.Model] = e.Score
		}
	}
	var out []Comparison
	for _, r := range results {
		if r.Err != nil {
			continue
		}
		c := Comparison{Case: r.Case, Model: r.Model, New: r.Score, Status: StatusNew}
		if o, ok := old[r.Case+"\x00"+r.Model]; ok {
			c.Old = o
			switch d := r.Score - o; {
			case d < -tolerance:
				c.Status = StatusRegressed
			case d > tolerance:
				c.Status = StatusImproved
			default:
				c.Status = StatusSame
			}
		}
		out = append(out, c)
	}
	return out
}

// Regressions counts the regressed comparisons.
func Regressions(cs []Comparison) int {
	n := 0
	for _, c := range cs {
		if c.Status == StatusRegressed {
			n++
		}
	}
	return n
}
