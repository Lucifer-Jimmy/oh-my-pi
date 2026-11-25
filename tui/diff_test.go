package tui

import (
	"strings"
	"testing"
)

func ops(lines []diffLine) string {
	var b strings.Builder
	for _, l := range lines {
		b.WriteByte(l.op)
	}
	return b.String()
}

func TestLineDiffMarksOnlyTheChangedLines(t *testing.T) {
	before := "func a() {\n\tx := 1\n\ty := 2\n\treturn x + y\n}\n"
	after := "func a() {\n\tx := 1\n\ty := 3\n\treturn x + y\n}\n"
	got := lineDiff(before, after)
	if o := ops(got); o != "  -+  " {
		t.Fatalf("ops = %q, want one removal and one addition among context", o)
	}
	if got[2].text != "\ty := 2" || got[3].text != "\ty := 3" {
		t.Errorf("changed lines = %q, %q", got[2].text, got[3].text)
	}
}

func TestLineDiffHandlesInsertionsDeletionsAndCRLF(t *testing.T) {
	if o := ops(lineDiff("a\r\nb\r\nc\r\n", "a\nb\nnew\nc\n")); o != "  + " {
		t.Errorf("insertion ops = %q", o)
	}
	if o := ops(lineDiff("a\nold\nb\n", "a\nb\n")); o != " - " {
		t.Errorf("deletion ops = %q", o)
	}
	if o := ops(lineDiff("", "x\ny\n")); o != "++" {
		t.Errorf("creation ops = %q", o)
	}
	if o := ops(lineDiff("same\n", "same\n")); o != " " {
		t.Errorf("no-change ops = %q", o)
	}
}

func TestCollapseKeepsContextAroundChanges(t *testing.T) {
	var before, after []string
	for i := 0; i < 20; i++ {
		before = append(before, "line")
		after = append(after, "line")
	}
	after[10] = "changed"
	got := collapse(lineDiff(strings.Join(before, "\n"), strings.Join(after, "\n")), 2)
	if o := ops(got); o != "~  -+  ~" {
		t.Fatalf("collapsed ops = %q", o)
	}
	if got[0].text != "8 unchanged lines" || got[len(got)-1].text != "7 unchanged lines" {
		t.Errorf("skip markers = %q, %q", got[0].text, got[len(got)-1].text)
	}
}

func TestRenderDiffLimitsRowsAndPointsAtTheViewer(t *testing.T) {
	var lines []diffLine
	for i := 0; i < 30; i++ {
		lines = append(lines, diffLine{'+', "x"})
	}
	rows := renderDiff(lines, 80, 8, "    ", false)
	if len(rows) != 9 {
		t.Fatalf("rows = %d, want 8 and a marker", len(rows))
	}
	if !strings.Contains(rows[8], "+22 lines") || !strings.Contains(rows[8], "ctrl+o") {
		t.Errorf("marker = %q", rows[8])
	}
}
