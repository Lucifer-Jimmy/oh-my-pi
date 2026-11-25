package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const maxDiffMemo = 32

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all"`
}

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type bashArgs struct {
	Command string `json:"command"`
}

type todoArgs struct {
	Items []struct {
		Content string `json:"content"`
		Status  string `json:"status"`
	} `json:"items"`
}

func (m *model) inputRows(name string, input json.RawMessage, width, limit int, wrap bool, lead string) []string {
	if len(input) == 0 {
		return nil
	}
	switch name {
	case "edit":
		var a editArgs
		if json.Unmarshal(input, &a) == nil && (a.OldString != "" || a.NewString != "") {
			rows := renderDiff(m.editDiff(input, a), width, limit, lead, wrap)
			if a.ReplaceAll {
				rows = append([]string{lead + styleWarn.Render("replaces every occurrence")}, rows...)
			}
			return rows
		}
	case "write":
		var a writeArgs
		if json.Unmarshal(input, &a) == nil {
			var lines []diffLine
			for _, l := range diffInput(a.Content) {
				lines = append(lines, diffLine{'+', l})
			}
			if len(lines) == 0 {
				return []string{lead + styleDim.Render("(empty file)")}
			}
			return renderDiff(lines, width, limit, lead, wrap)
		}
	case "bash":
		var a bashArgs
		if json.Unmarshal(input, &a) == nil && strings.TrimSpace(a.Command) != "" {
			var rows []string
			for i, l := range diffInput(strings.TrimSpace(a.Command)) {
				sign := "  "
				if i == 0 {
					sign = "$ "
				}
				rows = append(rows, fitRows(lead, sign+clean(l), width, wrap, styleCode)...)
			}
			return limitRows(rows, limit, lead)
		}
	case "todo":
		var a todoArgs
		if json.Unmarshal(input, &a) == nil && len(a.Items) > 0 {
			var rows []string
			for _, it := range a.Items {
				box := "☐ "
				switch it.Status {
				case "completed":
					box = "☑ "
				case "in_progress":
					box = "▶ "
				}
				rows = append(rows, fitRows(lead, box+clean(it.Content), width, wrap, stylePlain)...)
			}
			return limitRows(rows, limit, lead)
		}
	case "exit_plan_mode":
		return nil
	}
	var rows []string
	for _, l := range describeInput(input) {
		rows = append(rows, fitRows(lead, clean(l), width, wrap, styleDim)...)
	}
	return limitRows(rows, limit, lead)
}

func (m *model) editDiff(input json.RawMessage, a editArgs) []diffLine {
	key := string(input)
	if lines, ok := m.diffMemo[key]; ok {
		return lines
	}
	if m.diffMemo == nil || len(m.diffMemo) >= maxDiffMemo {
		m.diffMemo = map[string][]diffLine{}
	}
	lines := collapse(lineDiff(a.OldString, a.NewString), diffContext)
	m.diffMemo[key] = lines
	return lines
}

func fitRows(lead, text string, width int, wrap bool, style lipgloss.Style) []string {
	room := max(width-ansi.StringWidth(lead), 4)
	if !wrap {
		return []string{lead + style.Render(ansi.Truncate(text, room, "…"))}
	}
	var rows []string
	for _, part := range strings.Split(ansi.Hardwrap(text, room, true), "\n") {
		rows = append(rows, lead+style.Render(part))
	}
	return rows
}

func describeInput(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return diffInput(string(raw))
	}
	var lines []string
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		var value json.RawMessage
		if err != nil || !ok || dec.Decode(&value) != nil {
			return diffInput(string(raw))
		}
		var s string
		if json.Unmarshal(value, &s) != nil {
			var compact bytes.Buffer
			if json.Compact(&compact, value) == nil {
				s = compact.String()
			} else {
				s = string(value)
			}
		}
		parts := diffInput(s)
		if len(parts) == 0 {
			lines = append(lines, key+":")
			continue
		}
		for i, part := range parts {
			if i == 0 {
				lines = append(lines, key+": "+part)
				continue
			}
			lines = append(lines, strings.Repeat(" ", len(key)+2)+part)
		}
	}
	return lines
}

func moreLines(n int) string {
	return fmt.Sprintf("… +%d lines · ctrl+o shows all", n)
}
