package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plain(s string) string { return ansi.Strip(s) }

func plainMarkdown(src string, width int) []string {
	rows := renderMarkdown(src, width)
	for i, r := range rows {
		rows[i] = plain(r)
	}
	return rows
}

func TestMarkdownInlineText(t *testing.T) {
	cases := []struct{ src, want string }{
		{"run **go test** and *read* `main.go` ~~now~~", "run go test and read main.go now"},
		{"**注意：**这个参数必填", "注意：这个参数必填"},
		{"这是**“引用”**吗", "这是“引用”吗"},
		{"中文**（括号）**中文", "中文（括号）中文"},
		{"中文**\"quote\"**中文", "中文\"quote\"中文"},
		{`\*not emphasis\* &amp; &#x4e2d;`, "*not emphasis* & 中"},
		{"snake_case_name stays", "snake_case_name stays"},
		{"#42 is fixed", "#42 is fixed"},
		{"see [docs](https://x/y.md) and [a.go](a.go)", "see docs (https://x/y.md) and a.go"},
		{"mail <a@b.co> or <https://x.dev>", "mail a@b.co or https://x.dev"},
		{"line one<br>line two", "line one\nline two"},
		{"`a **b** c`", "a **b** c"},
	}
	for _, c := range cases {
		if got := strings.Join(plainMarkdown(c.src, 80), "\n"); got != c.want {
			t.Errorf("%q rendered %q, want %q", c.src, got, c.want)
		}
	}
}

func TestMarkdownStylesAreApplied(t *testing.T) {
	for _, src := range []string{"**注意：**这个", "*italic*", "~~gone~~", "`code`", "# Title"} {
		rows := renderMarkdown(src, 80)
		if len(rows) != 1 || rows[0] == plain(rows[0]) {
			t.Errorf("%q must render one styled row, got %q", src, rows)
		}
	}
}

func TestMarkdownBlockStyles(t *testing.T) {
	code := renderMarkdown("```\nx := 1\n\ny := 2\n```", 80)
	if len(code) != 5 || code[1] == plain(code[1]) || code[2] != "" {
		t.Errorf("code lines are colored and blank lines stay empty: %q", code)
	}
	quote := renderMarkdown("> quoted **text**", 80)
	if len(quote) != 1 || !strings.Contains(quote[0], mdDim.render("quoted ")) {
		t.Errorf("quote text is dimmed: %q", quote)
	}
	list := renderMarkdown("- item\n\n1. first", 80)
	if len(list) != 3 || !strings.HasPrefix(list[0], mdAccent.render("•")) || !strings.HasPrefix(list[2], mdAccent.render("1.")) {
		t.Errorf("list markers are colored: %q", list)
	}
}

func TestMarkdownHeadings(t *testing.T) {
	got := plainMarkdown("# Title\n## Section\n### Detail\ntext\n### 1. Steps\n1. first", 80)
	want := []string{"Title", "", "Section", "", "### Detail", "", "text", "", "### 1. Steps", "", "1. first"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	cases := map[string]string{"# a": mdStyle(mdBold | mdAccent | mdUnderline).render("a"), "## a": mdStyle(mdBold | mdAccent).render("a"), "### a": mdStyle(mdBold | mdAccent).render("### a")}
	for src, want := range cases {
		if row := renderMarkdown(src, 80)[0]; row != want {
			t.Errorf("%q must be bold in the accent colour: %q, want %q", src, row, want)
		}
	}
}

func TestMarkdownLists(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"- one\n- two\n  - nested\n    - deeper", []string{"• one", "• two", "  ◦ nested", "    ▪ deeper"}},
		{"8. a\n9. b\n10. c", []string{" 8. a", " 9. b", "10. c"}},
		{"1. a\n1. b\n1. c", []string{"1. a", "2. b", "3. c"}},
		{"1. a\n\n2. b", []string{"1. a", "", "2. b"}},
		{"- [ ] todo\n- [x] done", []string{"[ ] todo", "[x] done"}},
		{"intro:\n- a\n- b", []string{"intro:", "• a", "• b"}},
	}
	for _, c := range cases {
		if got := plainMarkdown(c.src, 80); strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%q rendered %q, want %q", c.src, got, c.want)
		}
	}
}

