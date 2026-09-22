package lmscr

import (
	"fmt"
	"strings"
)

// Text renders a run for a terminal: one line per step, then a summary.
func (o *Outcome) Text() string {
	var b strings.Builder
	if o.Script.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", o.Script.Description)
	}
	ran, failed, skipped := 0, 0, 0
	for _, s := range o.Steps {
		switch {
		case s.Skipped:
			skipped++
			fmt.Fprintf(&b, "  ○ %s (skipped: %s)\n", s.Step.Name, s.Step.Effective())
		case s.Err != nil:
			failed++
			ran++
			fmt.Fprintf(&b, "  ✗ %s: %v\n", s.Step.Name, s.Err)
		case s.ExitCode != 0:
			ran++
			mark := "✗"
			if s.Step.ContinueOnFail {
				mark = "!"
			} else {
				failed++
			}
			fmt.Fprintf(&b, "  %s %s: exit %d (%s)\n", mark, s.Step.Name, s.ExitCode, s.Duration.Round(10_000_000))
		default:
			ran++
			fmt.Fprintf(&b, "  ✓ %s (%s)\n", s.Step.Name, s.Duration.Round(10_000_000))
		}
	}
	fmt.Fprintf(&b, "\n%d step(s) run, %d skipped", ran, skipped)
	if failed > 0 {
		fmt.Fprintf(&b, ", %d failed\n", failed)
	} else {
		fmt.Fprint(&b, "\n")
	}
	return b.String()
}
