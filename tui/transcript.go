package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
)

const (
	transcriptReleaseDelay = 80 * time.Millisecond
	transcriptOutputRows   = 4000
)

type releaseMsg struct{}

type transcript struct {
	rows   []string
	offset int
	follow bool
	width  int
	cache  map[int]transcriptEntry
}

type transcriptEntry struct {
	sig  string
	rows []string
}

func (m *model) openTranscript() {
	m.transcript = &transcript{follow: true, cache: map[int]transcriptEntry{}}
	m.refreshTranscript()
}

func (m *model) closeTranscript() tea.Cmd {
	m.transcript = nil
	m.holding = true
	return tea.Tick(transcriptReleaseDelay, func(time.Time) tea.Msg { return releaseMsg{} })
}

func (m *model) refreshTranscript() {
	t := m.transcript
	if t == nil {
		return
	}
	width := max(m.width-1, minCols)
	if t.width != width {
		t.cache = map[int]transcriptEntry{}
		t.width = width
	}
	var rows []string
	for _, it := range m.state.Items {
		sig := itemSignature(it)
		e, ok := t.cache[it.ID]
		if !ok || e.sig != sig {
			e = transcriptEntry{sig: sig, rows: m.fullRows(it, width)}
			t.cache[it.ID] = e
		}
		rows = append(rows, e.rows...)
	}
	if len(m.asks) > 0 {
		rows = append(rows, m.askSection(m.asks[0], width)...)
	}
	t.rows = rows
	if t.follow {
		t.offset = t.maxOffset(m.pageRows())
	}
	t.offset = min(max(t.offset, 0), t.maxOffset(m.pageRows()))
}

func itemSignature(it presentation.Item) string {
	sig := fmt.Sprintf("%s|%t|%d|%d", it.Kind, it.Done, len(it.Text), it.Ended)
	if t := it.Tool; t != nil {
		sig += fmt.Sprintf("|%s|%d|%d", t.Status, len(t.Output), t.DurationMs)
		if t.Progress != nil {
			sig += "|" + t.Progress.Message + t.Progress.Name + fmt.Sprint(t.Progress.Tokens)
		}
	}
	return sig
}

func (m *model) fullRows(it presentation.Item, width int) []string {
	switch it.Kind {
	case presentation.KindText:
		rows, _ := textRows(it.Text, flow{}, width)
		return rows
	case presentation.KindThinking:
		rows := []string{"", styleDim.Render("✻ " + thoughtLabel(it))}
		for _, l := range diffInput(it.Text) {
			rows = append(rows, fitRows("  ", clean(l), width, true, styleDim)...)
		}
		return rows
	case presentation.KindTool:
		return append([]string{""}, m.fullToolRows(it, width)...)
	}
	return m.itemRows(it)
}

func (m *model) fullToolRows(it presentation.Item, width int) []string {
	t := it.Tool
	head := m.toolRows(it)[0]
	rows := []string{head}
	rows = append(rows, m.inputRows(t.Name, t.Input, width, 0, true, "    ")...)
	if line := progressLine(t.Progress); t.Status == presentation.StatusRunning && line != "" {
		rows = append(rows, fitRows("  ⎿ ", line, width, true, styleDim)...)
	}
	out := strings.TrimRight(t.Output, "\n ")
	if out == "" {
		if t.Status == presentation.StatusCancelled {
			rows = append(rows, "  ⎿ "+styleWarn.Render("Cancelled"))
		}
		return rows
	}
	style := styleDim
	if t.Status == presentation.StatusFailed {
		style = styleErr
	}
	lines := diffInput(out)
	shown := lines
	if len(shown) > transcriptOutputRows {
		shown = shown[:transcriptOutputRows]
	}
	for i, l := range shown {
		lead := "    "
		if i == 0 {
			lead = "  ⎿ "
		}
		rows = append(rows, fitRows(lead, clean(l), width, true, style)...)
	}
	if hidden := len(lines) - len(shown); hidden > 0 {
		rows = append(rows, "    "+styleDim.Render(fmt.Sprintf("… %d more lines not shown", hidden)))
	}
	return rows
}

func (m *model) pageRows() int {
	if m.height <= 1 {
		return 24
	}
	return m.height - 1
}

func (t *transcript) maxOffset(page int) int {
	return max(len(t.rows)-page, 0)
}

func (t *transcript) scroll(delta, page int) {
	t.offset = min(max(t.offset+delta, 0), t.maxOffset(page))
	t.follow = t.offset == t.maxOffset(page)
}

func (m *model) onTranscriptKey(k tea.KeyPressMsg) tea.Cmd {
	t, page := m.transcript, m.pageRows()
	switch k.String() {
	case "esc", "q", "ctrl+o":
		return m.closeTranscript()
	case "ctrl+c":
		m.interrupt()
		return m.closeTranscript()
	case "up", "k":
		t.scroll(-1, page)
	case "down", "j":
		t.scroll(1, page)
	case "pgup", "b":
		t.scroll(-page, page)
	case "pgdown", "space", "f":
		t.scroll(page, page)
	case "home", "g":
		t.scroll(-len(t.rows), page)
	case "end", "G":
		t.scroll(len(t.rows), page)
	}
	return nil
}

func (m *model) transcriptView() tea.View {
	t, page := m.transcript, m.pageRows()
	end := min(t.offset+page, len(t.rows))
	rows := append([]string(nil), t.rows[t.offset:end]...)
	for len(rows) < page {
		rows = append(rows, "")
	}
	where := "all"
	switch {
	case len(t.rows) <= page:
	case t.offset == 0:
		where = "top"
	case end >= len(t.rows):
		where = "bottom"
	default:
		where = fmt.Sprintf("%d%%", end*100/len(t.rows))
	}
	status := styleDim.Render(ansi.Truncate("transcript · "+where+" · ↑↓ pgup pgdn g G · esc closes", max(m.width-1, minCols), "…"))
	if len(m.asks) > 0 {
		status += styleDim.Render(" · ") + styleWarn.Render("approval waiting")
	}
	rows = append(rows, status)
	v := tea.NewView(strings.Join(rows, "\n"))
	v.AltScreen = true
	return v
}

func (m *model) askSection(r *askRequest, width int) []string {
	if r.req.Tool == "" || r.req.Tool == "exit_plan_mode" {
		return nil
	}
	bar := styleWarn.Render("▌") + " "
	rows := []string{"", bar + styleBold.Render(clean(r.req.Title)) + styleDim.Render(" · waiting for your answer")}
	lead := bar + "  "
	if r.target != "" {
		head := styleBold.Render(clean(r.target))
		if r.note != "" {
			head += styleDim.Render(" · " + r.note)
		}
		rows = append(rows, lead+head)
	}
	switch {
	case r.lines != nil:
		rows = append(rows, renderDiff(r.lines, width, 0, lead, true)...)
	case len(r.req.Input) > 0:
		rows = append(rows, m.inputRows(r.req.Tool, r.req.Input, width, 0, true, lead)...)
	default:
		for _, l := range diffInput(r.req.Detail) {
			rows = append(rows, fitRows(lead, clean(l), width, true, styleDim)...)
		}
	}
	return rows
}