func TestMarkdownListItemsHangUnderTheirText(t *testing.T) {
	rows := plainMarkdown("- "+strings.Repeat("long text ", 8), 20)
	if len(rows) < 2 || !strings.HasPrefix(rows[0], "• long") {
		t.Fatalf("a long item must wrap after its bullet: %q", rows)
	}
	for _, r := range rows[1:] {
		if !strings.HasPrefix(r, "  ") || strings.HasPrefix(r, "   ") {
			t.Errorf("continuation rows hang two cells in: %q", r)
		}
	}
}

func TestMarkdownTableFitsItsContent(t *testing.T) {
	got := plainMarkdown("| a | b |\n|---|--:|\n| 1 | 22 |", 80)
	want := []string{
		"┌───┬────┐",
		"│ a │  b │",
		"├───┼────┤",
		"│ 1 │ 22 │",
		"└───┴────┘",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestMarkdownTableSpacingFollowsTheSource(t *testing.T) {
	spaced := plainMarkdown("```\nx\n```\n\n| a |\n|---|\n| 1 |", 80)
	if len(spaced) < 5 || spaced[2] != "```" || spaced[3] != "" || !strings.HasPrefix(spaced[4], "┌") {
		t.Errorf("a blank line before a table is kept: %q", spaced)
	}
	tight := plainMarkdown("intro\n| a |\n|---|\n| 1 |", 80)
	if len(tight) < 2 || tight[0] != "intro" || !strings.HasPrefix(tight[1], "┌") {
		t.Errorf("a table right under a line stays attached: %q", tight)
	}
}

func TestMarkdownTableWrapsWideCellsWithinTheWidth(t *testing.T) {
	src := "| 名称 | 说明 |\n|---|---|\n| pigo | 每个事件一个独立类型，靠封闭接口区分，序列化时要按类型字段手写解码 |\n| omp | 一个扁平结构体<br>靠 Kind 区分 |"
	rows := plainMarkdown(src, 30)
	if len(rows) < 8 {
		t.Fatalf("the long cell must wrap onto several rows: %q", rows)
	}
	w := ansi.StringWidth(rows[0])
	for _, r := range rows {
		if ansi.StringWidth(r) != w || w > 30 {
			t.Errorf("every row must be %d cells and fit 30: %d %q", w, ansi.StringWidth(r), r)
		}
	}
	joined := strings.Join(rows, "\n")
	for _, want := range []string{"名称", "pigo", "omp", "靠 Kind 区分", "├"} {
		if !strings.Contains(joined, want) {
			t.Errorf("table lost %q:\n%s", want, joined)
		}
	}
}

func TestFitColumnsSpendsWidthWhereItSavesRows(t *testing.T) {
	lines := [][][]int{{{4}, {3}, {4}}, {{22}, {2}, {90}}, {{14}, {27}, {29}}}
	got := fitColumns(lines, []int{22, 27, 90}, []int{22, 14, 8}, []int{1, 2, 2}, 68)
	if !slices.Equal(got, []int{22, 16, 30}) {
		t.Errorf("widths %v, want [22 16 30]: the long column takes the room, the medium one wraps once", got)
	}
	if h := tableHeight(lines, got); h != 6 {
		t.Errorf("table body is %d rows, want 6", h)
	}
}

func TestMarkdownTableKeepsShortCellsWhole(t *testing.T) {
	src := "| 字段 | 含义 | 默认值 |\n|---|---|---|\n| `MaxTurns` | 最大轮数 | 0（不限制） |\n| `TokenBudget` | token 预算，超过后停止运行并返回 `token_budget` | 0 |"
	wide := strings.Join(plainMarkdown(src, 60), "\n")
	for _, want := range []string{"│ 默认值      │", "│ 0（不限制） │"} {
		if !strings.Contains(wide, want) {
			t.Errorf("at 60 columns a short cell must stay on one line, missing %q:\n%s", want, wide)
		}
	}
	narrow := plainMarkdown(src, 40)
	joined := strings.Join(narrow, "\n")
	if !strings.Contains(joined, "│ 默认值  │") || !strings.Contains(joined, "TokenBudget") {
		t.Errorf("at 40 columns the long column gives up width first and words stay whole:\n%s", joined)
	}
	for _, r := range narrow {
		if ansi.StringWidth(r) > 40 {
			t.Errorf("row wider than 40: %q", r)
		}
	}
}

func TestMarkdownLongWordsBreakAfterASeparator(t *testing.T) {
	rows := plainMarkdown("see https://github.com/golang/go/issues/12345678901234567890 now", 40)
	if len(rows) < 2 || rows[1] != "https://github.com/golang/go/issues/" || !strings.HasPrefix(rows[2], "12345678901234567890") {
		t.Errorf("a URL must break after its last slash: %q", rows)
	}
	path := plainMarkdown("`C:\\Code\\oh-my-pi\\agent\\scheduler_internal_dispatch_queue_test.go:142`", 40)
	if len(path) < 2 || path[0] != `C:\Code\oh-my-pi\agent\scheduler_` {
		t.Errorf("a path must break at a separator: %q", path)
	}
	if strings.Join(path, "") != `C:\Code\oh-my-pi\agent\scheduler_internal_dispatch_queue_test.go:142` {
		t.Errorf("breaking lost text: %q", path)
	}
	solid := plainMarkdown(strings.Repeat("x", 50), 20)
	if len(solid) != 3 || solid[0] != strings.Repeat("x", 20) {
		t.Errorf("a run without separators breaks at the width: %q", solid)
	}
}

func TestMarkdownWrappedCodeLinesAreMarked(t *testing.T) {
	line := "if err := r.ctx.Err(); err != nil { r.finish(ReasonAborted, err.Error()); return true }"
	rows := plainMarkdown("```go\n"+line+"\nreturn false\n```", 40)
	if len(rows) < 5 || rows[1] != line[:40] || !strings.HasPrefix(rows[2], "↳ ") {
		t.Fatalf("a wrapped code line continues on a marked row: %q", rows)
	}
	var joined strings.Builder
	for _, r := range rows[1 : len(rows)-2] {
		joined.WriteString(strings.TrimPrefix(r, "↳ "))
	}
	if joined.String() != line {
		t.Errorf("wrapping lost code: %q", joined.String())
	}
	if rows[len(rows)-2] != "return false" {
		t.Errorf("the next line is not a continuation: %q", rows[len(rows)-2])
	}
	for _, r := range rows {
		if ansi.StringWidth(r) > 40 {
			t.Errorf("row wider than 40: %q", r)
		}
	}
}

func TestMarkdownTableFallsBackWhenTooNarrow(t *testing.T) {
	src := "| a | b | c | d | e |\n|---|---|---|---|---|\n| 1 | 2 | 3 | 4 | 5 |"
	for _, r := range plainMarkdown(src, 12) {
		if ansi.StringWidth(r) > 12 {
			t.Errorf("row wider than 12: %q", r)
		}
	}
}

func TestMarkdownCodeBlockKeepsItsTextLiteral(t *testing.T) {
	got := plainMarkdown("```go\nfunc main() {\n\t**x** := 1\n}\n```", 80)
	want := []string{"```go", "func main() {", "    **x** := 1", "}", "```"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
	for _, r := range plainMarkdown("```\n"+strings.Repeat("x", 50)+"\n```", 20) {
		if ansi.StringWidth(r) > 20 {
			t.Errorf("code rows must wrap at the width: %q", r)
		}
	}
}

func TestMarkdownQuoteAndRule(t *testing.T) {
	got := plainMarkdown("> **note** this\n> second\n\n---\n\nafter", 10)
	want := []string{"│ note", "│ this", "│ second", "", "──────────", "", "after"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMarkdownWrapsCJKWithoutSpaces(t *testing.T) {
	src := "我们这边没有那十四个独立事件类型，只用一个扁平结构体，靠类型字段区分九种事件。消费方逐个拉取。"
	rows := plainMarkdown(src, 20)
	if len(rows) < 4 {
		t.Fatalf("expected the paragraph to wrap: %q", rows)
	}
	for _, r := range rows {
		if ansi.StringWidth(r) > 20 {
			t.Errorf("row wider than 20: %q", r)
		}
		if strings.HasPrefix(r, "，") || strings.HasPrefix(r, "。") {
			t.Errorf("a row must not start with closing punctuation: %q", r)
		}
	}
	if strings.Join(rows, "") != src {
		t.Errorf("wrapping lost text: %q", strings.Join(rows, ""))
	}
	if mixed := plainMarkdown("a 中文中文中文中文中文", 10); mixed[0] != "a 中文中文" {
		t.Errorf("CJK text must fill the row instead of breaking at the last space: %q", mixed)
	}
}

func TestStableCut(t *testing.T) {
	cases := []struct{ src, want string }{
		{"one paragraph\nstill going\n", ""},
		{"first\n\nsecond\n", "first\n"},
		{"first\n\nsecond", ""},
		{"# Title\ntext\n", "# Title\n"},
		{"```go\na\n\nb\n", ""},
		{"```go\na\n```\n\nafter\n", "```go\na\n```\n"},
		{"- a\n- b\n\n- c\n", ""},
		{"- a\n\ntext\n", "- a\n"},
		{"| a | b |\n|---|---|\n| 1 | 2 |\n", ""},
		{"intro\n\n| a | b |\n|---|---|\n", "intro\n"},
		{"para\n> quote\n", "para\n"},
	}
	for _, c := range cases {
		if got := c.src[:stableCut(c.src)]; got != c.want {
			t.Errorf("stableCut(%q) commits %q, want %q", c.src, got, c.want)
		}
	}
}

func TestStreamingCommitsTheSameRowsAsTheWholeDocument(t *testing.T) {
	doc := "## 计划\n\n先读 **代码**，再改。\n第二行紧跟着。\n\n1. 第一步\n2. 第二步\n\n   补充说明\n\n```go\nfunc main() {\n\n}\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n> 引用\n\n- [ ] 收尾\n\n结束。\n"
	for _, doc := range []string{doc, tightDoc} {
		want, _ := textRows(doc, flow{}, 60)
		var got []string
		offset, f, commits := 0, flow{}, 0
		for i := range doc {
			chunk := doc[offset:i]
			cut := stableCut(chunk)
			if cut == 0 {
				continue
			}
			rows, next := textRows(chunk[:cut], f, 60)
			if len(rows) > 0 {
				got = append(got, rows...)
				f = next
				commits++
			}
			offset += cut
		}
		rest, _ := textRows(doc[offset:], f, 60)
		got = append(got, rest...)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("streamed:\n%s\nwhole:\n%s", plain(strings.Join(got, "\n")), plain(strings.Join(want, "\n")))
		}
		if commits < 5 {
			t.Errorf("finished blocks must be committed while streaming, got %d commits", commits)
		}
	}
}

const tightDoc = "## 计划\n先读 **代码**。\n### 1. 步骤\n1. 第一步\n2. 第二步\n### 2. 验证\n```go\nx := 1\n```\n## 结论\n完成。\n"

func TestTextRowsKeepTheGutter(t *testing.T) {
	rows, _ := textRows("- "+strings.Repeat("long text ", 12), flow{}, 40)
	if len(rows) < 3 || rows[0] != "" || !strings.HasPrefix(plain(rows[1]), "● • long") {
		t.Fatalf("first row carries the turn bullet and the list bullet: %q", rows)
	}
	for _, r := range rows[2:] {
		if !strings.HasPrefix(plain(r), "    ") {
			t.Errorf("continuation keeps the gutter and the hang: %q", plain(r))
		}
		if w := ansi.StringWidth(r); w > 40 {
			t.Errorf("row wider than the terminal: %d %q", w, plain(r))
		}
	}
}
