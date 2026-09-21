package contract

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func kinds(fs []Failure) string {
	var k []string
	for _, f := range fs {
		k = append(k, f.Kind)
	}
	return strings.Join(k, ",")
}

func TestNilContractAlwaysPasses(t *testing.T) {
	if got := Check(nil, "anything"); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestRequiredSections(t *testing.T) {
	c := &ast.ContractBlock{RequiredSections: []string{"Summary", "Key Points"}}

	ok := "## Summary\nx\n\n## Key Points\n- a\n"
	if f := Check(c, ok); len(f) != 0 {
		t.Errorf("valid output rejected: %v", f)
	}
	if got := kinds(Check(c, "## Summary\nx\n")); got != "missing-section" {
		t.Errorf("missing Key Points: got %q", got)
	}
	if got := kinds(Check(c, "no headings at all")); got != "missing-section,missing-section" {
		t.Errorf("no headings: got %q", got)
	}
}

func TestHeadingMatchingIsExactAndLineAnchored(t *testing.T) {
	c := &ast.ContractBlock{RequiredSections: []string{"Summary"}}

	pass := []string{
		"## Summary",
		"## summary",                      // case-insensitive
		"### Summary",                     // any heading level
		"# Summary\n",                     // level 1
		"## Summary:",                     // trailing colon
		"##  Summary  ",                   // spacing
		"intro\n\n## Summary\r\nbody\r\n", // CRLF
		"  ## Summary",                    // up to 3 spaces of indent (CommonMark)
	}
	for _, out := range pass {
		if f := Check(c, out); len(f) != 0 {
			t.Errorf("%q should satisfy the contract: %v", out, f)
		}
	}

	fail := []string{
		"## Summary of findings",           // prefix must not match
		"## Executive Summary",             // suffix must not match
		"See the ## Summary section below", // not a heading line
		"Summary",                          // no heading marker
		"####### Summary",                  // 7 hashes is not a heading
		"    ## Summary",                   // 4 spaces = code block
	}
	for _, out := range fail {
		if f := Check(c, out); len(f) == 0 {
			t.Errorf("%q must NOT satisfy a required Summary section", out)
		}
	}
}

func TestSectionNamesWithRegexCharacters(t *testing.T) {
	c := &ast.ContractBlock{RequiredSections: []string{"Q&A (draft) [1]"}}
	if f := Check(c, "## Q&A (draft) [1]\n"); len(f) != 0 {
		t.Errorf("regex metacharacters must be treated literally: %v", f)
	}
	if f := Check(c, "## Q&A draft 1\n"); len(f) == 0 {
		t.Error("a different title must not match")
	}
}

func TestForbiddenSections(t *testing.T) {
	c := &ast.ContractBlock{ForbiddenSections: []string{"Disclaimer"}}
	if f := Check(c, "## Answer\nx"); len(f) != 0 {
		t.Errorf("clean output flagged: %v", f)
	}
	if got := kinds(Check(c, "## Answer\n\n### Disclaimer\nx")); got != "forbidden-section" {
		t.Errorf("got %q", got)
	}
	if f := Check(c, "## Disclaimers and more"); len(f) != 0 {
		t.Errorf("a longer title is not the forbidden section: %v", f)
	}
}

func TestMustIncludeAndMustNotInclude(t *testing.T) {
	c := &ast.ContractBlock{
		MustInclude:    []string{"Verdict"},
		MustNotInclude: []string{"As an AI", "LGTM"},
	}
	if f := Check(c, "The VERDICT is: fix it."); len(f) != 0 {
		t.Errorf("case-insensitive include failed: %v", f)
	}
	if got := kinds(Check(c, "Nothing here")); got != "missing-content" {
		t.Errorf("got %q", got)
	}
	if got := kinds(Check(c, "Verdict: lgtm, as an ai I think")); got != "forbidden-content,forbidden-content" {
		t.Errorf("got %q", got)
	}
}

func TestFailureDetailsAreActionable(t *testing.T) {
	c := &ast.ContractBlock{RequiredSections: []string{"Summary"}, MustNotInclude: []string{"LGTM"}}
	f := Check(c, "LGTM")
	if len(f) != 2 || !strings.Contains(f[0].Detail, `"Summary"`) || !strings.Contains(f[1].Detail, `"LGTM"`) {
		t.Errorf("details should name what failed: %+v", f)
	}
}
