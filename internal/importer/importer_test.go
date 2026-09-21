package importer

import (
	"strings"
	"testing"

	"github.com/sayandeep14/PromptLoom/internal/config"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/registry"
	"github.com/sayandeep14/PromptLoom/internal/resolve"
	"github.com/sayandeep14/PromptLoom/internal/validate"
)

// check runs the produced DSL through the real parser and validator: an import is only
// useful if what it writes is valid, warning-free v2.
func check(t *testing.T, r Result) *registry.Registry {
	t.Helper()
	nodes, err := parser.Parse("imported.prompt.loom", r.DSL)
	if err != nil {
		t.Fatalf("the imported DSL does not parse: %v\n%s", err, r.DSL)
	}
	reg := registry.New()
	if err := reg.Register(nodes); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Validation.RequireObjective = false
	cfg.Validation.RequireFormat = false
	for _, d := range validate.Validate(reg, cfg) {
		t.Errorf("the imported DSL has a diagnostic: %s\n%s", d, r.DSL)
	}
	return reg
}

const full = `# Code Reviewer

## Persona
You are a senior engineer.

## Objective
Review the diff.

## Instructions
- Read the diff.
* Check errors.
Suggest tests.

## Constraints
- Be kind.

## Output Format
- Summary
- Verdict
`

func TestImportProducesValidV2(t *testing.T) {
	r := Import(full, "CodeReviewer")
	if strings.Contains(r.DSL, ":\n") && !strings.Contains(r.DSL, ":=\n") {
		t.Error("the importer must write ':=' (the bare ':' is v1 syntax)")
	}
	for _, legacy := range []string{"persona:\n", "instructions:\n", "constraints:\n", "format:\n", "+=", "-="} {
		if strings.Contains(r.DSL, legacy) {
			t.Errorf("legacy syntax %q in:\n%s", legacy, r.DSL)
		}
	}
	reg := check(t, r)

	rp, err := resolve.Resolve("CodeReviewer", reg)
	if err != nil {
		t.Fatal(err)
	}
	if rp.Persona != "You are a senior engineer." || rp.Objective != "Review the diff." {
		t.Errorf("scalars: %q / %q", rp.Persona, rp.Objective)
	}
	want := []string{"Read the diff.", "Check errors.", "Suggest tests."}
	if strings.Join(rp.Instructions, "|") != strings.Join(want, "|") {
		t.Errorf("bullets are normalised (- and * and plain lines): %v", rp.Instructions)
	}
	if strings.Join(rp.Format, "|") != "Summary|Verdict" {
		t.Errorf("'Output Format' maps to format: %v", rp.Format)
	}
}

func TestHeadingAliasesAndCase(t *testing.T) {
	r := Import("## PERSONA\nx\n\n## output\n- a\n\n## Output Format\n- b\n", "P")
	reg := check(t, r)
	rp, _ := resolve.Resolve("P", reg)
	if rp.Persona != "x" || strings.Join(rp.Format, "|") != "a|b" {
		t.Errorf("persona=%q format=%v (two headings that map to one field must be merged)", rp.Persona, rp.Format)
	}
}

// Writing the same field twice would make the second silently replace the first.
func TestRepeatedHeadingsAreMergedNotOverwritten(t *testing.T) {
	r := Import("## Instructions\n- one\n\n## Instructions\n- two\n\n## Constraints\n- c1\n\n## Constraints\n- c2\n", "P")
	if strings.Count(r.DSL, "instructions :=") != 1 || strings.Count(r.DSL, "constraints :=") != 1 {
		t.Fatalf("each field may appear once:\n%s", r.DSL)
	}
	rp, _ := resolve.Resolve("P", check(t, r))
	if strings.Join(rp.Instructions, "|") != "one|two" || strings.Join(rp.Constraints, "|") != "c1|c2" {
		t.Errorf("content lost: %v / %v", rp.Instructions, rp.Constraints)
	}
}

func TestUnknownSectionsGoToNotesWithTheirHeadings(t *testing.T) {
	r := Import("## Persona\nx\n\n## Background\nold text\n\n## Tips\nnew text\n", "P")
	if len(r.Warnings) != 2 {
		t.Errorf("one warning per unrecognised section: %v", r.Warnings)
	}
	rp, _ := resolve.Resolve("P", check(t, r))
	if !strings.Contains(rp.Notes, "Background:") || !strings.Contains(rp.Notes, "old text") ||
		!strings.Contains(rp.Notes, "Tips:") || !strings.Contains(rp.Notes, "new text") {
		t.Errorf("both unknown sections must survive in notes: %q", rp.Notes)
	}
}

