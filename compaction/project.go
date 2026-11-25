package compaction

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const (
	tailLines   = 10
	tailLineCap = 200
)

func (c *Compactor) project(msgs []llm.Message, cp session.Compaction) ([]llm.Message, int) {
	start := 0
	if cp.Summary != "" {
		start = min(cp.FirstKept, len(msgs))
	}
	out, n := c.elideRange(msgs, start, len(msgs), cp.ElideBefore)
	if cp.Summary != "" {
		out = withSummary(out, cp.Summary)
	}
	return out, n
}

func (c *Compactor) elideRange(msgs []llm.Message, from, to, before int) ([]llm.Message, int) {
	end := min(before, to)
	names := toolNames(msgs[:max(end, 0)])
	out := make([]llm.Message, 0, to-from+1)
	n := 0
	for i := from; i < to; i++ {
		m := msgs[i]
		if i < end {
			var k int
			m, k = c.elide(m, names)
			n += k
		}
		out = append(out, m)
	}
	return out, n
}

func toolNames(msgs []llm.Message) map[string]string {
	names := map[string]string{}
	for _, m := range msgs {
		if m.Role != llm.RoleAssistant {
			continue
		}
		for _, b := range m.Content {
			if b.Type == llm.BlockToolUse {
				names[b.ID] = b.Name
			}
		}
	}
	return names
}

func (c *Compactor) elide(m llm.Message, names map[string]string) (llm.Message, int) {
	if m.Role != llm.RoleUser {
		return m, 0
	}
	var content []llm.ContentBlock
	n := 0
	for i, b := range m.Content {
		if b.Type != llm.BlockToolResult {
			continue
		}
		short, ok := c.elideResult(names[b.ToolUseID], b)
		if !ok {
			continue
		}
		if content == nil {
			content = slices.Clone(m.Content)
		}
		content[i] = short
		n++
	}
	if content != nil {
		m.Content = content
	}
	return m, n
}

func (c *Compactor) elideResult(tool string, b llm.ContentBlock) (llm.ContentBlock, bool) {
	size := estimateBlocks(b.Content)
	if size < c.minElide {
		return b, false
	}
	outcome := "succeeded"
	if b.IsError {
		outcome = "failed"
	}
	name := tool
	if name == "" {
		name = "tool"
	}
	text := resultText(b.Content)
	lines := countLines(text)
	var s string
	if c.reproducible[tool] {
		s = fmt.Sprintf(elidedReproducible, name, outcome, plural(lines, "line"), name)
	} else {
		tail := lastLines(text, tailLines)
		s = fmt.Sprintf(elidedOther, name, outcome, plural(lines, "line"), plural(countLines(tail), "line"))
		if tail != "" {
			s += "\n" + tail
		}
	}
	short := llm.ToolResultText(b.ToolUseID, s, b.IsError)
	if estimateBlocks(short.Content) >= size {
		return b, false
	}
	return short, true
}

func resultText(blocks []llm.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == llm.BlockText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func countLines(s string) int {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return ""
	}
	lines = lines[max(len(lines)-n, 0):]
	for i, l := range lines {
		lines[i] = clip(strings.TrimRight(l, "\r"), tailLineCap)
	}
	return strings.Join(lines, "\n")
}

func clip(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	head := limit / 2
	for head > 0 && !utf8.RuneStart(s[head]) {
		head--
	}
	tail := limit - head
	start := len(s) - tail
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[:head] + fmt.Sprintf("... (%d more bytes) ...", start-head) + s[start:]
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

func withSummary(view []llm.Message, summary string) []llm.Message {
	block := llm.TextBlock(fmt.Sprintf(summaryContext, summary))
	if len(view) > 0 && view[0].Role == llm.RoleUser {
		first := view[0]
		first.Content = append([]llm.ContentBlock{block}, first.Content...)
		view[0] = first
		return view
	}
	return append([]llm.Message{{Role: llm.RoleUser, Content: []llm.ContentBlock{block}}}, view...)
}

func elideBoundary(msgs []llm.Message, keep int) int {
	sum := 0
	for i := len(msgs) - 1; i > 0; i-- {
		sum += estimateMessage(msgs[i])
		if sum >= keep {
			return i
		}
	}
	return 0
}

func cutPoint(msgs []llm.Message, from, upTo int) int {
	for i := min(upTo, len(msgs)-1); i > from; i-- {
		if startsTurn(msgs[i]) {
			return i
		}
	}
	return 0
}

func startsTurn(m llm.Message) bool {
	if m.Role == llm.RoleAssistant {
		return true
	}
	return !slices.ContainsFunc(m.Content, func(b llm.ContentBlock) bool { return b.Type == llm.BlockToolResult })
}
