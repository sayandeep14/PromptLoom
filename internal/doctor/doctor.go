// Package doctor provides prompt library health checks and smell detection.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/registry"
	"github.com/sayandeepgiri/promptloom/internal/resolve"
	"github.com/sayandeepgiri/promptloom/internal/tokens"
)

// StructuralResult is a single structural check outcome.
type StructuralResult struct {
	Label  string
	Pass   bool   // true = OK
	IsWarn bool   // if !Pass, true = warning, false = error
	Detail string // optional extra info
}

// Smell is a heuristic quality signal for a prompt.
type Smell struct {
	Name   string
	Detail string
}

// HealthReport is the full doctor result for one prompt.
type HealthReport struct {
	Name        string
	Score       int
	Band        string
	Structurals []StructuralResult
	Smells      []Smell
}

// CheckPrompt runs all doctor checks for a single named prompt.
func CheckPrompt(name string, reg *registry.Registry, cfg *config.Config, cwd string) (*HealthReport, error) {
	node, ok := reg.LookupPrompt(name)
	if !ok {
		return nil, fmt.Errorf("prompt %q not found", name)
	}

	rp, err := resolve.Resolve(name, reg)
	if err != nil {
		return nil, err
	}

	report := &HealthReport{Name: name}
	report.Structurals = structuralChecks(node, rp, reg, cfg, cwd)
	report.Smells = detectSmells(node, rp, reg, cfg)
	report.Score = computeScore(report)
	report.Band = scoreBand(report.Score)
	return report, nil
}

// CheckAll runs doctor for every prompt in the registry.
func CheckAll(reg *registry.Registry, cfg *config.Config, cwd string) ([]*HealthReport, error) {
	prompts := reg.Prompts()
	sort.Slice(prompts, func(i, j int) bool { return prompts[i].Name < prompts[j].Name })

	var reports []*HealthReport
	for _, p := range prompts {
		r, err := CheckPrompt(p.Name, reg, cfg, cwd)
		if err != nil {
			continue
		}
		reports = append(reports, r)
	}
	return reports, nil
}

// UnusedBlocks returns block names that no prompt references.
func UnusedBlocks(reg *registry.Registry) []string {
	used := map[string]bool{}
	for _, p := range reg.Prompts() {
		for _, b := range p.Uses {
			used[b] = true
		}
	}
	var unused []string
	for _, b := range reg.Blocks() {
		if !used[b.Name] {
			unused = append(unused, b.Name)
		}
	}
	sort.Strings(unused)
	return unused
}

// ---- structural checks ----

func structuralChecks(node *ast.Node, rp *ast.ResolvedPrompt, reg *registry.Registry, cfg *config.Config, cwd string) []StructuralResult {
	var checks []StructuralResult

	checks = append(checks, StructuralResult{Label: "Parses cleanly", Pass: true})

	if node.Parent != "" {
		if _, ok := reg.LookupPrompt(node.Parent); ok {
			checks = append(checks, StructuralResult{Label: "Parent resolves", Pass: true})
		} else {
			checks = append(checks, StructuralResult{
				Label:  "Parent resolves",
				Detail: fmt.Sprintf("parent %q not found", node.Parent),
			})
		}
	}

	if len(node.Uses) > 0 {
		var missing []string
		for _, use := range node.Uses {
			if _, ok := reg.LookupBlock(use); !ok {
				missing = append(missing, use)
			}
		}
		if len(missing) == 0 {
			checks = append(checks, StructuralResult{Label: "All blocks resolve", Pass: true})
		} else {
			checks = append(checks, StructuralResult{
				Label:  "All blocks resolve",
				Detail: fmt.Sprintf("missing: %s", strings.Join(missing, ", ")),
			})
		}
	}

	checks = append(checks, distFreshnessCheck(node, cfg, cwd))

	if cfg.Validation.TokenLimitWarn > 0 {
		checks = append(checks, tokenLimitCheck(rp, cfg))
	}

	if cfg.Validation.RequireContract {
		if node.Contract == nil {
			checks = append(checks, StructuralResult{
				Label:   "Contract declared",
				IsWarn:  true,
				Detail:  "add a contract block (run: loom contract --help)",
			})
		} else {
			checks = append(checks, StructuralResult{Label: "Contract declared", Pass: true})
		}
	}

	return checks
}

