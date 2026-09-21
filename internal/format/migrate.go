package format

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/sayandeep14/PromptLoom/internal/ast"
	"github.com/sayandeep14/PromptLoom/internal/parser"
)

// Note is one migration step (or one item that needs a human), tied to a source line.
type Note struct {
	Line    int
	Message string
}

// MigrateResult is what Migrate did to one source file.
type MigrateResult struct {
	// Output is the migrated source. It equals the input when nothing was changed.
	Output string
	// Changes lists every rewrite that was applied.
	Changes []Note
	// Manual lists v1 constructs that have no mechanical v2 equivalent. They are left exactly
	// as written, so `loom inspect` keeps reporting them until someone decides what they mean.
	Manual []Note
}

// Changed reports whether Output differs from the input.
func (r MigrateResult) Changed() bool { return len(r.Changes) > 0 }

// Library tells Migrate which fields each block defines, across the whole project. The
// migration of `+=` depends on it: in v1 a prompt's `+=` added to what its `use`d blocks
// contributed, but in v2 a prompt that writes a list field replaces the block's items, so those
// cases cannot be rewritten mechanically. A nil Library only knows blocks in the same file.
type Library struct {
	blocks map[string]map[string]bool
}

// NewLibrary returns an empty Library.
func NewLibrary() *Library { return &Library{blocks: map[string]map[string]bool{}} }

// AddSource records the blocks declared in one source file. Files that do not parse are
// ignored (they are reported when they are migrated themselves).
func (l *Library) AddSource(filename, src string) {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		lines[i] = extendsRe.ReplaceAllString(ln, "${1}inherits${2}")
	}
	nodes, err := parser.Parse(filename, strings.Join(lines, "\n"))
	if err != nil {
		return
	}
	l.addNodes(nodes)
}

func (l *Library) addNodes(nodes []*ast.Node) {
	for _, n := range nodes {
		if n.Kind != ast.KindBlock {
			continue
		}
		set := map[string]bool{}
		for _, f := range n.Fields {
			set[f.FieldName] = true
		}
		l.blocks[n.Name] = set
	}
}

var extendsRe = regexp.MustCompile(`^(\s*prompt\s+[A-Za-z0-9_.-]+\s+)extends(\s)`)

// Migrate rewrites v1 syntax to v2 wherever the meaning is unambiguous:
//
//   - `prompt A extends B {`               → `prompt A inherits B {`
//   - `field:` (bare colon)                → `field :=`
//   - `list +=` in a prompt with parents   → `list := from(parent[*]) and { ... }`
//   - `list +=` in a block or overlay      → `list :=` (blocks and overlays add to lists themselves)
//   - `field +=` in a prompt with no parent → `field :=` (there is nothing to append to)
//
// Everything else is reported in Manual and left untouched: `-=` (v2 has no equivalent),
// `+=` on a scalar that inherits, `+=` inside variant and env blocks, and a field declared more
// than once in the same body. The output is checked by parsing it again, so a migration never
// produces a file that no longer parses or that lost a declaration or a comment.
func Migrate(filename, src string, lib *Library) (MigrateResult, error) {
	res := MigrateResult{Output: src}

	// `extends` is rejected by the lexer, so it has to be fixed as text before parsing.
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		if extendsRe.MatchString(l) {
			lines[i] = extendsRe.ReplaceAllString(l, "${1}inherits${2}")
			res.Changes = append(res.Changes, Note{i + 1, "`extends` → `inherits`"})
		}
	}
	work := strings.Join(lines, "\n")

	nodes, comments, err := parser.ParseWithComments(filename, work)
	if err != nil {
		return res, fmt.Errorf("cannot migrate %s: %w", filename, err)
	}

	// blocks known to the project, plus the ones declared in this very file
	known := NewLibrary()
	if lib != nil {
		for k, v := range lib.blocks {
			known.blocks[k] = v
		}
	}
	known.addNodes(nodes)

	m := &migrator{res: &res, blocks: known.blocks}
	for _, n := range nodes {
		m.node(n)
	}
	if !res.Changed() {
		return res, nil
	}

	out := render(nodes, comments)

	// Safety net: the result must parse, keep every comment, and declare exactly what the
	// migrated tree declares.
	reNodes, reComments, err := parser.ParseWithComments(filename, out)
	if err != nil {
		return MigrateResult{Output: src}, fmt.Errorf("refusing to migrate %s: the result does not parse (%v)", filename, err)
	}
	if want, got := inventory(nodes), inventory(reNodes); !equalStrings(want, got) {
		return MigrateResult{Output: src}, fmt.Errorf("refusing to migrate %s: the rewrite would change its content (%s)", filename, firstDiff(want, got))
	}
	if len(reComments) != len(comments) {
		return MigrateResult{Output: src}, fmt.Errorf("refusing to migrate %s: %d comment(s) would be lost", filename, len(comments)-len(reComments))
	}
	res.Output = out
	return res, nil
}

type migrator struct {
	res    *MigrateResult
	blocks map[string]map[string]bool
}

