package usage

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Filter narrows ReadAll's result before summarizing. A zero field matches everything.
type Filter struct {
	Since   time.Time
	Command string
	Model   string // matches Provider:Model or bare Model
}

// Apply returns the records in recs that match f, in the same order.
func (f Filter) Apply(recs []Record) []Record {
	var out []Record
	for _, r := range recs {
		if !f.Since.IsZero() && r.Time.Before(f.Since) {
			continue
		}
		if f.Command != "" && !strings.EqualFold(f.Command, r.Command) {
			continue
		}
		if f.Model != "" && !strings.EqualFold(f.Model, r.Model) && !strings.EqualFold(f.Model, r.Provider+":"+r.Model) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// group is one row of a Summary's breakdown: a command or a model, with its totals.
type group struct {
	Key             string
	Calls           int
	Input, Output   int
	CostUSD         float64
	UnknownPriceFor map[string]bool // model names seen under this key with no pricing entry
}

// Summary aggregates a set of Records: overall totals, and a breakdown by command and by model.
type Summary struct {
	Calls           int
	Input, Output   int
	CostUSD         float64
	AnyCost         bool // at least one record had a known price
	ByCommand       []group
	ByModel         []group
	UnknownPricedAt map[string]bool // every provider:model seen with no [[pricing]] entry
}

// Summarize aggregates recs.
func Summarize(recs []Record) Summary {
	var s Summary
	byCmd := map[string]*group{}
	byModel := map[string]*group{}
	s.UnknownPricedAt = map[string]bool{}
	var cmdOrder, modelOrder []string

	add := func(m map[string]*group, order *[]string, key string, r Record) {
		g, ok := m[key]
		if !ok {
			g = &group{Key: key}
			m[key] = g
			*order = append(*order, key)
		}
		g.Calls++
		g.Input += r.Input
		g.Output += r.Output
		if r.HasCost {
			g.CostUSD += r.CostUSD
		}
	}

	for _, r := range recs {
		s.Calls++
		s.Input += r.Input
		s.Output += r.Output
		if r.HasCost {
			s.CostUSD += r.CostUSD
			s.AnyCost = true
		} else {
			s.UnknownPricedAt[r.Provider+":"+r.Model] = true
		}
		add(byCmd, &cmdOrder, r.Command, r)
		add(byModel, &modelOrder, r.Provider+":"+r.Model, r)
	}
	sort.Strings(cmdOrder)
	sort.Strings(modelOrder)
	for _, k := range cmdOrder {
		s.ByCommand = append(s.ByCommand, *byCmd[k])
	}
	for _, k := range modelOrder {
		s.ByModel = append(s.ByModel, *byModel[k])
	}
	return s
}

// Text renders a Summary for a terminal.
func (s Summary) Text() string {
	if s.Calls == 0 {
		return "no usage recorded yet\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d call(s), %d input / %d output tokens", s.Calls, s.Input, s.Output)
	if s.AnyCost {
		fmt.Fprintf(&b, ", %s\n", formatCost(s.CostUSD))
	} else {
		fmt.Fprint(&b, "\n")
	}

	fmt.Fprint(&b, "\nby command:\n")
	for _, g := range s.ByCommand {
		fmt.Fprintf(&b, "  %-14s %5d call(s)  %6d in / %6d out%s\n", g.Key, g.Calls, g.Input, g.Output, costSuffix(g.CostUSD, s.AnyCost))
	}
	fmt.Fprint(&b, "\nby model:\n")
	for _, g := range s.ByModel {
		fmt.Fprintf(&b, "  %-28s %5d call(s)  %6d in / %6d out%s\n", g.Key, g.Calls, g.Input, g.Output, costSuffix(g.CostUSD, s.AnyCost))
	}
	if len(s.UnknownPricedAt) > 0 {
		var models []string
		for m := range s.UnknownPricedAt {
			models = append(models, m)
		}
		sort.Strings(models)
		fmt.Fprintf(&b, "\nno price on file for: %s (add [[pricing]] to loom.toml to see an estimated cost)\n", strings.Join(models, ", "))
	}
	return b.String()
}

func costSuffix(cost float64, anyCost bool) string {
	if !anyCost {
		return ""
	}
	return "  " + formatCost(cost)
}

func formatCost(cost float64) string {
	if cost < 0.01 && cost > 0 {
		return fmt.Sprintf("$%.4f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
