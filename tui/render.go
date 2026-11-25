package tui

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const (
	defaultWidth = 80
	minCols      = 20
	previewRows  = 5
	diffRows     = 8
	askMaxRows   = 12
	askToolRows  = 10
)

var (
	stylePlain  = lipgloss.NewStyle()
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleAccent = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	styleBold   = lipgloss.NewStyle().Bold(true)
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleCode   = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *model) cols() int { return max(m.width-1, minCols) }

func (m *model) spin() string { return spinnerFrames[m.frame%len(spinnerFrames)] }

func (m *model) banner() []string {
	rows := []string{
		styleAccent.Render("omp") + styleDim.Render(" · "+m.opts.Model+" · "+m.opts.CWD),
	}
	if note := resumeNote(m.sess); note != "" {
		rows = append(rows, styleDim.Render(note))
	}
	if m.opts.Note != "" {
		rows = append(rows, styleDim.Render(m.opts.Note))
	}
	return append(rows, styleDim.Render("Enter to send · Shift+Enter for a new line · Esc to interrupt · /help"))
}

func resumeNote(sess session.Session) string {
	if sess == nil {
		return ""
	}
	n := len(sess.Messages())
	if n == 0 {
		return ""
	}
	note := fmt.Sprintf("Resumed session with %d messages", n)
	if s, ok := sess.(*session.Store); ok && s.ID() != "" {
		note += " · " + s.ID()
	}
	return note
}

func (m *model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	if m.transcript != nil {
		return m.transcriptView()
	}
	todo := m.todoRows()
	ask, answerRow := m.askRows()
	dock := append(todo, ask...)
	dock = append(dock, m.pickerRows()...)
	dock = append(dock, m.queueRows()...)
	dock = append(dock, m.paletteRows()...)
	input := strings.Split(m.input.View(), "\n")
	live := m.liveRows()
	limit := 30
	if m.height > 0 {
		limit = m.height - len(dock) - len(input) - 2
	}
	if limit = max(limit, 3); len(live) > limit {
		live = live[len(live)-limit:]
	}
	rows := append(live, dock...)
	rows = append(rows, styleDim.Render(strings.Repeat("─", m.cols())))
	top := len(rows)
	rows = append(rows, input...)
	rows = append(rows, m.statusRow())
	v := tea.NewView(strings.Join(rows, "\n"))
	switch c := m.answer.Cursor(); {
	case answerRow >= 0 && c != nil:
		c.X += ansi.StringWidth(askBar)
		c.Y += len(live) + len(todo) + answerRow
		v.Cursor = c
	default:
		if c := m.input.Cursor(); c != nil {
			c.Y += top
			v.Cursor = c
		}
	}
	return v
}

func (m *model) liveRows() []string {
	var rows []string
	busy := false
	for i := m.printed; i < len(m.state.Items); i++ {
		it := m.state.Items[i]
		busy = busy || !it.Done
		switch {
		case it.Kind == presentation.KindText && i == m.printed:
			rows = append(rows, m.liveText(it.Text[m.offset:])...)
		case it.Kind == presentation.KindText:
			body, _ := textRows(it.Text, flow{}, m.cols())
			rows = append(rows, body...)
		case it.Kind == presentation.KindThinking && !it.Done:
			rows = append(rows, "", styleDim.Render(m.spin()+" Thinking..."))
		default:
			rows = append(rows, m.itemRows(it)...)
		}
	}
	if m.run != nil && !busy {
		rows = append(rows, "", styleAccent.Render(m.spin())+styleDim.Render(" Working..."))
	}
	return rows
}

type textCache struct {
	src   string
	flow  flow
	width int
	rows  []string
}

func (m *model) liveText(src string) []string {
	c := m.cache
	if c.rows != nil && c.src == src && c.flow == m.flow && c.width == m.cols() {
		return c.rows
	}
	rows, _ := textRows(src, m.flow, m.cols())
	m.cache = textCache{src: src, flow: m.flow, width: m.cols(), rows: rows}
	return m.cache.rows
}

type flow struct {
	begun   bool
	heading bool
}

func textRows(src string, f flow, width int) ([]string, flow) {
	body, first, last := renderBlocks(src, width-2)
	if len(body) == 0 {
		return nil, f
	}
	rows := make([]string, 0, len(body)+1)
	if !f.begun || leadingBlank(src) || f.heading || first {
		rows = append(rows, "")
	}
	for i, row := range body {
		switch {
		case i == 0 && !f.begun:
			rows = append(rows, styleAccent.Render("●")+" "+row)
		case row == "":
			rows = append(rows, "")
		default:
			rows = append(rows, "  "+row)
		}
	}
	return rows, flow{begun: true, heading: last}
}

