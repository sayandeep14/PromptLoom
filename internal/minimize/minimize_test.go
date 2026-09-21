package minimize

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func kinds(is []Issue) string {
	var out []string
	for _, i := range is {
		out = append(out, string(i.Kind)+":"+i.Field)
	}
	return strings.Join(out, ",")
}

func TestExactDuplicatesIgnoreCasePunctuationAndBullets(t *testing.T) {
	rp := &ast.ResolvedPrompt{Name: "P", Constraints: []string{"Be concise.", "- be   CONCISE!", "Something else"}}
	is := Analyze(rp, Options{})
	if kinds(is) != "exact-duplicate:constraints" || is[0].ItemA != "Be concise." || is[0].Prompt != "P" {
		t.Errorf("%+v", is)
	}
}

func TestNearDuplicate(t *testing.T) {
	rp := &ast.ResolvedPrompt{Name: "P", Instructions: []string{"Explain the reasoning behind each change", "Explain the reasoning behind every change"}}
	is := Analyze(rp, Options{})
	if kinds(is) != "near-duplicate:instructions" || is[0].Similarity < 0.85 || is[0].Similarity >= 1 {
		t.Errorf("%+v", is)
	}
	// a stricter threshold lets them through
	if is := Analyze(rp, Options{NearDupThreshold: 0.99}); len(is) != 0 {
		t.Errorf("%+v", is)
	}
}

// Similar text is not the same statement when a number or a negation differs.
func TestSimilarButDifferentItemsAreNotDuplicates(t *testing.T) {
	pairs := [][2]string{
		{"Use Python 3 for all scripts", "Use Python 2 for all scripts"},
		{"Return at most 5 results", "Return at most 50 results"},
		{"Always cite the source document", "Never cite the source document"},
		{"You may use external tools freely", "You may not use external tools freely"},
	}
	for _, p := range pairs {
		rp := &ast.ResolvedPrompt{Name: "P", Instructions: []string{p[0], p[1]}}
		for _, is := range Analyze(rp, Options{}) {
			if is.Kind == KindNearDuplicate || is.Kind == KindExactDuplicate {
				t.Errorf("%q vs %q reported as %s", p[0], p[1], is.Kind)
			}
		}
		clone := &ast.ResolvedPrompt{Instructions: []string{p[0], p[1]}}
		if n := Apply(clone, Options{}); n != 0 || len(clone.Instructions) != 2 {
			t.Errorf("Apply must not delete %q / %q (removed %d)", p[0], p[1], n)
		}
	}
}

func TestContradictions(t *testing.T) {
	cases := [][2]string{
		{"Never use tables", "Use tables"},
		{"Always use tables", "Never use tables"},
		{"Do not guess", "Always guess when unsure"},
		{"Don't apologise", "Always apologise"}, // the apostrophe is stripped by normalisation
		{"Avoid jargon", "jargon"},
	}
	for _, c := range cases {
		rp := &ast.ResolvedPrompt{Name: "P", Constraints: []string{c[0], c[1]}}
		is := Analyze(rp, Options{})
		if len(is) != 1 || is[0].Kind != KindContradiction {
			t.Errorf("%q vs %q: %+v", c[0], c[1], is)
		}
		// flagged, never auto-removed
		clone := &ast.ResolvedPrompt{Constraints: []string{c[0], c[1]}}
		if Apply(clone, Options{}) != 0 || len(clone.Constraints) != 2 {
			t.Errorf("contradictions must not be removed: %v", clone.Constraints)
		}
	}
	// unrelated items are fine
	rp := &ast.ResolvedPrompt{Constraints: []string{"Never use tables", "Always answer in English"}}
	if is := Analyze(rp, Options{}); len(is) != 0 {
		t.Errorf("%+v", is)
	}
}

