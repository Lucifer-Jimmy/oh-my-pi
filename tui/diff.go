package tui

import (
	"fmt"
	"strings"
)

const (
	diffContext   = 2
	maxDiffMatrix = 1_000_000
)

type diffLine struct {
	op   byte
	text string
}

func diffInput(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func lineDiff(before, after string) []diffLine {
	a, b := diffInput(before), diffInput(after)
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	out := make([]diffLine, 0, len(a)+len(b))
	for _, l := range a[:prefix] {
		out = append(out, diffLine{' ', l})
	}
	out = append(out, middleDiff(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])...)
	for _, l := range a[len(a)-suffix:] {
		out = append(out, diffLine{' ', l})
	}
	return out
}

func middleDiff(a, b []string) []diffLine {
	if len(a)*len(b) > maxDiffMatrix {
		out := make([]diffLine, 0, len(a)+len(b))
		for _, l := range a {
			out = append(out, diffLine{'-', l})
		}
		for _, l := range b {
			out = append(out, diffLine{'+', l})
		}
		return out
	}
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []diffLine
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, diffLine{' ', a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, diffLine{'-', a[i]})
			i++
		default:
			out = append(out, diffLine{'+', b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, diffLine{'-', a[i]})
	}
	for ; j < len(b); j++ {
		out = append(out, diffLine{'+', b[j]})
	}
	return out
}

func collapse(lines []diffLine, context int) []diffLine {
	keep := make([]bool, len(lines))
	changed := false
	for i, l := range lines {
		if l.op == ' ' {
			continue
		}
		changed = true
		for k := max(0, i-context); k <= min(len(lines)-1, i+context); k++ {
			keep[k] = true
		}
	}
	if !changed {
		return lines
	}
	var out []diffLine
	skipped := 0
	for i, l := range lines {
		if keep[i] {
			if skipped > 0 {
				out = append(out, diffLine{'~', fmt.Sprintf("%d unchanged lines", skipped)})
				skipped = 0
			}
			out = append(out, l)
			continue
		}
		skipped++
	}
	if skipped > 0 {
		out = append(out, diffLine{'~', fmt.Sprintf("%d unchanged lines", skipped)})
	}
	return out
}

func diffStat(lines []diffLine) (added, removed int) {
	for _, l := range lines {
		switch l.op {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return added, removed
}

func renderDiff(lines []diffLine, width, limit int, lead string, wrap bool) []string {
	var rows []string
	for _, l := range lines {
		style, sign := styleDim, "  "
		switch l.op {
		case '+':
			style, sign = styleOK, "+ "
		case '-':
			style, sign = styleErr, "- "
		case '~':
			sign = "⋯ "
		}
		rows = append(rows, fitRows(lead, sign+clean(l.text), width, wrap, style)...)
	}
	return limitRows(rows, limit, lead)
}

func limitRows(rows []string, limit int, lead string) []string {
	if limit <= 0 || len(rows) <= limit {
		return rows
	}
	return append(rows[:limit:limit], lead+styleDim.Render(moreLines(len(rows)-limit)))
}