func leadingBlank(s string) bool {
	line, _, ok := strings.Cut(s, "\n")
	return ok && strings.TrimSpace(line) == ""
}

func (m *model) planRows(plan string) []string {
	rows := []string{"", styleAccent.Render("◆") + " " + styleBold.Render("Plan")}
	for _, row := range renderMarkdown(plan, m.cols()-2) {
		if row != "" {
			row = "  " + row
		}
		rows = append(rows, row)
	}
	return rows
}

func planOf(input json.RawMessage) string {
	var a struct {
		Plan string `json:"plan"`
	}
	if json.Unmarshal(input, &a) != nil {
		return ""
	}
	return a.Plan
}

func (m *model) gutter(first, rest, text string, style lipgloss.Style) []string {
	width := max(m.cols()-lipgloss.Width(first), 1)
	rows := strings.Split(ansi.Wrap(clean(text), width, ""), "\n")
	out := make([]string, len(rows))
	for i, row := range rows {
		lead := rest
		if i == 0 {
			lead = first
		}
		out[i] = lead + style.Render(row)
	}
	return out
}

func clean(s string) string {
	s = strings.ReplaceAll(ansi.Strip(s), "\t", "    ")
	return strings.Map(func(r rune) rune {
		if r == '\n' || (r >= 0x20 && r != 0x7f && (r < 0x80 || r > 0x9f)) {
			return r
		}
		return -1
	}, s)
}

func (m *model) itemRows(it presentation.Item) []string {
	switch it.Kind {
	case presentation.KindUser:
		return append([]string{""}, m.userRows(it.Text)...)
	case presentation.KindThinking:
		return []string{"", styleDim.Render("✻ " + thoughtLabel(it))}
	case presentation.KindTool:
		return append([]string{""}, m.toolRows(it)...)
	case presentation.KindNotice:
		return append([]string{""}, m.noticeRows(it)...)
	}
	return nil
}

func (m *model) userRows(text string) []string {
	var rows []string
	for i, line := range strings.Split(text, "\n") {
		lead := "  "
		if i == 0 {
			lead = styleAccent.Render("›") + " "
		}
		rows = append(rows, m.gutter(lead, "  ", line, styleBold)...)
	}
	return rows
}

func (m *model) noticeRows(it presentation.Item) []string {
	lead, style := "  ", styleDim
	switch it.Level {
	case presentation.LevelWarn:
		lead, style = styleWarn.Render("⚠")+" ", styleWarn
	case presentation.LevelError:
		lead, style = styleErr.Render("✗")+" ", styleErr
	}
	var rows []string
	for i, line := range strings.Split(it.Text, "\n") {
		l := "  "
		if i == 0 {
			l = lead
		}
		rows = append(rows, m.gutter(l, "  ", line, style)...)
	}
	return rows
}

func thoughtLabel(it presentation.Item) string {
	d := time.Duration(it.Ended-it.Started) * time.Millisecond
	if d < time.Second {
		return "Thought"
	}
	return "Thought for " + duration(d)
}

func duration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

func (m *model) toolRows(it presentation.Item) []string {
	t := it.Tool
	end := it.Ended
	if !it.Done {
		end = m.stamp()
	}
	head := m.toolGlyph(t.Status) + " " + styleBold.Render(displayName(t.Name))
	d := time.Duration(end-it.Started) * time.Millisecond
	if it.Done {
		d = time.Duration(t.DurationMs) * time.Millisecond
	}
	var elapsed string
	if it.Started > 0 && d >= 0 && (!it.Done || d >= time.Second) {
		elapsed = styleDim.Render(" · " + duration(d))
	}
	if target := toolTarget(t.Name, t.Input); target != "" {
		room := max(m.cols()-lipgloss.Width(head)-lipgloss.Width(elapsed)-2, 8)
		head += styleDim.Render("(") + ansi.Truncate(clean(target), room, "...") + styleDim.Render(")")
	}
	rows := []string{ansi.Truncate(head+elapsed, m.cols(), "...")}
	return append(rows, m.toolBody(t)...)
}