func distFreshnessCheck(node *ast.Node, cfg *config.Config, cwd string) StructuralResult {
	distPath := filepath.Join(cwd, cfg.Paths.Out, node.Name+".md")
	distStat, distErr := os.Stat(distPath)
	if distErr != nil {
		return StructuralResult{
			Label:  "Dist file exists",
			IsWarn: true,
			Detail: "run loom weave to generate",
		}
	}

	promptStat, promptErr := os.Stat(node.Pos.File)
	if promptErr == nil && distStat.ModTime().Before(promptStat.ModTime()) {
		return StructuralResult{
			Label:  "Dist file fresh",
			IsWarn: true,
			Detail: fmt.Sprintf("%s is stale (run loom weave)", filepath.Base(distPath)),
		}
	}

	age := time.Since(distStat.ModTime())
	days := int(age.Hours() / 24)
	detail := ""
	if days > 7 {
		detail = fmt.Sprintf("%d days old", days)
	}
	return StructuralResult{Label: "Dist file fresh", Pass: true, Detail: detail}
}

func tokenLimitCheck(rp *ast.ResolvedPrompt, cfg *config.Config) StructuralResult {
	total := estimatePromptTokens(rp)
	label := fmt.Sprintf("Token limit (%d)", cfg.Validation.TokenLimitWarn)
	detail := fmt.Sprintf("%d tokens", total)
	if total > cfg.Validation.TokenLimitWarn {
		return StructuralResult{
			Label:  label,
			IsWarn: true,
			Detail: fmt.Sprintf("%d tokens (exceeds limit of %d)", total, cfg.Validation.TokenLimitWarn),
		}
	}
	return StructuralResult{Label: label, Pass: true, Detail: detail}
}

// ---- smell detectors ----

func detectSmells(node *ast.Node, rp *ast.ResolvedPrompt, reg *registry.Registry, cfg *config.Config) []Smell {
	var smells []Smell

	limit := cfg.Validation.SmellConstraintLimit
	if limit == 0 {
		limit = 25
	}

	if len(rp.Constraints) > limit {
		smells = append(smells, Smell{
			Name:   "Constraint Pile-Up",
			Detail: fmt.Sprintf("%d constraints (limit: %d) — consider splitting into blocks", len(rp.Constraints), limit),
		})
	}

	if sentences := countSentences(rp.Objective); sentences > 5 {
		smells = append(smells, Smell{
			Name:   "God Prompt",
			Detail: fmt.Sprintf("objective has %d sentences — consider splitting into focused child prompts", sentences),
		})
	}

	if len(rp.Format) == 0 {
		smells = append(smells, Smell{
			Name:   "Output Ambiguity",
			Detail: "no output format declared — add a format field to set expectations",
		})
	}

	if isPersonaSoup(rp.Persona) {
		smells = append(smells, Smell{
			Name:   "Persona Soup",
			Detail: "persona describes multiple potentially conflicting roles",
		})
	}

	allItems := append(append([]string{}, rp.Instructions...), rp.Constraints...)
	for _, pair := range findDuplicates(allItems) {
		smells = append(smells, Smell{
			Name:   "Duplicate Instructions",
			Detail: fmt.Sprintf("%q and %q are %.0f%% similar", pair.a, pair.b, pair.sim*100),
		})
	}

	for _, conflict := range findConflicts(allItems) {
		smells = append(smells, Smell{
			Name:   "Conflicting Instructions",
			Detail: conflict,
		})
	}

	if node.Parent != "" {
		if parentRP, err := resolve.Resolve(node.Parent, reg); err == nil {
			if len(parentRP.Format) > 0 && len(rp.Format) > 0 && !sameFormatSignature(parentRP.Format, rp.Format) {
				smells = append(smells, Smell{
					Name:   "Format Drift",
					Detail: "child prompt format differs from parent — check for unintended divergence",
				})
			}
		}
	}

	return smells
}

