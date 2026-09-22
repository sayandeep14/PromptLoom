package optimize

import (
	"context"
	"fmt"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/eval"
	"github.com/sayandeep14/PromptLoom/internal/format"
	"github.com/sayandeep14/PromptLoom/internal/llm"
	"github.com/sayandeep14/PromptLoom/internal/parser"
	"github.com/sayandeep14/PromptLoom/internal/starter"
)

const refinerSystem = `You improve PromptLoom prompt declarations so they score better on an evaluation suite.

You will be shown one prompt declaration and a list of eval cases it did not score well on, with
the grader's notes. Rewrite the declaration to address those notes. You must:
  - keep the exact same name, inherits list, use lines, var/slot declarations, variant and env
    blocks, and contract and capabilities blocks — copy them verbatim
  - change only the CONTENT of the prompt's own fields (persona, instructions, constraints, and
    so on) to fix the problems described
  - use only valid PromptLoom v2 syntax (below)
  - output ONLY the prompt declaration, starting with "prompt" and ending with the closing "}" —
    no explanation, no code fence, no other text

` + starter.DSLReference

// Candidate is a proposal the refiner produced, already validated against the rules above.
type Candidate struct {
	Fields []ast.FieldOperation // ready to hand to format.ReplaceFields
	Text   string               // the candidate's own canonical text, for the diff and the record
}

// Propose asks the refiner model for a better version of the prompt named `name`, given feedback
// from a failed eval run, and validates the result before returning it: the reply must parse as
// exactly one `prompt <Name> { ... }` declaration whose structure (parents, use, vars, variants,
// env blocks, contract, capabilities, tags) is identical to the current one. A reply that fails
// this is a normal outcome, not a crash: it comes back as an error the caller can show and move on
// from, never something silently accepted.
func Propose(ctx context.Context, refiner eval.Completer, filename, src, name string, feedback []Feedback) (*Candidate, error) {
	nodes, err := parser.Parse(filename, src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filename, err)
	}
	var orig *ast.Node
	for _, n := range nodes {
		if n.Name == name {
			if orig != nil {
				return nil, fmt.Errorf("%s: %q is declared more than once", filename, name)
			}
			orig = n
		}
	}
	if orig == nil {
		return nil, fmt.Errorf("%s: no prompt named %q", filename, name)
	}
	if orig.Kind != ast.KindPrompt {
		return nil, fmt.Errorf("%q is a %s, not a prompt — only prompts can be refined", name, kindWord(orig.Kind))
	}
	if len(feedback) == 0 {
		return nil, fmt.Errorf("no feedback to refine from")
	}

	current := format.Node(orig)
	reply, err := refiner.Complete(ctx, llm.Request{System: refinerSystem, User: refinerPrompt(current, feedback), MaxTokens: 4096})
	if err != nil {
		return nil, fmt.Errorf("refiner call failed: %w", err)
	}
	reply = stripFence(reply)

	candNodes, err := parser.Parse(filename+" (candidate)", reply)
	if err != nil {
		return nil, fmt.Errorf("the refiner's reply does not parse: %w\n\nreply:\n%s", err, snippet(reply))
	}
	if len(candNodes) != 1 {
		return nil, fmt.Errorf("the refiner returned %d declarations, expected exactly one", len(candNodes))
	}
	cand := candNodes[0]
	if cand.Name != name {
		return nil, fmt.Errorf("the refiner renamed the prompt (%q -> %q)", name, cand.Name)
	}
	if err := sameStructure(orig, cand); err != nil {
		return nil, fmt.Errorf("the refiner's proposal changes more than the prompt's fields: %w", err)
	}
	if fieldsEqual(orig.Fields, cand.Fields) {
		return nil, fmt.Errorf("the refiner made no change")
	}
	return &Candidate{Fields: cand.Fields, Text: format.Node(cand)}, nil
}

// sameStructure is the same rule format.ReplaceFields enforces, checked here too so a bad
// proposal is reported with a clear reason before any file is touched.
func sameStructure(a, b *ast.Node) error {
	if !equalStrSlice(a.Parents, b.Parents) {
		return fmt.Errorf("the inherits list changed")
	}
	if !equalStrSlice(a.Uses, b.Uses) {
		return fmt.Errorf("the use lines changed")
	}
	if !equalStrSlice(a.Tags, b.Tags) {
		return fmt.Errorf("the tags changed")
	}
	if len(a.Vars) != len(b.Vars) {
		return fmt.Errorf("the var/slot declarations changed")
	}
	for i := range a.Vars {
		if a.Vars[i].Name != b.Vars[i].Name || a.Vars[i].IsSlot != b.Vars[i].IsSlot {
			return fmt.Errorf("the var/slot declarations changed")
		}
	}
	if len(a.Variants) != len(b.Variants) {
		return fmt.Errorf("the variants changed")
	}
	for i := range a.Variants {
		if a.Variants[i].Name != b.Variants[i].Name {
			return fmt.Errorf("the variants changed")
		}
	}
	if len(a.EnvBlocks) != len(b.EnvBlocks) {
		return fmt.Errorf("the env blocks changed")
	}
	if (a.Contract == nil) != (b.Contract == nil) {
		return fmt.Errorf("the contract block was added or removed")
	}
	if (a.Capabilities == nil) != (b.Capabilities == nil) {
		return fmt.Errorf("the capabilities block was added or removed")
	}
	return nil
}

func equalStrSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fieldsEqual compares field content only (name, operator, value lines), ignoring source
// position, so a candidate that is byte-identical in meaning to the original is recognised even
// if the refiner reformatted whitespace.
func fieldsEqual(a, b []ast.FieldOperation) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].FieldName != b[i].FieldName || a[i].Op != b[i].Op || !equalStrSlice(a[i].Value, b[i].Value) {
			return false
		}
	}
	return true
}

func kindWord(k ast.NodeKind) string {
	switch k {
	case ast.KindBlock:
		return "block"
	case ast.KindOverlay:
		return "overlay"
	}
	return "prompt"
}

// stripFence removes a ```loom / ``` wrapper if the model added one despite being told not to.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if nl := strings.IndexByte(s, '\n'); nl >= 0 && !strings.HasPrefix(s, "\n") {
		s = s[nl+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

func snippet(s string) string {
	if r := []rune(s); len(r) > 500 {
		return string(r[:500]) + "…"
	}
	return s
}