func (m *model) toolGlyph(s presentation.Status) string {
	switch s {
	case presentation.StatusRunning:
		return styleAccent.Render(m.spin())
	case presentation.StatusFailed:
		return styleErr.Render("●")
	case presentation.StatusCancelled:
		return styleWarn.Render("●")
	}
	return styleOK.Render("●")
}

func (m *model) toolBody(t *presentation.Tool) []string {
	switch t.Status {
	case presentation.StatusRunning:
		if line := progressLine(t.Progress); line != "" {
			return m.bodyRows([]string{line}, styleDim)
		}
		return nil
	case presentation.StatusCancelled:
		return m.bodyRows([]string{"Cancelled"}, styleWarn)
	}
	if t.Name == "exit_plan_mode" {
		style := styleDim
		if t.Status == presentation.StatusFailed {
			style = styleWarn
		}
		return m.bodyRows(preview(firstLine(t.Output), 1), style)
	}
	if t.Status == presentation.StatusFailed {
		return m.bodyRows(preview(t.Output, previewRows), styleErr)
	}
	switch t.Name {
	case "read", "glob", "grep":
		return m.bodyRows([]string{summary(t.Name, t.Output)}, styleDim)
	case "edit":
		return append(m.bodyRows(preview(t.Output, 1), styleDim), m.inputRows(t.Name, t.Input, m.cols(), diffRows, false, "    ")...)
	case "write":
		return append(m.bodyRows(preview(t.Output, 1), styleDim), m.inputRows(t.Name, t.Input, m.cols(), diffRows, false, "    ")...)
	}
	return m.bodyRows(preview(t.Output, previewRows), styleDim)
}

func (m *model) bodyRows(lines []string, style lipgloss.Style) []string {
	out := make([]string, 0, len(lines))
	for i, l := range lines {
		lead := "    "
		if i == 0 {
			lead = "  ⎿ "
		}
		out = append(out, styleDim.Render(lead)+style.Render(ansi.Truncate(l, m.cols()-4, "...")))
	}
	return out
}

func preview(s string, n int) []string {
	text := strings.TrimRight(clean(s), "\n ")
	if text == "" {
		return []string{"(no output)"}
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return lines
	}
	return append(lines[:n:n], moreLines(len(lines)-n))
}

func summary(name, out string) string {
	var lines []string
	for _, l := range strings.Split(clean(out), "\n") {
		if strings.TrimSpace(l) != "" {
			lines = append(lines, l)
		}
	}
	switch {
	case len(lines) == 0:
		return "(no output)"
	case len(lines) == 1:
		return lines[0]
	case name == "read":
		return fmt.Sprintf("Read %d lines", len(lines))
	case name == "glob":
		return fmt.Sprintf("Found %d files", len(lines))
	}
	return fmt.Sprintf("Found %d lines", len(lines))
}

func toolTarget(name string, input json.RawMessage) string {
	var a map[string]any
	if json.Unmarshal(input, &a) != nil || len(a) == 0 {
		return ""
	}
	str := func(k string) string {
		s, _ := a[k].(string)
		return s
	}
	switch name {
	case "bash":
		cmd := strings.TrimSpace(str("command"))
		line, _, more := strings.Cut(cmd, "\n")
		if more {
			line += " ..."
		}
		return line
	case "read":
		p := str("path")
		if off, ok := a["offset"].(float64); ok && off > 0 {
			p += fmt.Sprintf(":%d", int(off))
		}
		return p
	case "edit", "write":
		return str("path")
	case "glob", "grep":
		s := str("pattern")
		if p := str("path"); p != "" {
			s += " in " + p
		}
		return s
	case "exit_plan_mode":
		return ""
	}
	b, _ := json.Marshal(a)
	return string(b)
}

