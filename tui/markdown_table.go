package tui

import (
	"strings"

	east "github.com/yuin/goldmark/extension/ast"
)

const (
	maxCellWord = 30
	shortCell   = 16
)

type tableCell struct {
	lines [][]glyph
	align east.Alignment
}

func (md *markdown) table(n *east.Table, width int) []string {
	var grid [][]tableCell
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		style := mdStyle(0)
		if _, ok := row.(*east.TableHeader); ok {
			style = mdBold
		}
		var cells []tableCell
		for c := row.FirstChild(); c != nil; c = c.NextSibling() {
			cell, ok := c.(*east.TableCell)
			if !ok {
				continue
			}
			var spans []span
			md.inline(cell, md.base|style, &spans)
			var lines [][]glyph
			for _, l := range splitLines(spans) {
				lines = append(lines, glyphs(l))
			}
			cells = append(cells, tableCell{lines: lines, align: cell.Alignment})
		}
		grid = append(grid, cells)
	}
	cols := 0
	for _, cells := range grid {
		cols = max(cols, len(cells))
	}
	if cols == 0 {
		return nil
	}
	natural, words, floor := make([]int, cols), make([]int, cols), make([]int, cols)
	for i := range cols {
		natural[i], floor[i] = 1, 1
	}
	for _, cells := range grid {
		for i, c := range cells {
			for _, l := range c.lines {
				natural[i] = max(natural[i], widthOf(l))
				words[i] = max(words[i], min(longestUnit(l), maxCellWord))
				floor[i] = max(floor[i], widestGlyph(l))
			}
		}
	}
	avail := width - 3*cols - 1
	least := 0
	for _, f := range floor {
		least += f
	}
	if avail < least {
		return narrowTable(grid, width)
	}
	widths := fitColumns(lineWidths(grid, cols), natural, words, floor, avail)
	bar := mdDim.render("│")
	rule := func(left, mid, right string) string {
		parts := make([]string, cols)
		for i, w := range widths {
			parts[i] = strings.Repeat("─", w+2)
		}
		return mdDim.render(left + strings.Join(parts, mid) + right)
	}
	var body [][]string
	tall := false
	for _, cells := range grid {
		lines := tableRow(cells, widths, bar)
		tall = tall || len(lines) > 1
		body = append(body, lines)
	}
	rows := []string{rule("┌", "┬", "┐")}
	for i, lines := range body {
		if i == 1 || (i > 1 && tall) {
			rows = append(rows, rule("├", "┼", "┤"))
		}
		rows = append(rows, lines...)
	}
	return append(rows, rule("└", "┴", "┘"))
}

func lineWidths(grid [][]tableCell, cols int) [][][]int {
	out := make([][][]int, len(grid))
	for r, cells := range grid {
		out[r] = make([][]int, cols)
		for i, c := range cells {
			for _, l := range c.lines {
				out[r][i] = append(out[r][i], widthOf(l))
			}
		}
	}
	return out
}

func fitColumns(lines [][][]int, natural, words, floor []int, avail int) []int {
	widths := make([]int, len(natural))
	total := 0
	for i := range widths {
		widths[i] = max(floor[i], min(natural[i], max(words[i], shortCell)))
		total += widths[i]
	}
	unbroken := func(i int) int { return max(floor[i], min(words[i], natural[i])) }
	shrinkColumns(lines, widths, &total, avail, unbroken)
	shrinkColumns(lines, widths, &total, avail, func(i int) int { return floor[i] })
	for total < avail {
		best, bestHeight := -1, 0
		for i := range widths {
			if widths[i] >= natural[i] {
				continue
			}
			widths[i]++
			h := tableHeight(lines, widths)
			widths[i]--
			if best < 0 || h < bestHeight || (h == bestHeight && natural[i]-widths[i] > natural[best]-widths[best]) {
				best, bestHeight = i, h
			}
		}
		if best < 0 {
			break
		}
		widths[best]++
		total++
	}
	return widths
}

func shrinkColumns(lines [][][]int, widths []int, total *int, avail int, limit func(int) int) {
	for *total > avail {
		best, bestHeight := -1, 0
		for i := range widths {
			if widths[i] <= limit(i) {
				continue
			}
			widths[i]--
			h := tableHeight(lines, widths)
			widths[i]++
			if best < 0 || h < bestHeight || (h == bestHeight && widths[i] > widths[best]) {
				best, bestHeight = i, h
			}
		}
		if best < 0 {
			return
		}
		widths[best]--
		*total--
	}
}

func tableHeight(lines [][][]int, widths []int) int {
	total := 0
	for _, row := range lines {
		height := 1
		for i, cell := range row {
			h := 0
			for _, w := range cell {
				h += max(1, (w+widths[i]-1)/widths[i])
			}
			height = max(height, h)
		}
		total += height
	}
	return total
}

func tableRow(cells []tableCell, widths []int, bar string) []string {
	wrapped := make([][][]glyph, len(widths))
	height := 1
	for i := range widths {
		if i < len(cells) {
			for _, l := range cells[i].lines {
				wrapped[i] = append(wrapped[i], wrapGlyphs(l, widths[i])...)
			}
		}
		height = max(height, len(wrapped[i]))
	}
	rows := make([]string, height)
	for k := range rows {
		var b strings.Builder
		b.WriteString(bar)
		for i, w := range widths {
			var line []glyph
			if k < len(wrapped[i]) {
				line = wrapped[i][k]
			}
			align := east.AlignNone
			if i < len(cells) {
				align = cells[i].align
			}
			b.WriteString(" " + pad(paint(line), w-widthOf(line), align) + " ")
			b.WriteString(bar)
		}
		rows[k] = b.String()
	}
	return rows
}

func pad(s string, gap int, align east.Alignment) string {
	gap = max(gap, 0)
	switch align {
	case east.AlignRight:
		return strings.Repeat(" ", gap) + s
	case east.AlignCenter:
		return strings.Repeat(" ", gap/2) + s + strings.Repeat(" ", gap-gap/2)
	}
	return s + strings.Repeat(" ", gap)
}

func narrowTable(grid [][]tableCell, width int) []string {
	sep := glyphs([]span{{" │ ", mdDim}})
	var rows []string
	for k, cells := range grid {
		var line []glyph
		for i, c := range cells {
			if i > 0 {
				line = append(line, sep...)
			}
			for j, l := range c.lines {
				if j > 0 {
					line = append(line, glyph{text: " ", width: 1, r: ' '})
				}
				line = append(line, l...)
			}
		}
		for _, row := range wrapGlyphs(line, width) {
			rows = append(rows, paint(row))
		}
		if k == 0 {
			rows = append(rows, mdDim.render(strings.Repeat("─", min(width, 80))))
		}
	}
	return rows
}
