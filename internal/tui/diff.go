package tui

import "strings"

type diffOpKind int

const (
	diffEqual diffOpKind = iota
	diffDelete
	diffInsert
)

type diffOp struct {
	kind diffOpKind
	line string
}

func renderUnifiedDiff(before, after string) string {
	ops := diffLines(before, after)
	if len(ops) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString(MutedStyle.Render("--- current") + "\n")
	b.WriteString(MutedStyle.Render("+++ new") + "\n")
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			b.WriteString(MutedStyle.Render("  " + op.line))
		case diffDelete:
			b.WriteString(ErrorStyle.Render("- " + op.line))
		case diffInsert:
			b.WriteString(SuccessStyle.Render("+ " + op.line))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func diffLines(before, after string) []diffOp {
	a := splitDiffLines(before)
	b := splitDiffLines(after)
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}

	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{kind: diffEqual, line: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			ops = append(ops, diffOp{kind: diffDelete, line: a[i]})
			i++
		default:
			ops = append(ops, diffOp{kind: diffInsert, line: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{kind: diffDelete, line: a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{kind: diffInsert, line: b[j]})
	}
	return ops
}

func splitDiffLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