func TestApplyKeepsTheFirstOccurrenceAndCountsRemovals(t *testing.T) {
	rp := &ast.ResolvedPrompt{
		Instructions: []string{"Be brief.", "Say hello", "be brief", "Be  Brief!"},
		Constraints:  []string{"a constraint", "a constraint"},
		Examples:     []string{"one", "two"},
		Format:       []string{"JSON", "json"},
		Notes:        "untouched",
	}
	n := Apply(rp, Options{})
	if n != 4 {
		t.Errorf("removed %d", n)
	}
	if strings.Join(rp.Instructions, "|") != "Be brief.|Say hello" || len(rp.Constraints) != 1 || len(rp.Examples) != 2 || len(rp.Format) != 1 || rp.Format[0] != "JSON" {
		t.Errorf("%+v", rp)
	}
}

// Items that are only punctuation normalise to "", which used to make every one of them a
// duplicate of the others and delete them.
func TestPunctuationOnlyItemsAreNotEachOthersDuplicates(t *testing.T) {
	rp := &ast.ResolvedPrompt{Examples: []string{"---", "***", "---", "...", "real one"}}
	if n := Apply(rp, Options{}); n != 1 {
		t.Errorf("only the repeated '---' is a duplicate, removed %d: %v", n, rp.Examples)
	}
	if strings.Join(rp.Examples, "|") != "---|***|...|real one" {
		t.Errorf("%v", rp.Examples)
	}
	if is := Analyze(&ast.ResolvedPrompt{Examples: []string{"---", "***"}}, Options{}); len(is) != 0 {
		t.Errorf("%+v", is)
	}
}

func TestAnalyzeAllAndEmpty(t *testing.T) {
	a := &ast.ResolvedPrompt{Name: "A", Constraints: []string{"x y", "x y"}}
	b := &ast.ResolvedPrompt{Name: "B"}
	c := &ast.ResolvedPrompt{Name: "C", Format: []string{"same", "same"}}
	is := AnalyzeAll([]*ast.ResolvedPrompt{a, b, c}, Options{})
	if len(is) != 2 || is[0].Prompt != "A" || is[1].Prompt != "C" {
		t.Errorf("%+v", is)
	}
	if len(AnalyzeAll(nil, Options{})) != 0 || Apply(&ast.ResolvedPrompt{}, Options{}) != 0 {
		t.Error("empty input")
	}
}

func TestIssueString(t *testing.T) {
	for _, i := range []Issue{
		{Kind: KindExactDuplicate, Prompt: "P", Field: "f", ItemA: "a"},
		{Kind: KindNearDuplicate, Prompt: "P", Field: "f", ItemA: "a", ItemB: "b", Similarity: 0.9},
		{Kind: KindContradiction, Prompt: "P", Field: "f", ItemA: "a", ItemB: "b"},
	} {
		s := i.String()
		if !strings.Contains(s, "P.f") || !strings.Contains(s, string(i.Kind)) {
			t.Errorf("%s", s)
		}
	}
	if !strings.Contains(Issue{Kind: KindNearDuplicate, Similarity: 0.9}.String(), "90%") {
		t.Error("percentage")
	}
	if (Issue{}).String() != "" {
		t.Error("unknown kind")
	}
}

func TestThresholdDefault(t *testing.T) {
	if (Options{}).threshold() != 0.85 || (Options{NearDupThreshold: -1}).threshold() != 0.85 || (Options{NearDupThreshold: 0.5}).threshold() != 0.5 {
		t.Error("threshold")
	}
}

func TestNormaliseSimilarityLevenshtein(t *testing.T) {
	if normalise("  - Hello,   World! ") != "hello world" || normalise("Don't") != "dont" || normalise("日本 語") != "日本 語" || normalise("!!!") != "" {
		t.Error("normalise")
	}
	if similarity("", "") != 1 || similarity("abc", "abc") != 1 || similarity("abc", "xyz") != 0 || similarity("kitten", "sitting") < 0.57 || similarity("kitten", "sitting") > 0.58 {
		t.Error("similarity")
	}
	if levenshtein([]rune("kitten"), []rune("sitting")) != 3 || levenshtein(nil, []rune("abc")) != 3 || levenshtein([]rune("abc"), nil) != 3 {
		t.Error("levenshtein")
	}
	if min3(3, 1, 2) != 1 || min3(1, 2, 3) != 1 || min3(2, 3, 1) != 1 || min3(2, 1, 3) != 1 {
		t.Error("min3")
	}
}
