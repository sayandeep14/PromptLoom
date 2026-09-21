package format

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sayandeepgiri/promptloom/internal/ast"
	"github.com/sayandeepgiri/promptloom/internal/parser"
)

// Source formats a whole source file, keeping its comments.
//
// Formatting rebuilds the file from the parsed tree, so it must be provably lossless:
// after formatting, the output is parsed again and an inventory of everything the file
// declares (fields, operators, variables and their metadata, blocks, variants, env
// blocks, contract entries, tags, comments) is compared with the original. If anything
// differs, Source returns an error and the caller must leave the file untouched.
func Source(filename, src string) (string, error) {
	nodes, comments, err := parser.ParseWithComments(filename, src)
	if err != nil {
		return "", err
	}
	c := newCommentCtx(nodes, comments)

	parts := make([]string, len(nodes))
	for i, n := range nodes {
		parts[i] = c.node(n)
	}
	out := strings.Join(parts, "\n")

	if len(c.fileTail) > 0 {
		var sb strings.Builder
		if len(nodes) > 0 {
			sb.WriteString("\n")
		}
		writeComments(&sb, c.fileTail, 0, 0)
		out += sb.String()
	}

	// Safety net: never hand back output that has lost something.
	reNodes, reComments, err := parser.ParseWithComments(filename, out)
	if err != nil {
		return "", fmt.Errorf("refusing to format %s: the formatted output does not parse (%v)", filename, err)
	}
	if want, got := inventory(nodes), inventory(reNodes); !equalStrings(want, got) {
		return "", fmt.Errorf("refusing to format %s: formatting would change its content (%s)", filename, firstDiff(want, got))
	}
	if len(reComments) != len(comments) {
		return "", fmt.Errorf("refusing to format %s: %d comment(s) would be lost", filename, len(comments)-len(reComments))
	}
	return out, nil
}

// inventory lists everything a set of nodes declares, in a form that is stable across
// formatting. from() expressions are compared by presence only, because the formatter
// legitimately simplifies them.
func inventory(nodes []*ast.Node) []string {
	var out []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	fields := func(owner string, fs []ast.FieldOperation) {
		for _, f := range fs {
			if f.FromExpr != nil {
				add("%s field %s %s from()", owner, f.FieldName, f.Op)
			} else {
				add("%s field %s %s %q", owner, f.FieldName, f.Op, strings.Join(f.Value, "\n"))
			}
		}
	}
	for _, n := range nodes {
		o := fmt.Sprintf("%d %s", n.Kind, n.Name)
		add("%s parents %s", o, strings.Join(n.Parents, ","))
		add("%s uses %s", o, strings.Join(n.Uses, ","))
		add("%s tags %s", o, strings.Join(n.Tags, ","))
		fields(o, n.Fields)
		for _, v := range n.Vars {
			add("%s var %s default=%q slot=%v required=%v secret=%v", o, v.Name, v.Default, v.IsSlot, v.Required, v.Secret)
		}
		for _, v := range n.Variants {
			add("%s variant %s", o, v.Name)
			fields(o+" variant "+v.Name, v.Fields)
		}
		for _, e := range n.EnvBlocks {
			add("%s env %s", o, e.Name)
			fields(o+" env "+e.Name, e.Fields)
		}
		if n.Contract != nil {
			c := n.Contract
			add("%s contract %q %q %q %q", o, c.RequiredSections, c.ForbiddenSections, c.MustInclude, c.MustNotInclude)
		}
		if n.Capabilities != nil {
			add("%s capabilities %q %q", o, n.Capabilities.Allowed, n.Capabilities.Forbidden)
		}
	}
	sort.Strings(out)
	return out
}

func equalStrings(a, b []string) bool {
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

func firstDiff(want, got []string) string {
	seen := map[string]int{}
	for _, g := range got {
		seen[g]++
	}
	for _, w := range want {
		if seen[w] == 0 {
			return "missing after formatting: " + strconv.Quote(w)
		}
		seen[w]--
	}
	return "unexpected extra content after formatting"
}
