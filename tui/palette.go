package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

type command struct {
	Name string
	Desc string
}

var paletteCommands = []command{
	{"/clear", "clear the screen, keep this conversation"},
	{"/compact", "summarize the conversation (/compact <focus>)"},
	{"/delete", "delete another session (/delete <id>)"},
	{"/effort", "show or set the thinking level (/effort high)"},
	{"/exit", "leave omp"},
	{"/export", "write the conversation to markdown (/export [path])"},
	{"/goal", "work on an objective until it is verifiably done (/goal <objective>)"},
	{"/help", "list commands and keys"},
	{"/memory", "show, sync or clear this project's memory"},
	{"/model", "show or switch the model (/model <provider>/<id>)"},
	{"/new", "start a new conversation"},
	{"/plan", "enter plan mode"},
	{"/quit", "leave omp"},
	{"/rename", "title this session (/rename <title>)"},
	{"/resume", "list sessions, or switch (/resume <id|prefix>)"},
}

const paletteMax = 6

func (m *model) paletteQuery() (string, bool) {
	text := m.input.Value()
	if !strings.HasPrefix(text, "/") || strings.ContainsAny(text, " \n") {
		return "", false
	}
	return text, true
}

func (m *model) paletteOpen() bool {
	if m.picker != nil {
		return false
	}
	_, ok := m.paletteQuery()
	return ok
}

func (m *model) paletteActive() bool {
	return m.paletteOpen() && !m.browsing
}

func (m *model) paletteMatches() []command {
	q, ok := m.paletteQuery()
	if !ok {
		return nil
	}
	var out []command
	for _, c := range paletteCommands {
		if strings.HasPrefix(c.Name, q) {
			out = append(out, c)
		}
	}
	return out
}

func (m *model) movePalette(dir int) {
	n := len(m.paletteMatches())
	if n == 0 {
		return
	}
	m.paletteIndex = (m.paletteIndex + dir + n) % n
}

func (m *model) completePalette() {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return
	}
	i := min(max(m.paletteIndex, 0), len(matches)-1)
	m.input.SetValue(matches[i].Name + " ")
	m.paletteIndex = 0
}

func (m *model) paletteRows() []string {
	matches := m.paletteMatches()
	if len(matches) == 0 {
		return nil
	}
	i := min(max(m.paletteIndex, 0), len(matches)-1)
	start := 0
	if i >= paletteMax {
		start = i - paletteMax + 1
	}
	end := min(start+paletteMax, len(matches))
	var rows []string
	if start > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for j := start; j < end; j++ {
		c := matches[j]
		line := c.Name + "  " + styleDim.Render(c.Desc)
		if j == i {
			line = styleAccent.Render("› ") + line
		} else {
			line = "  " + line
		}
		rows = append(rows, ansi.Truncate(line, m.cols(), "..."))
	}
	if end < len(matches) {
		rows = append(rows, styleDim.Render(fmt.Sprintf("  ↓ %d more", len(matches)-end)))
	}
	return rows
}