// Fields cannot contain blank lines; the old importer produced DSL that did not parse.
func TestMultiParagraphTextStillParses(t *testing.T) {
	r := Import("## Persona\n\nFirst paragraph.\n\nSecond paragraph.\n\n## Instructions\n- a\n", "P")
	rp, err := resolve.Resolve("P", check(t, r))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rp.Persona, "First paragraph.") || !strings.Contains(rp.Persona, "Second paragraph.") {
		t.Errorf("no text may be lost: %q", rp.Persona)
	}
	found := false
	for _, w := range r.Warnings {
		if strings.Contains(w, "blank lines") {
			found = true
		}
	}
	if !found {
		t.Errorf("the user should be told blank lines were removed: %v", r.Warnings)
	}
}

func TestHeadingsInsideCodeFencesAreNotSections(t *testing.T) {
	md := "## Examples\n- see below\n\n```markdown\n## Persona\nnot a heading\n```\n\n## Persona\nreal persona\n"
	r := Import(md, "P")
	rp, _ := resolve.Resolve("P", check(t, r))
	if rp.Persona != "real persona" {
		t.Errorf("a heading inside a fenced block was treated as a section: persona=%q", rp.Persona)
	}
	if !strings.Contains(strings.Join(rp.Examples, "\n"), "not a heading") {
		t.Errorf("the fenced example text should stay with its section: %v", rp.Examples)
	}
}

func TestTitleAndPreambleAreSkipped(t *testing.T) {
	r := Import("Some intro text.\n\n# Title Here\nmore preamble\n\n## Persona\nx\n", "P")
	if strings.Contains(r.DSL, "intro") || strings.Contains(r.DSL, "preamble") || strings.Contains(r.DSL, "Title") {
		t.Errorf("text outside sections is not part of the prompt:\n%s", r.DSL)
	}
	check(t, r)
}

func TestEmptyAndDegenerateInput(t *testing.T) {
	for _, src := range []string{"", "no headings at all", "## Persona\n\n\n", "# only a title\n"} {
		r := Import(src, "Empty")
		nodes, err := parser.Parse("x.loom", r.DSL)
		if err != nil || len(nodes) != 1 {
			t.Errorf("%q must still give a valid (possibly empty) prompt: %v\n%s", src, err, r.DSL)
		}
	}
}

func TestNamesAreAlwaysValidIdentifiers(t *testing.T) {
	cases := map[string]string{
		"code-reviewer":       "CodeReviewer",
		"my prompt.md":        "MyPrompt",
		"notes/api_spec.txt":  "ApiSpec",
		"v2.0 [final].md":     "V20Final",
		"123start.md":         "P123start",
		"日本語.md":              "ImportedPrompt",
		"...md":               "ImportedPrompt",
		"weird (copy) (1).md": "Weird(copy)(1)",
	}
	for path, want := range cases {
		got := NameFromPath(path)
		if want == "Weird(copy)(1)" {
			want = "WeirdCopy1"
		}
		if got != want {
			t.Errorf("NameFromPath(%q) = %q, want %q", path, got, want)
		}
		if _, err := parser.Parse("x.loom", Import("## Persona\nx\n", got).DSL); err != nil {
			t.Errorf("%q gives an unusable name %q: %v", path, got, err)
		}
	}
	// the --name flag goes through the same clean-up
	if r := Import("## Persona\nx\n", "my.bad name!"); r.Name != "mybadname" {
		t.Errorf("Name = %q", r.Name)
	}
	if _, err := parser.Parse("x.loom", Import("## Persona\nx\n", "").DSL); err != nil {
		t.Errorf("an empty name must still produce valid DSL: %v", err)
	}
}

func TestLongLinesDoNotAbortTheImport(t *testing.T) {
	long := strings.Repeat("word ", 40000) // ~200 KB on one line
	r := Import("## Persona\n"+long+"\n## Instructions\n- after\n", "P")
	if !strings.Contains(r.DSL, "instructions :=") {
		t.Error("sections after a very long line were lost (bufio.Scanner's default 64 KB limit)")
	}
}
