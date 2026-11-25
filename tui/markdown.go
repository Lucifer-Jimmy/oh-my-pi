package tui

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	textm "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	mdparse "github.com/Lucifer-Jimmy/oh-my-pi/markdown"
)

type mdStyle uint8

const (
	mdBold mdStyle = 1 << iota
	mdItalic
	mdStrike
	mdUnderline
	mdCode
	mdLink
	mdAccent
	mdDim
)

func (s mdStyle) style() lipgloss.Style {
	st := lipgloss.NewStyle().
		Bold(s&mdBold != 0).
		Italic(s&mdItalic != 0).
		Strikethrough(s&mdStrike != 0).
		Underline(s&mdUnderline != 0)
	switch {
	case s&mdCode != 0:
		st = st.Foreground(styleCode.GetForeground())
	case s&(mdLink|mdAccent) != 0:
		st = st.Foreground(styleAccent.GetForeground())
	case s&mdDim != 0:
		st = st.Foreground(styleDim.GetForeground())
	}
	return st
}

func (s mdStyle) render(text string) string {
	if text == "" {
		return text
	}
	a := mdAffixes[s]
	return a[0] + text + a[1]
}

var (
	mdAffixes = func() (t [256][2]string) {
		for i := range t {
			out := mdStyle(i).style().Render("x")
			at := strings.LastIndexByte(out, 'x')
			t[i] = [2]string{out[:at], out[at+1:]}
		}
		return t
	}()
	markdownParser = mdparse.Parser()
	reBreakTag     = regexp.MustCompile(`(?i)^<br\s*/?>$`)
	bullets        = []string{"•", "◦", "▪"}
)

func renderMarkdown(source string, width int) []string {
	rows, _, _ := renderBlocks(source, width)
	return rows
}

func renderBlocks(source string, width int) (rows []string, startsWithHeading, endsWithHeading bool) {
	src := []byte(source)
	md := &markdown{src: src}
	doc := markdownParser.Parse(textm.NewReader(src))
	return md.blocks(doc, max(width, 1), 0), isHeading(doc.FirstChild()), isHeading(doc.LastChild())
}

func isHeading(n ast.Node) bool {
	_, ok := n.(*ast.Heading)
	return ok
}

func stableCut(s string) int {
	end := strings.LastIndexByte(s, '\n') + 1
	if end == 0 {
		return 0
	}
	src := []byte(s[:end])
	last := markdownParser.Parse(textm.NewReader(src)).LastChild()
	if last == nil || last.PreviousSibling() == nil {
		return 0
	}
	at := last.Pos()
	if at <= 0 || at > len(src) {
		return 0
	}
	cut := bytes.LastIndexByte(src[:at], '\n') + 1
	for cut > 0 {
		prev := bytes.LastIndexByte(src[:cut-1], '\n') + 1
		if len(bytes.TrimSpace(src[prev:cut])) > 0 {
			break
		}
		cut = prev
	}
	return cut
}

type markdown struct {
	src  []byte
	base mdStyle
}

func (md *markdown) blocks(parent ast.Node, width, depth int) []string {
	var rows []string
	var prev ast.Node
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		block := md.block(n, width, depth)
		if len(block) == 0 {
			continue
		}
		if len(rows) > 0 && (md.gapBefore(n) || isHeading(prev) || isHeading(n)) {
			rows = append(rows, "")
		}
		rows = append(rows, block...)
		prev = n
	}
	return rows
}

func (md *markdown) gapBefore(n ast.Node) bool {
	t, ok := n.(*east.Table)
	if !ok || t.FirstChild() == nil {
		return n.HasBlankPreviousLines()
	}
	at := t.FirstChild().Pos()
	if at <= 0 || at > len(md.src) {
		return false
	}
	start := bytes.LastIndexByte(md.src[:at], '\n') + 1
	if start == 0 {
		return false
	}
	prev := bytes.LastIndexByte(md.src[:start-1], '\n') + 1
	return len(bytes.Trim(md.src[prev:start], " \t\r\n>")) == 0
}

func (md *markdown) block(n ast.Node, width, depth int) []string {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return md.prose(n, width, 0)
	case *ast.Heading:
		return md.heading(n, width)
	case *ast.ThematicBreak:
		return []string{mdDim.render(strings.Repeat("─", min(width, 80)))}
	case *ast.FencedCodeBlock:
		return md.code(n, clean(string(n.Language(md.src))), width)
	case *ast.CodeBlock:
		return md.code(n, "", width)
	case *ast.Blockquote:
		return md.quote(n, width, depth)
	case *ast.List:
		return md.list(n, width, depth)
	case *east.Table:
		return md.table(n, width)
	case *ast.HTMLBlock:
		return md.html(n, width)
	}
	return md.blocks(n, width, depth)
}

func (md *markdown) heading(n *ast.Heading, width int) []string {
	style := md.base | mdBold | mdAccent
	if n.Level == 1 {
		style |= mdUnderline
	}
	var spans []span
	if n.Level >= 3 {
		spans = append(spans, span{strings.Repeat("#", n.Level) + " ", style})
	}
	md.inline(n, style, &spans)
	return layout(spans, width)
}

func (md *markdown) prose(n ast.Node, width int, style mdStyle) []string {
	var spans []span
	md.inline(n, md.base|style, &spans)
	return layout(spans, width)
}

func (md *markdown) code(n ast.Node, lang string, width int) []string {
	rows := []string{mdDim.render(ansi.Truncate("```"+lang, width, "..."))}
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		rows = append(rows, codeRows(strings.TrimRight(clean(string(seg.Value(md.src))), "\n"), width)...)
	}
	return append(rows, mdDim.render("```"))
}