// ---- scoring ----

func computeScore(r *HealthReport) int {
	score := 100
	for _, c := range r.Structurals {
		if !c.Pass {
			if c.IsWarn {
				score -= 5
			} else {
				score -= 15
			}
		}
	}
	for _, s := range r.Smells {
		switch s.Name {
		case "God Prompt", "Conflicting Instructions":
			score -= 10
		default:
			score -= 5
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

func scoreBand(score int) string {
	switch {
	case score >= 90:
		return "Excellent"
	case score >= 75:
		return "Good"
	case score >= 60:
		return "Needs improvement"
	case score >= 40:
		return "Risky"
	default:
		return "Poor"
	}
}

// ---- helpers ----

func estimatePromptTokens(rp *ast.ResolvedPrompt) int {
	scalars := []string{rp.Summary, rp.Persona, rp.Context, rp.Objective, rp.Notes}
	lists := [][]string{rp.Instructions, rp.Constraints, rp.Examples, rp.Format}
	total := 0
	for _, s := range scalars {
		total += tokens.Estimate(s)
	}
	for _, list := range lists {
		for _, item := range list {
			total += tokens.Estimate(item)
		}
	}
	return total
}

func countSentences(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	n := 0
	for _, ch := range text {
		if ch == '.' || ch == '!' || ch == '?' {
			n++
		}
	}
	if n == 0 {
		return 1
	}
	return n
}

func isPersonaSoup(persona string) bool {
	lower := strings.ToLower(persona)
	markers := []string{
		" and you are also ",
		" and also act as ",
		" but also a ",
		" while also being a ",
		" as well as a ",
	}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

type similarPair struct {
	a, b string
	sim  float64
}

func findDuplicates(items []string) []similarPair {
	var pairs []similarPair
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if sim := jaccardSimilarity(items[i], items[j]); sim >= 0.80 {
				pairs = append(pairs, similarPair{a: truncate(items[i], 50), b: truncate(items[j], 50), sim: sim})
			}
		}
	}
	return pairs
}

func jaccardSimilarity(a, b string) float64 {
	setA := wordSet(a)
	setB := wordSet(b)
	intersection := 0
	for w := range setA {
		if setB[w] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

func wordSet(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?\"'()")
		if w != "" {
			m[w] = true
		}
	}
	return m
}

var conflictPairs = [][2]string{
	{"concise", "explain everything"},
	{"concise", "explain every detail"},
	{"be brief", "comprehensive"},
	{"be brief", "thorough"},
	{"keep it short", "detailed"},
	{"minimal", "exhaustive"},
	{"never explain", "explain"},
	{"do not explain", "explain why"},
}

func findConflicts(items []string) []string {
	lower := make([]string, len(items))
	for i, item := range items {
		lower[i] = strings.ToLower(item)
	}

	var found []string
	for _, pair := range conflictPairs {
		var itemA, itemB string
		for i, l := range lower {
			if strings.Contains(l, pair[0]) {
				itemA = items[i]
			}
			if strings.Contains(l, pair[1]) {
				itemB = items[i]
			}
		}
		if itemA != "" && itemB != "" {
			found = append(found, fmt.Sprintf("%q conflicts with %q", truncate(itemA, 40), truncate(itemB, 40)))
		}
	}
	return found
}

func sameFormatSignature(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !strings.EqualFold(strings.TrimSpace(a[i]), strings.TrimSpace(b[i])) {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
