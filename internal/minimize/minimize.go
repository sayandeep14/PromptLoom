// Package minimize detects redundant content across a resolved prompt library:
// exact duplicates, near-duplicates (Levenshtein), and contradictory constraints.
package minimize

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

// IssueKind classifies a minimization finding.
type IssueKind string

const (
	KindExactDuplicate    IssueKind = "exact-duplicate"
	KindNearDuplicate     IssueKind = "near-duplicate"
	KindContradiction     IssueKind = "contradiction"
)

// Issue is a single finding from the minimizer.
type Issue struct {
	Kind    IssueKind
	Field   string
	ItemA   string
	ItemB   string
	Prompt  string // prompt name where the issue was found
	// Similarity is set for near-duplicates (0–1, where 1 = identical).
	Similarity float64
}

func (i Issue) String() string {
	switch i.Kind {
	case KindExactDuplicate:
		return fmt.Sprintf("[%s] %s.%s: exact duplicate item %q", i.Kind, i.Prompt, i.Field, i.ItemA)
	case KindNearDuplicate:
		return fmt.Sprintf("[%s] %s.%s: %.0f%% similar: %q  vs  %q",
			i.Kind, i.Prompt, i.Field, i.Similarity*100, i.ItemA, i.ItemB)
	case KindContradiction:
		return fmt.Sprintf("[%s] %s.%s: contradictory items: %q  vs  %q",
			i.Kind, i.Prompt, i.Field, i.ItemA, i.ItemB)
	}
	return ""
}

// Options controls the minimizer thresholds.
type Options struct {
	// NearDupThreshold is the minimum similarity ratio (0–1) to flag as near-duplicate.
	// Default 0.85 if zero.
	NearDupThreshold float64
}

func (o Options) threshold() float64 {
	if o.NearDupThreshold <= 0 {
		return 0.85
	}
	return o.NearDupThreshold
}

// Analyze runs all minimization checks on a single resolved prompt.
func Analyze(rp *ast.ResolvedPrompt, opts Options) []Issue {
	var issues []Issue
	thresh := opts.threshold()

	for _, field := range []string{"instructions", "constraints", "examples", "format"} {
		items := fieldItems(rp, field)
		issues = append(issues, checkList(rp.Name, field, items, thresh)...)
	}
	return issues
}

// AnalyzeAll runs minimization across all resolved prompts in a slice.
func AnalyzeAll(prompts []*ast.ResolvedPrompt, opts Options) []Issue {
	var all []Issue
	for _, rp := range prompts {
		all = append(all, Analyze(rp, opts)...)
	}
	return all
}

// Apply removes exact duplicates and near-duplicates from list fields in-place.
// Contradictions are flagged but NOT auto-removed — too risky.
// Returns the number of items removed.
func Apply(rp *ast.ResolvedPrompt, opts Options) int {
	removed := 0
	thresh := opts.threshold()
	for _, field := range []string{"instructions", "constraints", "examples", "format"} {
		items := fieldItems(rp, field)
		cleaned, n := deduplicate(items, thresh)
		removed += n
		setFieldItems(rp, field, cleaned)
	}
	return removed
}

// ---- helpers ----

func fieldItems(rp *ast.ResolvedPrompt, field string) []string {
	switch field {
	case "instructions":
		return rp.Instructions
	case "constraints":
		return rp.Constraints
	case "examples":
		return rp.Examples
	case "format":
		return rp.Format
	}
	return nil
}

func setFieldItems(rp *ast.ResolvedPrompt, field string, val []string) {
	switch field {
	case "instructions":
		rp.Instructions = val
	case "constraints":
		rp.Constraints = val
	case "examples":
		rp.Examples = val
	case "format":
		rp.Format = val
	}
}

func checkList(prompt, field string, items []string, thresh float64) []Issue {
	var issues []Issue
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			a, b := normalise(items[i]), normalise(items[j])
			if a == "" || b == "" {
				continue
			}
			if a == b {
				issues = append(issues, Issue{
					Kind:   KindExactDuplicate,
					Field:  field,
					Prompt: prompt,
					ItemA:  items[i],
				})
				continue
			}
			sim := similarity(a, b)
			if sim >= thresh {
				issues = append(issues, Issue{
					Kind:       KindNearDuplicate,
					Field:      field,
					Prompt:     prompt,
					ItemA:      items[i],
					ItemB:      items[j],
					Similarity: sim,
				})
				continue
			}
			// Contradiction: one item negates the other.
			if isContradiction(a, b) {
				issues = append(issues, Issue{
					Kind:   KindContradiction,
					Field:  field,
					Prompt: prompt,
					ItemA:  items[i],
					ItemB:  items[j],
				})
			}
		}
	}
	return issues
}

// deduplicate removes exact and near-duplicates, keeping the first occurrence.
func deduplicate(items []string, thresh float64) ([]string, int) {
	var kept []string
	removed := 0
	for _, item := range items {
		norm := normalise(item)
		isDup := false
		for _, k := range kept {
			kn := normalise(k)
			if kn == norm {
				isDup = true
				break
			}
			if norm != "" && kn != "" && similarity(norm, kn) >= thresh {
				isDup = true
				break
			}
		}
		if isDup {
			removed++
		} else {
			kept = append(kept, item)
		}
	}
	return kept, removed
}

// normalise lowercases and strips punctuation for comparison.
func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "- ")
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// similarity returns a value 0–1 using normalised Levenshtein distance.
func similarity(a, b string) float64 {
	maxLen := len([]rune(a))
	if lb := len([]rune(b)); lb > maxLen {
		maxLen = lb
	}
	if maxLen == 0 {
		return 1
	}
	dist := levenshtein([]rune(a), []rune(b))
	return 1 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b []rune) int {
	la, lb := len(a), len(b)
	dp := make([][]int, la+1)
	for i := range dp {
		dp[i] = make([]int, lb+1)
		dp[i][0] = i
	}
	for j := 0; j <= lb; j++ {
		dp[0][j] = j
	}
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1]
			} else {
				dp[i][j] = 1 + min3(dp[i-1][j], dp[i][j-1], dp[i-1][j-1])
			}
		}
	}
	return dp[la][lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// isContradiction detects simple negation patterns.
// E.g. "always do X" vs "never do X" / "do not X" vs "do X".
func isContradiction(a, b string) bool {
	negPrefixes := []string{"never ", "do not ", "don't ", "avoid ", "never use ", "not "}
	for _, neg := range negPrefixes {
		if strings.HasPrefix(a, neg) {
			core := strings.TrimPrefix(a, neg)
			if strings.HasPrefix(b, "always ") && strings.Contains(b, core) {
				return true
			}
			if strings.EqualFold(b, core) {
				return true
			}
		}
		if strings.HasPrefix(b, neg) {
			core := strings.TrimPrefix(b, neg)
			if strings.HasPrefix(a, "always ") && strings.Contains(a, core) {
				return true
			}
			if strings.EqualFold(a, core) {
				return true
			}
		}
	}
	return false
}
