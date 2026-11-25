package markdown

import (
	"strings"
	"testing"
)

func TestHTMLRendersGFMBlocks(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{
			name:   "heading and paragraph",
			source: "# Title\n\nSome text.",
			want:   []string{"<h1>Title</h1>", "<p>Some text.</p>"},
		},
		{
			name:   "table",
			source: "| a | b |\n|---|---|\n| 1 | 2 |",
			want:   []string{"<table>", "<th>a</th>", "<td>2</td>"},
		},
		{
			name:   "task list",
			source: "- [x] done\n- [ ] todo",
			want:   []string{`type="checkbox"`, "checked"},
		},
		{
			name:   "fenced code",
			source: "```go\nfmt.Println(1)\n```",
			want:   []string{`<pre><code class="language-go">`, "fmt.Println(1)"},
		},
		{
			name:   "strikethrough",
			source: "a ~~b~~ c",
			want:   []string{"<del>b</del>"},
		},
		{
			name:   "hard wrap keeps the newline",
			source: "a\nb",
			want:   []string{"a<br"},
		},
		{
			name:   "raw html is omitted",
			source: "<script>alert(1)</script>\n\nx",
			want:   []string{"x"},
			absent: []string{"<script>", "alert(1)"},
		},
		{
			name:   "inline html tags are omitted",
			source: "a <b>bold</b> c",
			want:   []string{"a ", "bold", " c"},
			absent: []string{"<b>", "</b>"},
		},
		{
			name:   "dangerous link destination is dropped",
			source: "[x](javascript:alert(1))",
			absent: []string{"javascript:"},
		},
		{
			name:   "text is escaped",
			source: "a < b & c",
			want:   []string{"a &lt; b &amp; c"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := HTML(c.source)
			for _, w := range c.want {
				if !strings.Contains(out, w) {
					t.Errorf("HTML(%q) = %q, missing %q", c.source, out, w)
				}
			}
			for _, a := range c.absent {
				if strings.Contains(out, a) {
					t.Errorf("HTML(%q) = %q, contains %q", c.source, out, a)
				}
			}
		})
	}
}

func TestHTMLEmphasisNextToCJK(t *testing.T) {
	cases := []struct{ source, want string }{
		{"**注意：**这个", "<strong>注意：</strong>这个"},
		{"这是**“引用”**吗", "<strong>“引用”</strong>"},
		{"**bold** text", "<strong>bold</strong>"},
	}
	for _, c := range cases {
		out := HTML(c.source)
		if !strings.Contains(out, c.want) {
			t.Errorf("HTML(%q) = %q, want %q", c.source, out, c.want)
		}
	}
	if out := HTML("snake_case_name"); strings.Contains(out, "<em>") {
		t.Errorf("underscores inside a word must stay literal, got %q", out)
	}
}

func TestIsCJK(t *testing.T) {
	wide := []rune{'中', 'あ', 'ア', '한', '，', '！', '（', 'Ａ'}
	for _, r := range wide {
		if !IsCJK(r) {
			t.Errorf("IsCJK(%q) = false, want true", r)
		}
	}
	narrow := []rune{'a', 'Z', '1', ' ', ',', '!', 'é'}
	for _, r := range narrow {
		if IsCJK(r) {
			t.Errorf("IsCJK(%q) = true, want false", r)
		}
	}
}