func displayName(name string) string {
	if name == "" {
		return name
	}
	name = strings.ReplaceAll(name, "_", " ")
	return strings.ToUpper(name[:1]) + name[1:]
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

const (
	askBar       = "▌ "
	maxShownOpts = 9
)

func (m *model) askRows() ([]string, int) {
	if len(m.asks) == 0 {
		return nil, -1
	}
	r := m.asks[0]
	bar := styleWarn.Render("▌") + " "
	title := r.req.Title
	if n := len(m.asks); n > 1 {
		title += fmt.Sprintf(" (1 of %d)", n)
	}
	rows := []string{"", bar + styleBold.Render(clean(title))}
	if msg := strings.TrimRight(r.req.Message, "\n"); msg != "" && msg != m.shown {
		lines := strings.Split(msg, "\n")
		if len(lines) > askMaxRows {
			lines = append(lines[:askMaxRows:askMaxRows], moreLines(len(lines)-askMaxRows))
		}
		for _, l := range lines {
			rows = append(rows, m.gutter(bar, bar, l, stylePlain)...)
		}
	}
	rows = append(rows, m.askTool(r, bar, m.dockToolRows())...)
	answerRow := -1
	switch r.req.Method {
	case agent.AskSelect:
		rows = append(rows, m.optionRows(r, bar)...)
	case agent.AskInput:
		answerRow = len(rows)
		for _, line := range strings.Split(m.answer.View(), "\n") {
			rows = append(rows, bar+line)
		}
	}
	return append(rows, bar+styleDim.Render(m.askHint(r))), answerRow
}

func (m *model) dockToolRows() int {
	if m.height <= 0 {
		return askToolRows
	}
	return min(askToolRows, max(3, (m.height-14)/2))
}

func (m *model) askTool(r *askRequest, bar string, limit int) []string {
	if r.req.Tool == "" || r.req.Tool == "exit_plan_mode" {
		return nil
	}
	lead := bar + "  "
	var rows []string
	if r.target != "" {
		head := styleBold.Render(clean(r.target))
		if r.note != "" {
			head += styleDim.Render(" · " + r.note)
		}
		rows = append(rows, lead+head)
	}
	switch {
	case r.lines != nil:
		rows = append(rows, renderDiff(r.lines, m.cols(), limit, lead, false)...)
	case len(r.req.Input) > 0:
		rows = append(rows, m.inputRows(r.req.Tool, r.req.Input, m.cols(), limit, false, lead)...)
	default:
		for _, l := range diffInput(r.req.Detail) {
			rows = append(rows, fitRows(lead, clean(l), m.cols(), false, styleDim)...)
		}
		rows = limitRows(rows, limit, lead)
	}
	return rows
}

func (m *model) optionRows(r *askRequest, bar string) []string {
	opts := r.req.Options
	start := 0
	if m.askSel >= maxShownOpts {
		start = m.askSel - maxShownOpts + 1
	}
	end := min(start+maxShownOpts, len(opts))
	var rows []string
	if start > 0 {
		rows = append(rows, bar+styleDim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		number, marker, style := " ", " ", stylePlain
		if i < 9 {
			number = fmt.Sprint(i + 1)
		}
		if i == m.askSel {
			marker, style = "›", styleAccent
		}
		first := bar + styleAccent.Render(marker) + styleBold.Render(number) + " "
		rows = append(rows, m.gutter(first, bar+"   ", opts[i], style)...)
	}
	if end < len(opts) {
		rows = append(rows, bar+styleDim.Render(fmt.Sprintf("  ↓ %d more", len(opts)-end)))
	}
	return rows
}

func (m *model) askHint(r *askRequest) string {
	tail := " · ctrl+c interrupts"
	if r.req.Tool != "" && r.req.Tool != "exit_plan_mode" {
		tail = " · ctrl+o full view" + tail
	}
	switch r.req.Method {
	case agent.AskInput:
		return "enter answers · esc dismisses" + tail
	case agent.AskSelect:
		n := min(len(r.req.Options), 9)
		keys := fmt.Sprintf("press 1-%d", n)
		if n == 1 {
			keys = "press 1"
		}
		if r.req.Tool == "" {
			return keys + " or ↑↓ enter · esc dismisses" + tail
		}
		return keys + " · esc denies" + tail
	}
	return "y approve   n deny   esc denies" + tail
}

func (m *model) todoRows() []string {
	items := m.state.Todos
	if len(items) == 0 {
		return nil
	}
	done := 0
	current := ""
	for _, it := range items {
		switch it.Status {
		case "completed":
			done++
		case "in_progress":
			current = it.Content
		}
	}
	if done == len(items) {
		return []string{styleDim.Render(fmt.Sprintf("  ✓ all %d todos done", len(items)))}
	}
	if current == "" {
		for _, it := range items {
			if it.Status != "completed" {
				current = it.Content
				break
			}
		}
	}
	line := styleAccent.Render("● ") + clean(current) +
		styleDim.Render(fmt.Sprintf("  (%d/%d)", done, len(items)))
	return []string{ansi.Truncate(line, m.cols(), "...")}
}

const queueShown = 3

func (m *model) queueRows() []string {
	var rows []string
	for i, q := range m.queue {
		if i == queueShown {
			rows = append(rows, styleDim.Render(fmt.Sprintf("  … %d more queued", len(m.queue)-queueShown)))
			break
		}
		rows = append(rows, styleDim.Render(ansi.Truncate("⏎ queued: "+clean(firstLine(q)), m.cols(), "...")))
	}
	return rows
}

const minModelLabel = 8

type statusPart struct {
	text string
	rank int
}

func (m *model) statusRow() string {
	u := m.state.Usage
	hint, hintRank := "/help", 4
	switch {
	case len(m.asks) > 0:
		hint, hintRank = "approval needed", 2
	case m.run != nil:
		hint, hintRank = "esc to interrupt", 2
	}
	mode := "no policy"
	if m.opts.Policy != nil {
		mode = string(m.opts.Policy.Mode())
	}
	if m.opts.Goal != nil {
		if g := m.opts.Goal.Summary(); g != "" {
			mode += " · " + g
		}
	}
	parts := []statusPart{
		{mode, 0},
		{"ctx " + m.contextLabel(), 1},
		{fmt.Sprintf("↑%s ↓%s", tokens(u.InputTokens+u.CacheReadTokens+u.CacheWriteTokens), tokens(u.OutputTokens)), 3},
		{hint, hintRank},
	}
	cols, brand := m.cols(), styleAccent.Render("omp")
	labels := modelLabels(m.opts.Model)
	var lefts []string
	for _, l := range labels {
		lefts = append(lefts, brand+styleDim.Render(" · "+l))
	}
	if len(lefts) == 0 {
		lefts = []string{brand}
	}
	for {
		texts := make([]string, len(parts))
		for i, p := range parts {
			texts[i] = p.text
		}
		right := styleDim.Render(strings.Join(texts, " · "))
		room := cols - lipgloss.Width(right) - 2
		place := func(left string) string {
			return left + strings.Repeat(" ", cols-lipgloss.Width(left)-lipgloss.Width(right)) + right
		}
		for _, left := range lefts {
			if lipgloss.Width(left) <= room {
				return place(left)
			}
		}
		if i := worstStatus(parts); i >= 0 && parts[i].rank >= 2 {
			parts = slices.Delete(parts, i, i+1)
			continue
		}
		if keep := room - lipgloss.Width("omp · "); len(labels) > 0 && keep >= minModelLabel {
			return place(brand + styleDim.Render(" · "+ansi.Truncate(labels[len(labels)-1], keep, "…")))
		}
		if lipgloss.Width(brand) <= room {
			return place(brand)
		}
		if i := worstStatus(parts); i >= 0 {
			parts = slices.Delete(parts, i, i+1)
			continue
		}
		return ansi.Truncate(brand+" "+right, cols, "…")
	}
}

func worstStatus(parts []statusPart) int {
	worst := -1
	for i, p := range parts {
		if p.rank > 0 && (worst < 0 || p.rank > parts[worst].rank) {
			worst = i
		}
	}
	return worst
}

func modelLabels(model string) []string {
	if model == "" {
		return nil
	}
	labels := []string{model}
	add := func(s string) {
		if s != "" && s != labels[len(labels)-1] {
			labels = append(labels, s)
		}
	}
	if strings.Count(model, "/") >= 2 {
		_, rest, _ := strings.Cut(model, "/")
		add(rest)
	}
	add(model[strings.LastIndex(model, "/")+1:])
	return labels
}

func tokens(n int) string { return presentation.FormatTokens(n) }

func (m *model) contextLabel() string {
	used := m.state.Context
	if m.opts.Window == nil {
		return tokens(used)
	}
	w := m.opts.Window()
	if w <= 0 {
		return tokens(used)
	}
	return fmt.Sprintf("%.1f%%/%s", float64(used)*100/float64(w), strings.Replace(tokens(w), ".0", "", 1))
}

func progressLine(p *presentation.Progress) string {
	if p == nil {
		return ""
	}
	line := clean(p.Message)
	if p.Name != "" {
		line = displayName(p.Name)
		if target := toolTarget(p.Name, p.Input); target != "" {
			line += "(" + clean(target) + ")"
		}
	}
	if p.Tokens > 0 {
		if line != "" {
			line += " · "
		}
		line += tokens(p.Tokens) + " tokens"
	}
	return line
}
