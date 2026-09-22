package optimize

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Text renders one Step for a terminal: the score, and if a candidate was produced, the diff.
func (r *StepResult) Text() string {
	var b strings.Builder
	b.WriteString(r.Before.Text())
	if !r.HasCandidate() {
		fmt.Fprintf(&b, "  %s\n", r.Message)
		return b.String()
	}
	fmt.Fprintf(&b, "\n  %s\n\n", filepath.ToSlash(r.File))
	for _, line := range strings.Split(strings.TrimRight(r.Diff, "\n"), "\n") {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	if r.After != nil {
		fmt.Fprintf(&b, "\n%s", r.After.Text())
	}
	return b.String()
}

// Text renders a whole Loop run.
func (r *LoopResult) Text() string {
	var b strings.Builder
	for i, step := range r.Steps {
		if len(r.Steps) > 1 {
			fmt.Fprintf(&b, "── iteration %d ──\n", i+1)
		}
		b.WriteString(step.Text())
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%s\n", r.Reason)
	if r.Final != nil {
		fmt.Fprintf(&b, "final: %s", r.Final.Text())
	}
	return b.String()
}
