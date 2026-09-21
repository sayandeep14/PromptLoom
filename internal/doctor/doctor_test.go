package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sayandeepgiri/promptloom/internal/config"
	"github.com/sayandeepgiri/promptloom/internal/parser"
	"github.com/sayandeepgiri/promptloom/internal/registry"
)

func build(t *testing.T, srcs ...string) *registry.Registry {
	t.Helper()
	reg := registry.New()
	for i, src := range srcs {
		nodes, err := parser.Parse(fmt.Sprintf("f%d.loom", i), src)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if err := reg.Register(nodes); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

func smellNames(r *HealthReport) map[string]bool {
	m := map[string]bool{}
	for _, s := range r.Smells {
		m[s.Name] = true
	}
	return m
}

func check(t *testing.T, reg *registry.Registry, cfg *config.Config, name string) *HealthReport {
	t.Helper()
	r, err := CheckPrompt(name, reg, cfg, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUnknownPrompt(t *testing.T) {
	if _, err := CheckPrompt("Nope", registry.New(), config.Defaults(), t.TempDir()); err == nil {
		t.Error("expected an error for an unknown prompt")
	}
}

func TestHealthyPromptHasNoSmells(t *testing.T) {
	reg := build(t, `
prompt Good {
  persona :=
    You are a careful reviewer.
  objective :=
    Review the code.
  instructions :=
    - Read the diff.
    - Report issues by severity.
  format :=
    - Summary
}`)
	r := check(t, reg, config.Defaults(), "Good")
	if len(r.Smells) != 0 {
		t.Errorf("unexpected smells: %+v", r.Smells)
	}
}

func TestOutputAmbiguity(t *testing.T) {
	reg := build(t, "prompt P {\n  persona :=\n    x.\n}")
	if !smellNames(check(t, reg, config.Defaults(), "P"))["Output Ambiguity"] {
		t.Error("a prompt with no format should be flagged")
	}
}

func TestConstraintPileUp(t *testing.T) {
	var b strings.Builder
	b.WriteString("prompt P {\n  format :=\n    - a\n  constraints :=\n")
	for i := 0; i < 26; i++ {
		fmt.Fprintf(&b, "    - unique rule number %c%c\n", 'a'+rune(i%26), 'a'+rune((i/3)%26))
	}
	b.WriteString("}")
	reg := build(t, b.String())

	if !smellNames(check(t, reg, config.Defaults(), "P"))["Constraint Pile-Up"] {
		t.Error("26 constraints exceed the default limit of 25")
	}
	cfg := config.Defaults()
	cfg.Validation.SmellConstraintLimit = 100
	if smellNames(check(t, reg, cfg, "P"))["Constraint Pile-Up"] {
		t.Error("the limit is configurable")
	}
}

func TestGodPrompt(t *testing.T) {
	reg := build(t, `
prompt P {
  objective :=
    One. Two. Three. Four. Five. Six.
  format :=
    - a
}`)
	if !smellNames(check(t, reg, config.Defaults(), "P"))["God Prompt"] {
		t.Error("a six-sentence objective should be flagged")
	}
}

func TestPersonaSoup(t *testing.T) {
	reg := build(t, `
prompt P {
  persona :=
    You are a lawyer and you are also a chef.
  format :=
    - a
}`)
	if !smellNames(check(t, reg, config.Defaults(), "P"))["Persona Soup"] {
		t.Error("multiple roles should be flagged")
	}
}

func TestDuplicateInstructions(t *testing.T) {
	reg := build(t, `
prompt P {
  instructions :=
    - Always validate every user input carefully before processing
    - Always validate every user input carefully before processing it
  format :=
    - a
}`)
	if !smellNames(check(t, reg, config.Defaults(), "P"))["Duplicate Instructions"] {
		t.Error("near-identical instructions should be flagged")
	}
}

func TestConflictingInstructions(t *testing.T) {
	reg := build(t, `
prompt P {
  instructions :=
    - Be concise.
    - Explain everything in detail.
  format :=
    - a
}`)
	r := check(t, reg, config.Defaults(), "P")
	if !smellNames(r)["Conflicting Instructions"] {
		t.Fatalf("expected a conflict, got %+v", r.Smells)
	}
}

// One item can contain both halves of a pair ("never explain" also contains "explain").
// It cannot conflict with itself.
func TestSingleItemNeverConflictsWithItself(t *testing.T) {
	reg := build(t, `
prompt P {
  constraints :=
    - Never explain internal reasoning to the user.
  format :=
    - a
}`)
	r := check(t, reg, config.Defaults(), "P")
	if smellNames(r)["Conflicting Instructions"] {
		t.Errorf("false positive: %+v", r.Smells)
	}
	if got := findConflicts([]string{"Never explain X", "Please explain why"}); len(got) != 1 {
		t.Errorf("two different items should still conflict, got %v", got)
	}
}

func TestFormatDrift(t *testing.T) {
	reg := build(t, `
prompt Base {
  format :=
    - Summary
    - Details
}

prompt Child inherits Base {
  format :=
    - Answer
}`)
	if !smellNames(check(t, reg, config.Defaults(), "Child"))["Format Drift"] {
		t.Error("a child that changes the parent's format should be flagged")
	}
	reg = build(t, `
prompt Base {
  format :=
    - Summary
}

prompt Child inherits Base {
  persona :=
    x.
}`)
	if smellNames(check(t, reg, config.Defaults(), "Child"))["Format Drift"] {
		t.Error("inheriting the same format is not drift")
	}
}

func TestStructuralChecks(t *testing.T) {
	reg := build(t, `
block B {
  constraints :=
    - x
}

prompt Base {
  format :=
    - a
}

prompt Child inherits Base {
  use B
}`)
	cfg := config.Defaults()
	cfg.Validation.RequireContract = true
	cfg.Validation.TokenLimitWarn = 1
	r := check(t, reg, cfg, "Child")

	got := map[string]StructuralResult{}
	for _, s := range r.Structurals {
		got[s.Label] = s
	}
	for _, label := range []string{"Parses cleanly", "Parent resolves", "All blocks resolve"} {
		if s, ok := got[label]; !ok || !s.Pass {
			t.Errorf("%q should pass: %+v", label, s)
		}
	}
	if s := got["Contract declared"]; s.Pass || !s.IsWarn {
		t.Errorf("missing contract should warn: %+v", s)
	}
	if s := got["Dist file exists"]; s.Pass || !s.IsWarn {
		t.Errorf("no dist file should warn: %+v", s)
	}
	if _, ok := got["Token limit (1)"]; !ok {
		t.Errorf("token limit check missing: %v", got)
	}
}

func TestDistFreshness(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "prompts", "P.prompt.loom")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "prompt P {\n  format :=\n    - a\n}\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	nodes, err := parser.Parse(src, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(nodes); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()

	dist := filepath.Join(dir, cfg.Paths.Out, "P.md")
	if err := os.MkdirAll(filepath.Dir(dist), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dist, []byte("# P"), 0o644); err != nil {
		t.Fatal(err)
	}

	find := func() StructuralResult {
		r, err := CheckPrompt("P", reg, cfg, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range r.Structurals {
			if strings.HasPrefix(s.Label, "Dist file") {
				return s
			}
		}
		t.Fatal("no dist check")
		return StructuralResult{}
	}

	now := time.Now()
	os.Chtimes(src, now.Add(-time.Hour), now.Add(-time.Hour))
	os.Chtimes(dist, now, now)
	if s := find(); !s.Pass || s.Label != "Dist file fresh" {
		t.Errorf("dist newer than source should be fresh: %+v", s)
	}

	os.Chtimes(dist, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	if s := find(); s.Pass || !strings.Contains(s.Detail, "stale") {
		t.Errorf("dist older than source should be stale: %+v", s)
	}
}

func TestScoreAndBands(t *testing.T) {
	cases := map[int]string{100: "Excellent", 90: "Excellent", 89: "Good", 75: "Good", 74: "Needs improvement",
		60: "Needs improvement", 59: "Risky", 40: "Risky", 39: "Poor", 0: "Poor"}
	for score, want := range cases {
		if got := scoreBand(score); got != want {
			t.Errorf("scoreBand(%d) = %q, want %q", score, got, want)
		}
	}

	r := &HealthReport{
		Structurals: []StructuralResult{{Pass: false, IsWarn: true}, {Pass: false}, {Pass: true}},
		Smells:      []Smell{{Name: "God Prompt"}, {Name: "Persona Soup"}},
	}
	if got := computeScore(r); got != 100-5-15-10-5 {
		t.Errorf("score = %d, want 65", got)
	}
	r = &HealthReport{}
	for i := 0; i < 20; i++ {
		r.Structurals = append(r.Structurals, StructuralResult{})
	}
	if got := computeScore(r); got != 0 {
		t.Errorf("score must floor at 0, got %d", got)
	}
}

func TestCheckAllIsSortedAndSkipsBrokenPrompts(t *testing.T) {
	reg := build(t, `
prompt Zeta {
  format :=
    - a
}

prompt Alpha {
  format :=
    - a
}

prompt Broken inherits Missing {
  format :=
    - a
}`)
	reports, err := CheckAll(reg, config.Defaults(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range reports {
		names = append(names, r.Name)
	}
	if strings.Join(names, ",") != "Alpha,Zeta" {
		t.Errorf("got %v, want [Alpha Zeta] (sorted, broken prompt skipped)", names)
	}
}

func TestUnusedBlocks(t *testing.T) {
	reg := build(t, `
block Used {
  constraints :=
    - a
}

block Orphan {
  constraints :=
    - b
}

block AlsoOrphan {
  constraints :=
    - c
}

prompt P {
  use Used
}`)
	got := strings.Join(UnusedBlocks(reg), ",")
	if got != "AlsoOrphan,Orphan" {
		t.Errorf("got %q", got)
	}
}

func TestHelpers(t *testing.T) {
	if countSentences("") != 0 || countSentences("no punctuation") != 1 || countSentences("A. B! C?") != 3 {
		t.Error("countSentences")
	}
	if jaccardSimilarity("a b c", "a b c") != 1 || jaccardSimilarity("a b", "c d") != 0 {
		t.Error("jaccardSimilarity")
	}
	if jaccardSimilarity("", "") != 1 {
		t.Error("two empty strings are identical")
	}
	if truncate("short", 10) != "short" || len([]rune(truncate("this is quite long indeed", 10))) > 10 {
		t.Error("truncate")
	}
	if !sameFormatSignature([]string{" Summary "}, []string{"summary"}) || sameFormatSignature([]string{"a"}, []string{"a", "b"}) {
		t.Error("sameFormatSignature")
	}
}

func TestTruncateNeverSplitsACharacter(t *testing.T) {
	in := "日本語のプロンプトは長い文章になることがあります"
	for n := 2; n <= 20; n++ { // every length, so no byte boundary can line up by luck
		out := truncate(in, n)
		if !utf8.ValidString(out) {
			t.Errorf("truncate(%d) produced invalid UTF-8: %q", n, out)
		}
		if c := utf8.RuneCountInString(out); c > n {
			t.Errorf("truncate(%d) has %d characters", n, c)
		}
	}
	if got := truncate("héllo wörld", 20); got != "héllo wörld" {
		t.Errorf("short input must be unchanged, got %q", got)
	}
}
