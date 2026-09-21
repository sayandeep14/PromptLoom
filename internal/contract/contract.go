// Package contract validates rendered output against a prompt's declared contract.
package contract

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
)

// Failure is a single contract violation.
type Failure struct {
	Kind   string // missing-section, forbidden-section, missing-content, forbidden-content
	Detail string
}

// Check validates outputText against a ContractBlock.
// Returns nil if no contract is declared.
func Check(c *ast.ContractBlock, outputText string) []Failure {
	if c == nil {
		return nil
	}
	var failures []Failure
	lower := strings.ToLower(outputText)

	for _, sec := range c.RequiredSections {
		if !hasHeading(outputText, sec) {
			failures = append(failures, Failure{
				Kind:   "missing-section",
				Detail: fmt.Sprintf("required section %q not found in output", sec),
			})
		}
	}

	for _, sec := range c.ForbiddenSections {
		if hasHeading(outputText, sec) {
			failures = append(failures, Failure{
				Kind:   "forbidden-section",
				Detail: fmt.Sprintf("forbidden section %q found in output", sec),
			})
		}
	}

	for _, phrase := range c.MustInclude {
		if !strings.Contains(lower, strings.ToLower(phrase)) {
			failures = append(failures, Failure{
				Kind:   "missing-content",
				Detail: fmt.Sprintf("required content %q not found in output", phrase),
			})
		}
	}

	for _, phrase := range c.MustNotInclude {
		if strings.Contains(lower, strings.ToLower(phrase)) {
			failures = append(failures, Failure{
				Kind:   "forbidden-content",
				Detail: fmt.Sprintf("forbidden content %q found in output", phrase),
			})
		}
	}

	return failures
}

// hasHeading reports whether text contains a Markdown heading (any level, # to ######)
// whose title is exactly name, ignoring case, surrounding spaces and a trailing colon.
// It is line-anchored on purpose: "## Summary of findings" or an inline mention of
// "## summary" inside a sentence must not satisfy a required "Summary" section.
func hasHeading(text, name string) bool {
	re := regexp.MustCompile(`(?im)^[ \t]{0,3}#{1,6}[ \t]+` + regexp.QuoteMeta(strings.TrimSpace(name)) + `[ \t]*:?[ \t]*#*[ \t]*\r?$`)
	return re.MatchString(text)
}
