package fingerprint

import (
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/ast"
)

func base() *ast.ResolvedPrompt {
	return &ast.ResolvedPrompt{
		Summary: "s", Persona: "p", Context: "c", Objective: "o", Notes: "n",
		Instructions: []string{"i1", "i2"}, Constraints: []string{"c1"}, Examples: []string{"e1"}, Format: []string{"f1"},
	}
}

func fp(t *testing.T, rp *ast.ResolvedPrompt) string {
	t.Helper()
	s, err := Compute(rp)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Lockfiles store these hashes. If this value changes, EVERY user's loom.lock reports
// drift after upgrading: change the payload only deliberately, and say so in the release notes.
func TestFingerprintFormatIsStable(t *testing.T) {
	const want = "sha256:e55d1f7d019479d7a2b2be16e015e30b742aba07aa932545e9058d6af1df2fb1"
	got := fp(t, base())
	if got != want {
		t.Errorf("the fingerprint of a fixed prompt changed:\n got  %s\n want %s\nThis invalidates every existing loom.lock. If intentional, update this constant and call it out in the release notes.", got, want)
	}
}

func TestDeterministic(t *testing.T) {
	a, b := fp(t, base()), fp(t, base())
	if a != b {
		t.Error("the same prompt must always hash the same")
	}
}

func TestEveryRenderedFieldChangesTheHash(t *testing.T) {
	ref := fp(t, base())
	mutate := map[string]func(*ast.ResolvedPrompt){
		"summary":      func(r *ast.ResolvedPrompt) { r.Summary += "!" },
		"persona":      func(r *ast.ResolvedPrompt) { r.Persona += "!" },
		"context":      func(r *ast.ResolvedPrompt) { r.Context += "!" },
		"objective":    func(r *ast.ResolvedPrompt) { r.Objective += "!" },
		"notes":        func(r *ast.ResolvedPrompt) { r.Notes += "!" },
		"instructions": func(r *ast.ResolvedPrompt) { r.Instructions = append(r.Instructions, "x") },
		"constraints":  func(r *ast.ResolvedPrompt) { r.Constraints[0] += "!" },
		"examples":     func(r *ast.ResolvedPrompt) { r.Examples = nil },
		"format":       func(r *ast.ResolvedPrompt) { r.Format[0] += "!" },
	}
	for field, f := range mutate {
		rp := base()
		f(rp)
		if fp(t, rp) == ref {
			t.Errorf("changing %s must change the fingerprint", field)
		}
	}
}

func TestListOrderMatters(t *testing.T) {
	rp := base()
	rp.Instructions = []string{"i2", "i1"}
	if fp(t, rp) == fp(t, base()) {
		t.Error("reordering instructions changes the prompt, so it must change the hash")
	}
}

// Fields must not be confusable with one another: moving text between neighbouring fields
// has to change the hash.
func TestValuesCannotBeShiftedBetweenFields(t *testing.T) {
	a := &ast.ResolvedPrompt{Persona: "ab", Context: ""}
	b := &ast.ResolvedPrompt{Persona: "a", Context: "b"}
	c := &ast.ResolvedPrompt{Persona: "", Context: "ab"}
	if fp(t, a) == fp(t, b) || fp(t, a) == fp(t, c) || fp(t, b) == fp(t, c) {
		t.Error("text moved between fields must produce different hashes")
	}
	x := &ast.ResolvedPrompt{Instructions: []string{"a", "b"}}
	y := &ast.ResolvedPrompt{Instructions: []string{"ab"}}
	if fp(t, x) == fp(t, y) {
		t.Error("list items must not be concatenated")
	}
}

// Known scope, pinned: the fingerprint covers the rendered text only. Metadata that does not
// change the rendered prompt (contract, tags, variants list, trace) does not change the hash.
func TestOnlyRenderedTextIsHashed(t *testing.T) {
	ref := fp(t, base())
	rp := base()
	rp.Name = "Renamed"
	rp.Fingerprint = "ignored"
	rp.Warnings = []string{"w"}
	rp.UnresolvedTokens = []string{"x"}
	if fp(t, rp) != ref {
		t.Error("name, warnings and trace data must not affect the fingerprint")
	}
}

func TestSpecialCharactersAreStable(t *testing.T) {
	rp := &ast.ResolvedPrompt{Persona: "<tag> & \"quotes\"   日本語 \x00"}
	if fp(t, rp) != fp(t, rp) {
		t.Error("unstable")
	}
	if fp(t, &ast.ResolvedPrompt{}) == fp(t, rp) {
		t.Error("empty and non-empty prompts must differ")
	}
}
