package tui

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	mdparse "github.com/Lucifer-Jimmy/oh-my-pi/markdown"
)

const (
	noBreakBefore  = "，。、；：！？）」』】》〉”’…,.;:!?)]}%·"
	noBreakAfter   = "（「『【《〈“‘([{"
	softBreakAfter = `/\_-.:?&=#,;`
)

type span struct {
	text  string
	style mdStyle
}

type glyph struct {
	text  string
	width int
	style mdStyle
	r     rune
}

func (g glyph) space() bool { return g.r == ' ' }

func layout(spans []span, width int) []string {
	var rows []string
	for _, line := range splitLines(spans) {
		for _, row := range wrapGlyphs(glyphs(line), width) {
			rows = append(rows, paint(row))
		}
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func splitLines(spans []span) [][]span {
	lines := [][]span{nil}
	for _, s := range spans {
		for i, part := range strings.Split(s.text, "\n") {
			if i > 0 {
				lines = append(lines, nil)
			}
			if part != "" {
				lines[len(lines)-1] = append(lines[len(lines)-1], span{part, s.style})
			}
		}
	}
	return lines
}

func glyphs(line []span) []glyph {
	var out []glyph
	for _, s := range line {
		t := clean(s.text)
		for t != "" {
			g, w := t[:1], 1
			if t[0] >= utf8.RuneSelf || (len(t) > 1 && t[1] >= utf8.RuneSelf) {
				if c, cw := ansi.FirstGraphemeCluster(t, ansi.GraphemeWidth); c != "" {
					g, w = c, cw
				}
			}
			r, _ := utf8.DecodeRuneInString(g)
			out = append(out, glyph{text: g, width: w, style: s.style, r: r})
			t = t[len(g):]
		}
	}
	return out
}

func wrapGlyphs(gs []glyph, width int) [][]glyph {
	var rows [][]glyph
	var row, gap []glyph
	used, gapWidth := 0, 0
	for i := 0; i < len(gs); {
		j := unitEnd(gs, i)
		unit := gs[i:j]
		i = j
		w := widthOf(unit)
		if unit[0].space() {
			if len(row) > 0 {
				gap, gapWidth = unit, w
			}
			continue
		}
		if len(row) > 0 && used+gapWidth+w > width {
			rows = append(rows, row)
			row, used = nil, 0
		}
		if len(row) > 0 {
			row = append(row, gap...)
			used += gapWidth
		}
		gap, gapWidth = nil, 0
		for _, g := range unit {
			if len(row) > 0 && used+g.width > width {
				keep := softBreak(row, width)
				rows = append(rows, row[:keep])
				row = append([]glyph(nil), row[keep:]...)
				used = widthOf(row)
			}
			row = append(row, g)
			used += g.width
		}
	}
	return append(rows, row)
}

func softBreak(row []glyph, width int) int {
	for k := len(row) - 1; k > 0; k-- {
		if widthOf(row[:k]) < width/2 {
			break
		}
		if strings.Contains(softBreakAfter, row[k-1].text) {
			return k
		}
	}
	return len(row)
}

func unitEnd(gs []glyph, i int) int {
	j := i + 1
	for j < len(gs) && gs[j].space() == gs[i].space() {
		if !gs[j].space() && breakable(gs[j-1].r, gs[j].r) {
			break
		}
		j++
	}
	return j
}

func breakable(prev, next rune) bool {
	if !mdparse.IsCJK(prev) && !mdparse.IsCJK(next) {
		return false
	}
	return !strings.ContainsRune(noBreakBefore, next) && !strings.ContainsRune(noBreakAfter, prev)
}

func widthOf(gs []glyph) int {
	w := 0
	for _, g := range gs {
		w += g.width
	}
	return w
}

func widestGlyph(gs []glyph) int {
	w := 0
	for _, g := range gs {
		w = max(w, g.width)
	}
	return w
}

func longestUnit(gs []glyph) int {
	longest := 0
	for i := 0; i < len(gs); {
		j := unitEnd(gs, i)
		if !gs[i].space() {
			longest = max(longest, widthOf(gs[i:j]))
		}
		i = j
	}
	return longest
}

func paint(row []glyph) string {
	var b, run strings.Builder
	for i, g := range row {
		run.WriteString(g.text)
		if i == len(row)-1 || row[i+1].style != g.style {
			b.WriteString(g.style.render(run.String()))
			run.Reset()
		}
	}
	return b.String()
}

func hardWrap(s string, width int) []string {
	if ansi.StringWidth(s) <= width {
		return []string{s}
	}
	return strings.Split(ansi.Hardwrap(s, width, true), "\n")
}
