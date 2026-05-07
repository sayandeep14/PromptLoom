// Package audit scans resolved prompt fields for dangerous patterns.
package audit

import (
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
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
	Risk    RiskLevel
	Field   string  // e.g. "instructions", "constraints"
	Value   string  // the offending text
	Reason  string  // human-readable explanation
	Fix     string  // suggested remediation
}

type pattern struct {
	phrases []string
	risk    RiskLevel
	reason  string
	fix     string
	// negation: if any of these phrases also appear nearby, skip the match
	negation []string
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
		phrases:  []string{"ignore policy", "bypass validation", "skip tests", "ignore all instructions", "disregard previous"},
		risk:     High,
		reason:   "safety or policy bypass instruction",
		fix:      "Remove the bypass instruction; use conditional logic instead",
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
		phrases:  []string{"use production", "in production database", "production credentials", "prod api key"},
		risk:     High,
		reason:   "direct production environment reference",
		fix:      "Use environment separation: `env prod { ... }` with a secret slot",
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
		phrases:  []string{"without confirmation", "without asking", "no approval needed", "skip confirmation"},
		risk:     Medium,
		reason:   "removes user confirmation gate",
		fix:      `Add "after user confirms" qualifier`,
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
			for _, phrase := range pat.phrases {
				if !strings.Contains(lower, phrase) {
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
