// Package optimize proposes and applies targeted improvements to a prompt's field content, using
// its own eval suite (internal/eval) as the judge of whether an improvement helped. It never
// changes a prompt's structure (inheritance, blocks, vars, contract) — only field text — and
// never writes without the caller's explicit go-ahead. See docs/AGENT_RUNTIME.md, "Exception:
// loom optimize".
package optimize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/eval"
)

// Feedback is one failing case, reduced to what a refiner model needs: which criteria fell
// short and why, and any contract rule the answer broke.
type Feedback struct {
	Case             string
	Input            string
	Weak             []WeakCriterion
	ContractFailures []string
}

// WeakCriterion is one criterion that scored under its case's pass mark.
type WeakCriterion struct {
	Criterion string
	Score     int
	Note      string
}

// feedbackFrom reduces the eval results of one suite (already run against ONE model) to the
// feedback a refiner needs. Cases that errored (a model or judge failure, not a quality problem)
// are not feedback — they are reported separately by the caller.
func feedbackFrom(results []eval.CaseResult) []Feedback {
	var out []Feedback
	for _, r := range results {
		if r.Err != nil || r.Passed {
			continue
		}
		f := Feedback{Case: r.Case, Input: r.Input, ContractFailures: contractDetails(r)}
		for _, c := range r.Criteria {
			if c.Score < r.Threshold {
				f.Weak = append(f.Weak, WeakCriterion{Criterion: c.Criterion, Score: c.Score, Note: c.Note})
			}
		}
		out = append(out, f)
	}
	return out
}

func contractDetails(r eval.CaseResult) []string {
	var out []string
	for _, f := range r.ContractFailures {
		out = append(out, f.Detail)
	}
	return out
}

// refinerPrompt renders the feedback as text for the refiner's user message.
func refinerPrompt(current string, feedback []Feedback) string {
	var b strings.Builder
	b.WriteString("Here is the prompt declaration, exactly as it is written today:\n\n```\n")
	b.WriteString(strings.TrimRight(current, "\n"))
	b.WriteString("\n```\n\nOn the eval cases below, it did not score well enough. For each case: the input that was")
	b.WriteString(" sent, and which criteria fell short (with the grader's note).\n")
	names := make([]string, len(feedback))
	for i := range feedback {
		names[i] = feedback[i].Case
	}
	sort.Strings(names) // stable order regardless of how the caller collected feedback
	byName := map[string]Feedback{}
	for _, f := range feedback {
		byName[f.Case] = f
	}
	for i, name := range names {
		f := byName[name]
		fmt.Fprintf(&b, "\nCase %d — %q\ninput: %s\n", i+1, f.Case, clip(f.Input))
		for _, w := range f.Weak {
			fmt.Fprintf(&b, "  - %q scored %d/100: %s\n", w.Criterion, w.Score, orDash(w.Note))
		}
		for _, c := range f.ContractFailures {
			fmt.Fprintf(&b, "  - contract violated: %s\n", c)
		}
	}
	b.WriteString("\nRewrite the prompt declaration to address these problems. Keep its name, its `inherits`" +
		" list, its `use` lines, its `var`/`slot` declarations, its `variant`/`env` blocks and its `contract` " +
		"and `capabilities` blocks EXACTLY as they are — change only the text of its own fields " +
		"(persona, instructions, constraints, and so on). Output the complete, valid declaration and nothing else.")
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "(no note)"
	}
	return s
}

const maxFeedbackChars = 4000

func clip(s string) string {
	r := []rune(s)
	if len(r) <= maxFeedbackChars {
		return s
	}
	return string(r[:maxFeedbackChars]) + "…"
}