func (m *migrator) change(line int, format string, a ...any) {
	m.res.Changes = append(m.res.Changes, Note{line, fmt.Sprintf(format, a...)})
}

func (m *migrator) manual(line int, format string, a ...any) {
	m.res.Manual = append(m.res.Manual, Note{line, fmt.Sprintf(format, a...)})
}

func (m *migrator) node(n *ast.Node) {
	m.fields(n, n.Fields, "")
	for _, v := range n.Variants {
		m.fields(n, v.Fields, "variant "+v.Name)
	}
	for _, e := range n.EnvBlocks {
		m.fields(n, e.Fields, "env "+e.Name)
	}
}

// fields migrates one body. where is "" for the node's own body, or "variant x" / "env x".
func (m *migrator) fields(n *ast.Node, fields []ast.FieldOperation, where string) {
	seen := map[string]int{}
	for _, f := range fields {
		seen[f.FieldName]++
	}
	for i := range fields {
		f := &fields[i]
		line := f.Pos.Line
		label := f.FieldName
		if where != "" {
			label = where + " › " + f.FieldName
		}

		switch f.Op {
		case ast.OpDefine:
			f.Op = ast.OpOverride
			m.change(line, "`%s:` → `%s :=`", label, f.FieldName)

		case ast.OpRemove:
			m.manual(line, "`%s -=` has no v2 equivalent: write the list you want with `:=`, and select parent items with `from(parent[N].%s[a..b])`", label, f.FieldName)

		case ast.OpAppend:
			m.appendOp(n, f, label, where, seen[f.FieldName] > 1)
		}
	}
}

func (m *migrator) appendOp(n *ast.Node, f *ast.FieldOperation, label, where string, duplicated bool) {
	line := f.Pos.Line
	if where != "" {
		m.manual(line, "`%s +=` inside a %s block: decide whether it should replace the field or extend it with `from(parent[*]) and { ... }`", label, where)
		return
	}
	if duplicated {
		m.manual(line, "`%s +=` and another declaration of `%s` in the same body: merge them into one `:=` field", label, f.FieldName)
		return
	}
	isList := ast.ListFields[f.FieldName]

	// v1: parent items + the items of every `use`d block + this prompt's. v2 has no way to keep
	// the block's items once the prompt writes the field itself, so this is a human decision.
	if n.Kind == ast.KindPrompt && isList {
		for _, b := range n.Uses {
			defines, known := m.blocks[b][f.FieldName], false
			_, known = m.blocks[b]
			if defines || !known {
				why := "also defines"
				if !known {
					why = "may also define (it is not in the files scanned)"
				}
				m.manual(line, "`%s +=` in a prompt that uses block %s, which %s `%s`: in v2 this prompt's `%s` would replace the block's items. Copy the block's items into this prompt, or move this prompt's items into the block", label, b, why, f.FieldName, f.FieldName)
				return
			}
		}
	}

	switch {
	case n.Kind == ast.KindBlock || n.Kind == ast.KindOverlay:
		if !isList {
			m.manual(line, "`%s +=` on a scalar field in a %s: v2 replaces scalars, so choose the final text and write it with `:=`", label, kindName(n))
			return
		}
		f.Op = ast.OpOverride
		m.change(line, "`%s +=` → `%s :=` (a %s adds to lists itself)", label, f.FieldName, kindName(n))

	case len(n.Parents) == 0:
		f.Op = ast.OpOverride
		m.change(line, "`%s +=` → `%s :=` (no parent to append to)", label, f.FieldName)

	case !isList:
		m.manual(line, "`%s +=` on a scalar field that is inherited: v2 replaces scalars, so choose the final text and write it with `:=`", label)

	default:
		items, ok := bulletItems(f.Value)
		if !ok {
			m.manual(line, "`%s +=` has lines that are not `- ` items; write it as `%s := from(parent[*]) and { - item }` by hand", label, f.FieldName)
			return
		}
		units := []ast.FromUnit{{Kind: ast.FromParentRef, ParentSub: ast.Subscript{Kind: ast.SubAll}, Pos: f.Pos}}
		if len(items) > 0 {
			units = append(units, ast.FromUnit{Kind: ast.FromLiteral, Items: items, Pos: f.Pos})
		}
		f.Op = ast.OpOverride
		f.Value = nil
		f.FromExpr = &ast.FromExpression{Units: units, Pos: f.Pos}
		m.change(line, "`%s +=` → `%s := from(parent[*]) and { ... }`", label, f.FieldName)
	}
}

// bulletItems strips the "- " of each line of a list value. ok is false when some line is not
// a bullet (a continuation line, for example), which would change meaning if rewritten.
func bulletItems(value []string) (items []string, ok bool) {
	for _, l := range value {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "- ") {
			return nil, false
		}
		items = append(items, strings.TrimSpace(strings.TrimPrefix(t, "- ")))
	}
	return items, true
}

func kindName(n *ast.Node) string {
	if n.Kind == ast.KindOverlay {
		return "overlay"
	}
	return "block"
}
