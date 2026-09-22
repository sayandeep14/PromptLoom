package bench

import (
	"fmt"
	"strings"
)

// Text renders a bench run as a table: one row per model.
func (o *Outcome) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-28s %6s %8s %10s %10s %10s\n", "model", "runs", "errors", "avg ms", "avg in/out", "cost")
	for _, m := range o.Models {
		cost := "—"
		if total, ok := m.TotalCost(); ok {
			cost = formatCost(total)
		} else if m.Errors() < len(m.Calls) {
			cost = "n/a"
		}
		fmt.Fprintf(&b, "%-28s %6d %8d %10d %10s %10s\n",
			m.Label, len(m.Calls), m.Errors(), m.AvgDuration().Milliseconds(),
			fmt.Sprintf("%.0f/%.0f", m.AvgInput(), m.AvgOutput()), cost)
	}
	for _, m := range o.Models {
		for i, c := range m.Calls {
			if c.Err != nil {
				fmt.Fprintf(&b, "  ✗ %s run %d: %v\n", m.Label, i+1, c.Err)
			}
		}
	}
	return b.String()
}

func formatCost(cost float64) string {
	if cost < 0.01 && cost > 0 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
