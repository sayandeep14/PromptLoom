package format

import (
	"sort"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/lexer"
)

// commentCtx carries the comments of the file being formatted. Every comment is
// attached to the element that FOLLOWS it (a field, use, var, variant, env block,
// contract, tags line or a whole prompt), so it travels with that element even when
// canonical ordering moves the element. A zero commentCtx has no comments.
type commentCtx struct {
	lead     map[int][]lexer.Comment // element source line -> comments printed above it
	trailing map[*ast.Node][]lexer.Comment
	fileTail []lexer.Comment // after the last node
	used     map[int]bool
}

// elementLines lists the source line of every element of n that can carry a comment.
func elementLines(n *ast.Node) []int {
	lines := []int{n.Pos.Line}
	add := func(l int) {
		if l > 0 {
			lines = append(lines, l)
		}
	}
	add(n.TagsLine)
	for _, v := range n.Vars {
		add(v.Pos.Line)
	}
	for _, u := range n.UsePos {
		add(u.Line)
	}
	for _, f := range n.Fields {
		add(f.Pos.Line)
	}
	for _, v := range n.Variants {
		add(v.Pos.Line)
		for _, f := range v.Fields {
			add(f.Pos.Line)
		}
	}
	for _, e := range n.EnvBlocks {
		add(e.Pos.Line)
		for _, f := range e.Fields {
			add(f.Pos.Line)
		}
	}
	if n.Contract != nil {
		add(n.Contract.Pos.Line)
	}
	if n.Capabilities != nil {
		add(n.Capabilities.Pos.Line)
	}
	sort.Ints(lines)
	return lines
}

func newCommentCtx(nodes []*ast.Node, comments []lexer.Comment) *commentCtx {
	c := &commentCtx{
		lead:     map[int][]lexer.Comment{},
		trailing: map[*ast.Node][]lexer.Comment{},
		used:     map[int]bool{},
	}
	for _, cm := range comments {
		placed := false
		for i, n := range nodes {
			if cm.Line < n.Pos.Line {
				// between the previous node and this one: leads this node
				c.lead[n.Pos.Line] = append(c.lead[n.Pos.Line], cm)
				placed = true
				break
			}
			if n.EndLine > 0 && cm.Line < n.EndLine {
				// inside n's body: leads the next element, or trails the body
				next := 0
				for _, l := range elementLines(n) {
					if l > cm.Line {
						next = l
						break
					}
				}
				if next > 0 {
					c.lead[next] = append(c.lead[next], cm)
				} else {
					c.trailing[n] = append(c.trailing[n], cm)
				}
				placed = true
				break
			}
			_ = i
		}
		if !placed {
			c.fileTail = append(c.fileTail, cm)
		}
	}
	return c
}

// emit writes the comments attached to the element at source line `line`.
func (c *commentCtx) emit(sb *strings.Builder, line, indent int) {
	if c == nil || c.lead == nil || line <= 0 {
		return
	}
	cms := c.lead[line]
	if len(cms) == 0 || c.used[line] {
		return
	}
	c.used[line] = true
	writeComments(sb, cms, indent, line)
}

// writeComments prints comments at the given indent, keeping a blank line wherever the
// author had one (between comments, and between the last comment and elementLine).
func writeComments(sb *strings.Builder, cms []lexer.Comment, indent, elementLine int) {
	pad := strings.Repeat(" ", indent)
	for i, cm := range cms {
		if i > 0 && cm.Line-cms[i-1].Line > 1 {
			sb.WriteString("\n")
		}
		sb.WriteString(pad + cm.Text + "\n")
	}
	if elementLine > 0 && elementLine-cms[len(cms)-1].Line > 1 {
		sb.WriteString("\n")
	}
}

// leftover returns the comments that trail n's body plus any attached to elements of n
// that were never rendered.
func (c *commentCtx) leftover(n *ast.Node) []lexer.Comment {
	if c == nil || c.lead == nil {
		return nil
	}
	out := append([]lexer.Comment(nil), c.trailing[n]...)
	for _, l := range elementLines(n) {
		if l != n.Pos.Line && !c.used[l] && len(c.lead[l]) > 0 {
			c.used[l] = true
			out = append(out, c.lead[l]...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Line < out[j].Line })
	return out
}