func codeRows(line string, width int) []string {
	parts := hardWrap(line, width)
	if len(parts) == 1 || width <= 4 {
		rows := make([]string, len(parts))
		for i, p := range parts {
			rows[i] = mdCode.render(p)
		}
		return rows
	}
	rows := []string{mdCode.render(parts[0])}
	for _, p := range hardWrap(strings.Join(parts[1:], ""), width-2) {
		rows = append(rows, mdDim.render("↳ ")+mdCode.render(p))
	}
	return rows
}

func (md *markdown) quote(n ast.Node, width, depth int) []string {
	bar := mdDim.render("│")
	base := md.base
	md.base |= mdDim
	inner := md.blocks(n, max(width-2, 1), depth)
	md.base = base
	rows := make([]string, len(inner))
	for i, row := range inner {
		if row == "" {
			rows[i] = bar
			continue
		}
		rows[i] = bar + " " + row
	}
	return rows
}

func (md *markdown) list(n *ast.List, width, depth int) []string {
	digits := len(strconv.Itoa(n.Start + n.ChildCount() - 1))
	var rows []string
	i := 0
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		marker := mdAccent.render(bullets[depth%len(bullets)])
		if n.IsOrdered() {
			marker = mdAccent.render(fmt.Sprintf("%*d%c", digits, n.Start+i, n.Marker))
		}
		if box := taskBox(item); box != nil {
			marker = checkbox(box.IsChecked)
			box.Parent().RemoveChild(box.Parent(), box)
		}
		hang := ansi.StringWidth(marker) + 1
		body := md.blocks(item, max(width-hang, 1), depth+1)
		if i > 0 && item.HasBlankPreviousLines() {
			rows = append(rows, "")
		}
		if len(body) == 0 {
			body = []string{""}
		}
		for j, row := range body {
			switch {
			case j == 0:
				rows = append(rows, strings.TrimRight(marker+" "+row, " "))
			case row == "":
				rows = append(rows, "")
			default:
				rows = append(rows, strings.Repeat(" ", hang)+row)
			}
		}
		i++
	}
	return rows
}

func taskBox(item ast.Node) *east.TaskCheckBox {
	first := item.FirstChild()
	if first == nil {
		return nil
	}
	box, _ := first.FirstChild().(*east.TaskCheckBox)
	return box
}

func checkbox(checked bool) string {
	if checked {
		return styleOK.Render("[x]")
	}
	return styleDim.Render("[ ]")
}

func (md *markdown) html(n *ast.HTMLBlock, width int) []string {
	var rows []string
	add := func(seg textm.Segment) {
		line := strings.TrimRight(clean(string(seg.Value(md.src))), "\n")
		for _, row := range hardWrap(line, width) {
			rows = append(rows, mdDim.render(row))
		}
	}
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		add(lines.At(i))
	}
	if n.HasClosure() {
		add(n.ClosureLine)
	}
	return rows
}

func (md *markdown) inline(n ast.Node, style mdStyle, out *[]span) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			*out = append(*out, span{unescape(c.Segment.Value(md.src)), style})
			if c.SoftLineBreak() || c.HardLineBreak() {
				*out = append(*out, span{"\n", style})
			}
		case *ast.String:
			*out = append(*out, span{string(c.Value), style})
		case *ast.CodeSpan:
			*out = append(*out, span{md.codeSpan(c), style | mdCode})
		case *ast.Emphasis:
			if c.Level >= 2 {
				md.inline(c, style|mdBold, out)
			} else {
				md.inline(c, style|mdItalic, out)
			}
		case *east.Strikethrough:
			md.inline(c, style|mdStrike, out)
		case *ast.Link:
			md.link(c, string(c.Destination), style, out)
		case *ast.Image:
			md.link(c, string(c.Destination), style, out)
		case *ast.AutoLink:
			*out = append(*out, span{string(c.Label(md.src)), style | mdLink})
		case *ast.RawHTML:
			md.rawHTML(c, style, out)
		case *east.TaskCheckBox:
			*out = append(*out, span{ansi.Strip(checkbox(c.IsChecked)) + " ", style})
		default:
			md.inline(c, style, out)
		}
	}
}

func (md *markdown) link(n ast.Node, dest string, style mdStyle, out *[]span) {
	start := len(*out)
	md.inline(n, style|mdLink, out)
	var label strings.Builder
	for _, s := range (*out)[start:] {
		label.WriteString(s.text)
	}
	text := strings.TrimSpace(label.String())
	switch {
	case dest == "" || dest == text || strings.TrimPrefix(dest, "mailto:") == text:
	case text == "":
		*out = append(*out, span{dest, style | mdLink})
	default:
		*out = append(*out, span{" (" + dest + ")", mdDim})
	}
}

func (md *markdown) codeSpan(n *ast.CodeSpan) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch c := c.(type) {
		case *ast.Text:
			v := c.Segment.Value(md.src)
			if bytes.HasSuffix(v, []byte("\n")) {
				b.Write(v[:len(v)-1])
				b.WriteByte(' ')
				continue
			}
			b.Write(v)
		case *ast.String:
			b.Write(c.Value)
		}
	}
	return b.String()
}

func (md *markdown) rawHTML(n *ast.RawHTML, style mdStyle, out *[]span) {
	var b strings.Builder
	for i := 0; i < n.Segments.Len(); i++ {
		seg := n.Segments.At(i)
		b.Write(seg.Value(md.src))
	}
	if reBreakTag.MatchString(b.String()) {
		*out = append(*out, span{"\n", style})
		return
	}
	*out = append(*out, span{b.String(), style})
}

func unescape(b []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(b))))
}
