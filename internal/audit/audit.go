// Package audit scans resolved prompt fields for dangerous patterns.
package audit

import (
	"regexp"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

// RiskLevel classifies the severity of a finding.
type RiskLevel int

const (
	Low RiskLevel = iota
	Medium
	High
)

func (r RiskLevel) String() string {
	switch r {
	case High:
		return "HIGH"
	case Medium:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// Finding is one flagged item from the audit scan.
type Finding struct {
	Risk   RiskLevel
	Field  string // e.g. "instructions", "constraints"
	Value  string // the offending text
	Reason string // human-readable explanation
	Fix    string // suggested remediation
}

type pattern struct {
	phrases []string
	risk    RiskLevel
	reason  string
	fix     string
	// negation: if any of these phrases also appear nearby, skip the match
	negation []string
	res      []*regexp.Regexp // compiled from phrases, see init
}

// prohibitions turn an instruction into its opposite ("never skip tests" is fine).
var prohibitions = []string{"never", "do not", "don't", "must not", "should not", "not allowed"}

func init() {
	for i := range patterns {
		p := &patterns[i]
		for _, ph := range p.phrases {
			p.res = append(p.res, phraseRegexp(ph))
		}
		// Telling the model NOT to do something risky is not itself a risk.
		p.negation = append(p.negation, prohibitions...)
	}
}

// phraseRegexp matches phrase case-insensitively, requiring a word boundary on any
// edge that is a word character, so "ssn" no longer matches inside "className".
func phraseRegexp(phrase string) *regexp.Regexp {
	isWord := func(b byte) bool {
		return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
	}
	q := regexp.QuoteMeta(strings.ToLower(phrase))
	if isWord(phrase[0]) {
		q = `\b` + q
	}
	if isWord(phrase[len(phrase)-1]) {
		q += `\b`
	}
	return regexp.MustCompile(q)
}

var patterns = []pattern{
	// HIGH — hardcoded secret/credential references
	{
		phrases: []string{".env", "credentials", "api_key=", "api_secret=", "private_key=", "access_token="},
		risk:    High,
		reason:  "hardcoded secret source reference",
		fix:     "Replace with a secret slot — `slot api_key { secret: true }`",
	},
	// HIGH — safety/policy bypass
	{
		phrases: []string{"ignore policy", "bypass validation", "skip tests", "ignore all instructions", "disregard previous"},
		risk:    High,
		reason:  "safety or policy bypass instruction",
		fix:     "Remove the bypass instruction; use conditional logic instead",
	},
	// HIGH — destructive commands without confirmation
	{
		phrases:  []string{"rm -rf", "drop table", "drop database", "delete all", "truncate table", "format disk"},
		risk:     High,
		reason:   "destructive command without confirmation qualifier",
		fix:      `Add "only after user confirms" or "with explicit confirmation" qualifier`,
		negation: []string{"confirmation", "confirm", "after user"},
	},
	// HIGH — production environment references
	{
		phrases: []string{"use production", "in production database", "production credentials", "prod api key"},
		risk:    High,
		reason:  "direct production environment reference",
		fix:     "Use environment separation: `env prod { ... }` with a secret slot",
	},
	// MEDIUM — PII without privacy qualifier
	{
		phrases:  []string{"social security", "ssn", "date of birth", "credit card number", "passport number"},
		risk:     Medium,
		reason:   "PII field reference without privacy qualifier",
		fix:      `Add "do not log", "redact", or "mask" qualifier near PII references`,
		negation: []string{"redact", "mask", "anonymi", "do not log", "never log"},
	},
	// MEDIUM — removes confirmation gate
	{
		phrases: []string{"without confirmation", "without asking", "no approval needed", "skip confirmation"},
		risk:    Medium,
		reason:  "removes user confirmation gate",
		fix:     `Add "after user confirms" qualifier`,
	},
	// LOW — urgency without safety qualifier
	{
		phrases:  []string{"as fast as possible", "without delay", "immediately execute", "run right away"},
		risk:     Low,
		reason:   "urgency instruction without safety qualifier",
		fix:      `Consider adding "safely" or "after validation" qualifier`,
		negation: []string{"safely", "after validation", "if safe"},
	},
}

// reviewVerbs start a clause that asks the model to find or report something, as opposed to
// do it.
var reviewVerbs = []string{
	"flag", "detect", "identify", "report", "look for", "check for", "scan for", "watch for",
	"warn about", "warn of", "point out", "highlight", "find", "catch", "call out", "search for",
}

// isReviewDirective reports whether the clause of text that contains the match at pos (clauses
// end at . ; ! ? or a line break) opens with, or has before the match, a review verb.
func isReviewDirective(text string, pos int) bool {
	start := strings.LastIndexAny(text[:pos], ".;!?\n") + 1
	before := text[start:pos]
	for _, v := range reviewVerbs {
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(v) + `\b`)
		if re.MatchString(before) {
			return true
		}
	}
	return false
}

// Audit scans all resolved text fields of rp and returns a list of findings.
func Audit(rp *ast.ResolvedPrompt) []Finding {
	var findings []Finding

	// Collect all text items with their field name.
	type item struct {
		field string
		value string
	}
	var items []item

	addScalar := func(name, val string) {
		if val != "" {
			items = append(items, item{name, val})
		}
	}
	addList := func(name string, vals []string) {
		for _, v := range vals {
			if v != "" {
				items = append(items, item{name, v})
			}
		}
	}

	addScalar("summary", rp.Summary)
	addScalar("persona", rp.Persona)
	addScalar("context", rp.Context)
	addScalar("objective", rp.Objective)
	addScalar("notes", rp.Notes)
	addList("instructions", rp.Instructions)
	addList("constraints", rp.Constraints)
	addList("examples", rp.Examples)
	addList("format", rp.Format)

	for _, it := range items {
		lower := strings.ToLower(it.value)
		for _, pat := range patterns {
			for _, re := range pat.res {
				loc := re.FindStringIndex(lower)
				if loc == nil {
					continue
				}
				// "Flag hardcoded credentials" tells a reviewer what to look for; it does not
				// hand out credentials.
				if isReviewDirective(lower, loc[0]) {
					continue
				}
				// Check negation words — if any appear in the same text, skip.
				negated := false
				for _, neg := range pat.negation {
					if strings.Contains(lower, neg) {
						negated = true
						break
					}
				}
				if negated {
					continue
				}
				findings = append(findings, Finding{
					Risk:   pat.risk,
					Field:  it.field,
					Value:  it.value,
					Reason: pat.reason,
					Fix:    pat.fix,
				})
				break // one match per pattern per item is enough
			}
		}
	}

	return findings
}

// MaxRisk returns the highest risk level across a list of findings.
func MaxRisk(findings []Finding) RiskLevel {
	max := Low
	for _, f := range findings {
		if f.Risk > max {
			max = f.Risk
		}
	}
	return max
}

// HasHigh reports whether any findings are High risk.
func HasHigh(findings []Finding) bool {
	for _, f := range findings {
		if f.Risk == High {
			return true
		}
	}
	return false
}

// HasMedium reports whether any findings are Medium risk.
func HasMedium(findings []Finding) bool {
	for _, f := range findings {
		if f.Risk == Medium {
			return true
		}
	}
	return false
}
