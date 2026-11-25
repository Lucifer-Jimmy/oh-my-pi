package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const (
	pickerShown         = 8
	composerPlaceholder = "Ask anything · / for commands"
	pickerPlaceholder   = "Search saved sessions"
)

type picker struct {
	sessions []session.Info
}

func (m *model) openPicker() tea.Cmd {
	if m.run != nil {
		m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before resuming", m.stamp())
		return m.flush()
	}
	if m.opts.ListSessions == nil {
		m.state.Notice(presentation.LevelWarn, "No session list in this session", m.stamp())
		return m.flush()
	}
	infos, err := m.opts.ListSessions()
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not list sessions: "+err.Error(), m.stamp())
		return m.flush()
	}
	if len(infos) == 0 {
		m.state.Notice(presentation.LevelInfo, "No earlier sessions in this directory", m.stamp())
		return m.flush()
	}
	m.picker = &picker{sessions: infos}
	m.pickerSel = 0
	m.browsing = false
	m.input.Reset()
	m.input.Placeholder = pickerPlaceholder
	return m.flush()
}

func (m *model) closePicker() {
	if m.picker == nil {
		return
	}
	m.picker, m.pickerSel = nil, 0
	m.input.Reset()
	m.input.Placeholder = composerPlaceholder
}

func (m *model) pickerMatches() []session.Info {
	if m.picker == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		return m.picker.sessions
	}
	var out []session.Info
	for _, info := range m.picker.sessions {
		if strings.Contains(strings.ToLower(info.Title), q) || strings.Contains(strings.ToLower(info.ID), q) {
			out = append(out, info)
		}
	}
	return out
}

func (m *model) movePicker(dir int) {
	if n := len(m.pickerMatches()); n > 0 {
		m.pickerSel = max(0, min(m.pickerSel+dir, n-1))
	}
}

func (m *model) onPickerKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc", "ctrl+c":
		m.closePicker()
		return m.flush()
	case "enter":
		return m.pickSession()
	case "shift+enter", "alt+enter", "ctrl+j":
		return nil
	case "up":
		m.movePicker(-1)
		return nil
	case "down":
		m.movePicker(1)
		return nil
	case "pgup":
		m.movePicker(-pickerShown)
		return nil
	case "pgdown":
		m.movePicker(pickerShown)
		return nil
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.input.Value() != before {
		m.pickerSel = 0
	}
	return cmd
}

func (m *model) pickSession() tea.Cmd {
	matches := m.pickerMatches()
	if len(matches) == 0 {
		return nil
	}
	info := matches[max(0, min(m.pickerSel, len(matches)-1))]
	if id := m.currentID(); id != "" && info.ID == id {
		m.closePicker()
		m.state.Notice(presentation.LevelInfo, "Already in session "+id, m.stamp())
		return m.flush()
	}
	m.closePicker()
	return m.resume(info.ID)
}

func (m *model) currentID() string {
	if s, ok := m.sess.(*session.Store); ok {
		return s.ID()
	}
	return ""
}

func (m *model) pickerRows() []string {
	if m.picker == nil {
		return nil
	}
	matches := m.pickerMatches()
	bar := styleWarn.Render("▌") + " "
	count := fmt.Sprintf(" · %d saved", len(m.picker.sessions))
	if strings.TrimSpace(m.input.Value()) != "" {
		count = fmt.Sprintf(" · %d of %d", len(matches), len(m.picker.sessions))
	}
	rows := []string{"", bar + styleBold.Render("Resume session") + styleDim.Render(count)}
	if len(matches) == 0 {
		rows = append(rows, bar+styleDim.Render("  no session matches"))
		return append(rows, bar+styleDim.Render("esc cancels"))
	}
	sel := max(0, min(m.pickerSel, len(matches)-1))
	start := 0
	if sel >= pickerShown {
		start = sel - pickerShown + 1
	}
	end := min(start+pickerShown, len(matches))
	if start > 0 {
		rows = append(rows, bar+styleDim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		rows = append(rows, m.pickerLine(matches[i], i == sel, bar))
	}
	if end < len(matches) {
		rows = append(rows, bar+styleDim.Render(fmt.Sprintf("  ↓ %d more", len(matches)-end)))
	}
	rows = append(rows, bar+styleDim.Render("↑↓ move · enter resumes · esc cancels"))
	for i, row := range rows {
		rows[i] = ansi.Truncate(row, m.cols(), "...")
	}
	return rows
}

func (m *model) pickerLine(info session.Info, selected bool, bar string) string {
	label := clean(firstLine(info.Title))
	if label == "" {
		label = info.ID
	}
	meta := ago(m.now(), info.Modified)
	if info.Messages > 0 {
		meta += fmt.Sprintf(" · %d msg", info.Messages)
	}
	if id := m.currentID(); id != "" && info.ID == id {
		meta += " · current"
	}
	head := bar + "  "
	if selected {
		head = bar + styleAccent.Render("›") + " "
	}
	if m.cols()-ansi.StringWidth(head)-ansi.StringWidth(meta)-8 < 8 {
		meta = ""
	}
	room := m.cols() - ansi.StringWidth(head) - ansi.StringWidth(meta)
	if meta != "" {
		room -= 2
	}
	label = ansi.Truncate(label, max(room, 8), "...")
	if selected {
		label = styleBold.Render(label)
	}
	if meta == "" {
		return head + label
	}
	pad := max(m.cols()-ansi.StringWidth(head)-ansi.StringWidth(label)-ansi.StringWidth(meta), 1)
	return head + label + strings.Repeat(" ", pad) + styleDim.Render(meta)
}

func ago(now, t time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006-01-02")
}
