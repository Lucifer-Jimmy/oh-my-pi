package markdown

import (
	"bytes"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer/html"
	textm "github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var (
	parserInstance = newMarkdown().Parser()
	htmlRenderer   = newMarkdown(goldmark.WithRendererOptions(html.WithHardWraps()))
)

func newMarkdown(opts ...goldmark.Option) goldmark.Markdown {
	options := []goldmark.Option{
		goldmark.WithParser(newParser()),
		goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList),
	}
	return goldmark.New(append(options, opts...)...)
}

func Parser() parser.Parser { return parserInstance }

func HTML(source string) string {
	var b bytes.Buffer
	if err := htmlRenderer.Convert([]byte(source), &b); err != nil {
		return ""
	}
	return b.String()
}

func IsCJK(r rune) bool {
	switch {
	case r >= 0x3000 && r <= 0x303f, r >= 0xfe10 && r <= 0xfe1f, r >= 0xfe30 && r <= 0xfe4f, r >= 0xff00 && r <= 0xffef:
		return true
	}
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
}

func newParser() parser.Parser {
	inlines := parser.DefaultInlineParsers()
	for i := range inlines {
		if inlines[i].Value == any(parser.NewEmphasisParser()) {
			inlines[i].Value = emphasisParser{}
		}
	}
	md := goldmark.New(
		goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(parser.DefaultBlockParsers()...),
			parser.WithInlineParsers(inlines...),
			parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
		)),
		goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList),
	)
	return md.Parser()
}

type emphasisParser struct{}

func (emphasisParser) Trigger() []byte { return []byte{'*', '_'} }

func (emphasisParser) Parse(_ ast.Node, block textm.Reader, pc parser.Context) ast.Node {
	before := block.PrecendingCharacter()
	line, seg := block.PeekLine()
	d := emphasisDelimiter(line, before)
	d.Segment = seg.WithStop(seg.Start + d.OriginalLength)
	block.Advance(d.OriginalLength)
	pc.PushDelimiter(d)
	return d
}

type emphasisDelimiters struct{}

func (emphasisDelimiters) IsDelimiter(b byte) bool { return b == '*' || b == '_' }

func (emphasisDelimiters) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (emphasisDelimiters) OnMatch(consumes int) ast.Node { return ast.NewEmphasis(consumes) }

func emphasisDelimiter(line []byte, before rune) *parser.Delimiter {
	c := line[0]
	n := 1
	for n < len(line) && line[n] == c {
		n++
	}
	after := ' '
	if n < len(line) {
		after = util.ToRune(line, n)
	}
	beforeSpace, afterSpace := util.IsSpaceRune(before), util.IsSpaceRune(after)
	beforePunct, afterPunct := util.IsPunctRune(before), util.IsPunctRune(after)
	if c == '*' {
		beforePunct = beforePunct && !IsCJK(before)
		afterPunct = afterPunct && !IsCJK(after)
	}
	left := !afterSpace && (!afterPunct || beforeSpace || beforePunct || IsCJK(before))
	right := !beforeSpace && (!beforePunct || afterSpace || afterPunct || IsCJK(after))
	canOpen, canClose := left, right
	if c == '_' {
		canOpen = left && (!right || beforePunct)
		canClose = right && (!left || afterPunct)
	}
	return parser.NewDelimiter(canOpen, canClose, n, c, emphasisDelimiters{})
}
