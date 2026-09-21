package eval

import (
	"fmt"
	"strings"
)

// Summary aggregates a set of results.
type Summary struct {
	Total, Passed, Failed, Errored int
	Mean                           float64 // over the cases that produced a score
}

// Summarize counts results.
func Summarize(rs []CaseResult) Summary {
	var s Summary
	sum, scored := 0, 0
	for _, r := range rs {
		s.Total++
		switch {
		case r.Err != nil:
			s.Errored++
		case r.Passed:
			s.Passed++
			sum += r.Score
			scored++
		default:
			s.Failed++
			sum += r.Score
			scored++
		}
	}
	if scored > 0 {
		s.Mean = float64(sum) / float64(scored)
	}
	return s
}

// OK reports whether every case passed.
func (s Summary) OK() bool { return s.Failed == 0 && s.Errored == 0 }

// Report renders the results of one suite for a terminal. With several models the scores are laid
// out as a table (cases × models); failing cases are always explained criterion by criterion.
func Report(s *Suite, results []CaseResult, comparisons []Comparison) string {
	var b strings.Builder
	models := modelsOf(results)
	fmt.Fprintf(&b, "%s  (prompt %s)\n", s.Name, s.Prompt)

	cmp := map[string]Comparison{}
	for _, c := range comparisons {
		cmp[c.Case+"\x00"+c.Model] = c
	}

	if len(models) > 1 {
		writeMatrix(&b, s, results, models)
	}
	for _, m := range models {
		if len(models) > 1 {
			fmt.Fprintf(&b, "\n  model %s\n", m)
		}
		for _, r := range results {
			if r.Model != m {
				continue
			}
			writeCase(&b, r, cmp[r.Case+"\x00"+r.Model])
		}
		sum := Summarize(only(results, m))
		fmt.Fprintf(&b, "  %d case(s): %d passed, %d failed, %d errored · mean %.1f\n", sum.Total, sum.Passed, sum.Failed, sum.Errored, sum.Mean)
	}
	return b.String()
}

func writeCase(b *strings.Builder, r CaseResult, c Comparison) {
	switch {
	case r.Err != nil:
		fmt.Fprintf(b, "  ✗ %-32s error: %v\n", r.Case, r.Err)
		return
	case r.Passed:
		fmt.Fprintf(b, "  ✓ %-32s %3d%s\n", r.Case, r.Score, deltaNote(c))
	default:
		fmt.Fprintf(b, "  ✗ %-32s %3d  (needs %d)%s\n", r.Case, r.Score, r.Threshold, deltaNote(c))
		for _, cr := range r.Criteria {
			if cr.Score < r.Threshold {
				fmt.Fprintf(b, "      %3d  %s", cr.Score, cr.Criterion)
				if cr.Note != "" {
					fmt.Fprintf(b, " — %s", cr.Note)
				}
				b.WriteByte('\n')
			}
		}
		for _, f := range r.ContractFailures {
			fmt.Fprintf(b, "      contract: %s\n", f.Detail)
		}
	}
	if r.Passed && len(r.ContractFailures) > 0 {
		for _, f := range r.ContractFailures {
			fmt.Fprintf(b, "      contract: %s\n", f.Detail)
		}
	}
}

func deltaNote(c Comparison) string {
	switch c.Status {
	case StatusRegressed:
		return fmt.Sprintf("  ▼ was %d (%+d)", c.Old, c.Delta())
	case StatusImproved:
		return fmt.Sprintf("  ▲ was %d (%+d)", c.Old, c.Delta())
	case StatusNew:
		return "  (new: no baseline)"
	}
	return ""
}

func writeMatrix(b *strings.Builder, s *Suite, results []CaseResult, models []string) {
	fmt.Fprintf(b, "\n  %-32s", "case")
	for _, m := range models {
		fmt.Fprintf(b, " %14s", short(m))
	}
	b.WriteByte('\n')
	for _, c := range s.Cases {
		fmt.Fprintf(b, "  %-32s", c.Name)
		for _, m := range models {
			cell := "—"
			for _, r := range results {
				if r.Case == c.Name && r.Model == m {
					switch {
					case r.Err != nil:
						cell = "error"
					case r.Passed:
						cell = fmt.Sprintf("%d ✓", r.Score)
					default:
						cell = fmt.Sprintf("%d ✗", r.Score)
					}
				}
			}
			fmt.Fprintf(b, " %14s", cell)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(b, "  %-32s", "mean")
	for _, m := range models {
		fmt.Fprintf(b, " %14.1f", Summarize(only(results, m)).Mean)
	}
	b.WriteByte('\n')
}

func short(m string) string {
	if r := []rune(m); len(r) > 14 {
		return string(r[:13]) + "…"
	}
	return m
}

func modelsOf(rs []CaseResult) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		if !seen[r.Model] {
			seen[r.Model] = true
			out = append(out, r.Model)
		}
	}
	return out
}

func only(rs []CaseResult, model string) []CaseResult {
	var out []CaseResult
	for _, r := range rs {
		if r.Model == model {
			out = append(out, r)
		}
	}
	return out
}
