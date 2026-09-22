package format

import (
	"fmt"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/lexer"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// ReplaceFields rewrites ONLY the field values of the prompt or block named `name` in src, using
// newFields as its new body. Everything else in the file keeps its content: every other
// declaration, and for the target node itself its kind, name, parents, `use` lines, vars,
// variants, env blocks, contract, capabilities and tags — ReplaceFields never even looks at those
// parts of newFields' surrounding declaration, so a caller cannot smuggle a structural change
// through it. No comment is lost, exactly like format.Source: one that sat inside the replaced
// field content moves to the end of the node's body (there is no way to know which new line it
// still belongs to); every other comment keeps its exact text and position.
//
// This is the primitive `loom fmt --migrate` and `loom optimize` write through: a caller that
// proposes new field text (a human edit, or a model's suggestion) can apply it with the same
// lossless guarantee the formatter gives the rest of the file, and a proposal that tries to change
// anything else is refused rather than silently accepted.
func ReplaceFields(filename, src, name string, newFields []ast.FieldOperation) (string, error) {
	nodes, comments, err := parser.ParseWithComments(filename, src)
	if err != nil {
		return "", fmt.Errorf("%s: %w", filename, err)
	}

	idx := -1
	for i, n := range nodes {
		if n.Name == name {
			if idx != -1 {
				return "", fmt.Errorf("%s: %q is declared more than once", filename, name)
			}
			idx = i
		}
	}
	if idx == -1 {
		return "", fmt.Errorf("%s: no declaration named %q", filename, name)
	}

	orig := nodes[idx]
	replaced := *orig
	replaced.Fields = newFields
	newNodes := append([]*ast.Node(nil), nodes...)
	newNodes[idx] = &replaced

	out := render(newNodes, comments)

	// Safety net: parse the result and check every guarantee above.
	reNodes, reComments, err := parser.ParseWithComments(filename, out)
	if err != nil {
		return "", fmt.Errorf("refusing the change to %s: the result does not parse (%v)", name, err)
	}
	if len(reNodes) != len(nodes) {
		return "", fmt.Errorf("refusing the change to %s: the result declares %d node(s), expected %d", name, len(reNodes), len(nodes))
	}
	reIdx := -1
	for i, n := range reNodes {
		if n.Name == name {
			reIdx = i
		}
	}
	if reIdx < 0 {
		return "", fmt.Errorf("refusing the change to %s: it is missing from the result", name)
	}
	if reIdx != idx {
		return "", fmt.Errorf("refusing the change to %s: it moved within the file", name)
	}
	if err := sameStructure(orig, reNodes[reIdx]); err != nil {
		return "", fmt.Errorf("refusing the change to %s: %w", name, err)
	}
	if want, got := withoutFields(inventory(nodes), orig), withoutFields(inventory(reNodes), reNodes[reIdx]); !equalStrings(want, got) {
		return "", fmt.Errorf("refusing the change to %s: something besides its own fields changed (%s)", name, firstDiff(want, got))
	}
	if len(reComments) != len(comments) {
		return "", fmt.Errorf("refusing the change to %s: %d comment(s) would be lost", name, len(comments)-len(reComments))
	}
	if before, after := commentsOutside(comments, orig), commentsOutside(reComments, reNodes[reIdx]); !sameComments(before, after) {
		return "", fmt.Errorf("refusing the change to %s: a comment outside it would be lost or moved", name)
	}
	return out, nil
}

// sameStructure checks the parts of a node ReplaceFields must never change.
func sameStructure(a, b *ast.Node) error {
	if a.Kind != b.Kind {
		return fmt.Errorf("its kind changed")
	}
	if !equalStrSlices(a.Parents, b.Parents) {
		return fmt.Errorf("its parents changed (%v -> %v)", a.Parents, b.Parents)
	}
	if !equalStrSlices(a.Uses, b.Uses) {
		return fmt.Errorf("its `use` lines changed (%v -> %v)", a.Uses, b.Uses)
	}
	if !equalStrSlices(a.Tags, b.Tags) {
		return fmt.Errorf("its tags changed")
	}
	if len(a.Vars) != len(b.Vars) {
		return fmt.Errorf("its var/slot declarations changed")
	}
	if len(a.Variants) != len(b.Variants) {
		return fmt.Errorf("its variants changed")
	}
	if len(a.EnvBlocks) != len(b.EnvBlocks) {
		return fmt.Errorf("its env blocks changed")
	}
	if (a.Contract == nil) != (b.Contract == nil) {
		return fmt.Errorf("its contract block was added or removed")
	}
	if (a.Capabilities == nil) != (b.Capabilities == nil) {
		return fmt.Errorf("its capabilities block was added or removed")
	}
	return nil
}

func equalStrSlices(a, b []string) bool {
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

// withoutFields is the inventory of everything EXCEPT the target node's own field entries: every
// other node, and the target node's non-field declarations (already re-checked by sameStructure,
// kept here too as a second, independent check).
func withoutFields(inv []string, target *ast.Node) []string {
	fieldPrefix := fmt.Sprintf("%d %s field ", target.Kind, target.Name)
	var out []string
	for _, e := range inv {
		if !strings.HasPrefix(e, fieldPrefix) {
			out = append(out, e)
		}
	}
	return out
}

// commentsOutside returns the comments that do not sit inside node's own body (before its
// declaration line, or after its closing brace), in file order.
func commentsOutside(comments []lexer.Comment, node *ast.Node) []lexer.Comment {
	var out []lexer.Comment
	for _, c := range comments {
		if c.Line < node.Pos.Line || c.Line > node.EndLine {
			out = append(out, c)
		}
	}
	return out
}

func sameComments(a, b []lexer.Comment) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Text != b[i].Text {
			return false
		}
	}
	return true
}
