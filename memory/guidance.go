package memory

import (
	"strings"
	"unicode/utf8"
)

const injectionTokens = 5000

var blockText = strings.NewReplacer("<", "&lt;", ">", "&gt;")

const guidanceRules = `Memory from earlier sessions in this project. It is advice about process, not proof of current state:
- The repository, command output and the user's instructions win over memory. Memory that disagrees with them is stale: follow the current state.
- Before relying on a remembered fact, check it against the repository; memory alone never proves anything.
- When memory changes your plan, say which part of it did.
- Save a new durable lesson with the learn tool when you discover one; it applies from the next session.`

func Guidance(summary, lessons string) string {
	summary = strings.TrimSpace(summary)
	lessons = strings.TrimSpace(lessons)
	if summary == "" && lessons == "" {
		return ""
	}
	sum := truncateTokens(summary, injectionTokens)
	left := injectionTokens - (len(sum)+3)/4
	les := ""
	if lessons != "" && left > 0 {
		les = truncateTokens(lessons, left)
	}
	if sum == "" && les == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("<memory>\n")
	b.WriteString(guidanceRules)
	if sum != "" {
		b.WriteString("\n\n## Summary of earlier sessions\n")
		b.WriteString(blockText.Replace(sum))
	}
	if les != "" {
		b.WriteString("\n\n## Lessons saved with learn (durable, but may be stale)\n")
		b.WriteString(blockText.Replace(les))
	}
	b.WriteString("\n</memory>")
	return b.String()
}

const truncatedMark = "\n[… truncated …]\n"

func truncateTokens(s string, tokens int) string {
	if tokens <= 0 {
		return ""
	}
	limit := tokens * 4
	if len(s) <= limit {
		return s
	}
	head := runeCut(s, limit*6/10, false)
	tail := runeCut(s, limit-len(head)-len(truncatedMark), true)
	return head + truncatedMark + tail
}

func runeCut(s string, n int, fromEnd bool) string {
	if n <= 0 {
		return ""
	}
	if n >= len(s) {
		return s
	}
	if fromEnd {
		i := len(s) - n
		for i < len(s) && !utf8.RuneStart(s[i]) {
			i++
		}
		return s[i:]
	}
	i := n
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return s[:i]
}
