package tui

import (
	"context"
	"iter"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type Runner = agent.Runner

type CompactFunc func(ctx context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error]

const (
	maxBatch      = 256
	tickInterval  = 100 * time.Millisecond
	inputMaxRows  = 10
	answerMaxRows = 4
)

type runUpdate struct {
	ev  agent.Event
	err error
}

type runBatch struct {
	ch      <-chan runUpdate
	updates []runUpdate
	closed  bool
}

type askMsg struct{ req *askRequest }

type printedMsg struct{}

type tickMsg struct{}

type activeRun struct {
	cancel context.CancelFunc
	ch     <-chan runUpdate
}

type model struct {
	ctx   context.Context
	opts  Options
	now   func() time.Time
	print func(string) tea.Cmd

	state presentation.State
	sess  session.Session
	input textarea.Model
	width int

	height int

	run   *activeRun
	queue []string
	asks  []*askRequest

	history []string
	histPos int

	printed int
	offset  int
	flow    flow
	early   bool
	shown   string
	cache   textCache

	outbox   []string
	printing bool
	quitting bool

	frame   int
	ticking bool

	paletteIndex int
	browsing     bool

	picker    *picker
	pickerSel int

	transcript  *transcript
	holding     bool
	openPending bool

	askFor   *askRequest
	askSel   int
	answer   textarea.Model
	diffMemo map[string][]diffLine

	modes []permission.Mode
}

func newTextArea(placeholder string, maxRows int) textarea.Model {
	in := textarea.New()
	in.ShowLineNumbers = false
	in.Placeholder = placeholder
	in.CharLimit = 0
	in.DynamicHeight = true
	in.MinHeight = 1
	in.MaxHeight = maxRows
	in.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("shift+enter", "alt+enter", "ctrl+j"))
	in.SetPromptFunc(2, func(info textarea.PromptInfo) string {
		if info.LineNumber == 0 {
			return "› "
		}
		return "  "
	})
	styles := textarea.DefaultDarkStyles()
	styles.Focused.CursorLine = styles.Focused.Text
	styles.Focused.Prompt = styleAccent
	styles.Blurred.Prompt = styleDim
	styles.Cursor.Color = nil
	in.SetStyles(styles)
	in.SetVirtualCursor(false)
	return in
}

func newModel(ctx context.Context, opts Options) *model {
	in := newTextArea(composerPlaceholder, inputMaxRows)
	in.Focus()
	ans := newTextArea(answerPlaceholder, answerMaxRows)

	m := &model{
		ctx:    ctx,
		opts:   opts,
		now:    time.Now,
		print:  printCmd,
		sess:   session.NewMemory(nil),
		input:  in,
		answer: ans,
		askSel: -1,
		width:  defaultWidth,
		modes:  []permission.Mode{permission.ModeDefault, permission.ModeAcceptEdits, permission.ModePlan},
	}
	if opts.Session != nil {
		m.sess = opts.Session
	}
	m.state.Projectors = opts.Projectors
	m.state.InjectedTags = opts.InjectedTags
	if opts.Policy != nil && opts.Policy.Mode() == permission.ModeBypass {
		m.modes = append(m.modes, permission.ModeBypass)
	}
	m.input.SetWidth(m.cols())
	m.answer.SetWidth(m.answerWidth())
	return m
}

const answerPlaceholder = "Type your answer"

func (m *model) answerWidth() int { return max(m.cols()-2, 8) }

func printCmd(text string) tea.Cmd {
	return tea.Sequence(tea.Println(text), func() tea.Msg { return printedMsg{} })
}

func (m *model) Init() tea.Cmd {
	m.outbox = append(m.outbox, m.banner()...)
	m.state.Replay(m.sess.Messages())
	cmds := []tea.Cmd{waitAsk(m.opts.Host)}
	if len(m.opts.Prompts) > 0 {
		cmds = append(cmds, m.start(strings.Join(m.opts.Prompts, "\n\n")))
	}
	cmds = append(cmds, m.flush())
	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.syncFocus()
	return m, cmd
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(m.cols())
		m.answer.SetWidth(m.answerWidth())
		m.refreshTranscript()
		return nil
	case runBatch:
		return m.onBatch(msg)
	case askMsg:
		msg.req.prepare(m.opts.CWD)
		m.asks = append(m.asks, msg.req)
		m.refreshTranscript()
		return waitAsk(m.opts.Host)
	case printedMsg:
		m.printing = false
		if m.openPending {
			m.openPending = false
			m.openTranscript()
			return nil
		}
		return m.flush()
	case releaseMsg:
		m.holding = false
		return m.flush()
	case tickMsg:
		m.ticking = false
		m.frame++
		m.pruneAsks()
		return m.ensureTick()
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case tea.PasteMsg:
		if m.transcript != nil {
			return nil
		}
		if m.picker != nil {
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			m.pickerSel = 0
			return cmd
		}
		if len(m.asks) > 0 {
			if m.asks[0].req.Method != agent.AskInput {
				return nil
			}
			var cmd tea.Cmd
			m.answer, cmd = m.answer.Update(msg)
			return cmd
		}
		m.paletteIndex = 0
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return cmd
}

func (m *model) syncFocus() {
	var front *askRequest
	if len(m.asks) > 0 {
		front = m.asks[0]
	}
	if front != m.askFor {
		m.askFor, m.askSel = front, -1
		m.answer.Reset()
		m.answer.Blur()
		if front != nil && front.req.Method == agent.AskInput {
			m.answer.Placeholder = answerPlaceholder
			if p := strings.TrimSpace(front.req.Placeholder); p != "" {
				m.answer.Placeholder = p
			}
			m.answer.Focus()
		}
	}
	if len(m.asks) > 0 || m.quitting {
		m.input.Blur()
		return
	}
	if !m.input.Focused() {
		m.input.Focus()
	}
}

func (m *model) stamp() int64 { return m.now().UnixMilli() }

func (m *model) freshSession() (session.Session, error) {
	if m.opts.NewSession == nil {
		return session.NewMemory(nil), nil
	}
	return m.opts.NewSession()
}

func (m *model) start(prompt string) tea.Cmd {
	m.state.Submit(prompt, m.stamp())
	runner, sess := m.opts.Runner, m.sess
	return m.drive(func(ctx context.Context) iter.Seq2[agent.Event, error] {
		return runner.Run(ctx, sess, prompt)
	})
}

func (m *model) startCompact(instructions string) tea.Cmd {
	if m.run != nil {
		m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before compacting", m.stamp())
		return m.flush()
	}
	if m.opts.Compact == nil {
		m.state.Notice(presentation.LevelWarn, "Compaction is not available in this session", m.stamp())
		return m.flush()
	}
	compact, sess := m.opts.Compact, m.sess
	return m.drive(func(ctx context.Context) iter.Seq2[agent.Event, error] {
		return compact(ctx, sess, instructions)
	})
}

func (m *model) drive(seq func(ctx context.Context) iter.Seq2[agent.Event, error]) tea.Cmd {
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan runUpdate, maxBatch)
	m.run = &activeRun{cancel: cancel, ch: ch}
	go func() {
		defer close(ch)
		for ev, err := range seq(ctx) {
			if err != nil {
				ch <- runUpdate{err: err}
				return
			}
			ch <- runUpdate{ev: ev}
		}
	}()
	return tea.Batch(waitRun(ch), m.ensureTick(), m.flush())
}

func waitRun(ch <-chan runUpdate) tea.Cmd {
	return func() tea.Msg {
		u, ok := <-ch
		if !ok {
			return runBatch{ch: ch, closed: true}
		}
		b := runBatch{ch: ch, updates: []runUpdate{u}}
		for len(b.updates) < maxBatch {
			select {
			case u, ok := <-ch:
				if !ok {
					b.closed = true
					return b
				}
				b.updates = append(b.updates, u)
			default:
				return b
			}
		}
		return b
	}
}

func waitAsk(h *Host) tea.Cmd {
	if h == nil {
		return nil
	}
	return func() tea.Msg { return askMsg{<-h.requests} }
}

func (m *model) onBatch(b runBatch) tea.Cmd {
	if m.run == nil || b.ch != m.run.ch {
		return nil
	}
	for _, u := range b.updates {
		if u.err != nil {
			m.state.Notice(presentation.LevelError, u.err.Error(), m.stamp())
			continue
		}
		m.state.Apply(u.ev)
	}
	m.pruneAsks()
	m.refreshTranscript()
	if !b.closed {
		return tea.Batch(waitRun(b.ch), m.flush())
	}
	return m.endRun()
}

func (m *model) endRun() tea.Cmd {
	m.run.cancel()
	m.run = nil
	if m.state.Running {
		m.state.Apply(agent.Event{Kind: agent.EventResult, Timestamp: m.stamp(), Result: &agent.Result{
			Reason: agent.ReasonError, Err: "the run ended without a result",
		}})
	}
	m.dropAsks()
	var next tea.Cmd
	if len(m.queue) > 0 && !m.quitting {
		if m.state.Reason == agent.ReasonAborted {
			m.restoreQueue()
		} else {
			prompt := strings.Join(m.queue, "\n\n")
			m.queue = nil
			next = m.start(prompt)
		}
	}
	return tea.Batch(m.flush(), next)
}

func (m *model) restoreQueue() {
	text := strings.Join(m.queue, "\n\n")
	if draft := m.input.Value(); draft != "" {
		text += "\n\n" + draft
	}
	m.queue = nil
	m.browsing = false
	m.histPos = len(m.history)
	m.input.SetValue(text)
}

func (m *model) interrupt() {
	if m.run == nil {
		return
	}
	m.run.cancel()
	m.dropAsks()
}

func (m *model) dropAsks() {
	for _, r := range m.asks {
		r.answer(agent.AskResponse{Cancelled: true})
	}
	m.asks = nil
}

func (m *model) pruneAsks() {
	kept := m.asks[:0]
	for _, r := range m.asks {
		if r.ctx.Err() == nil {
			kept = append(kept, r)
		}
	}
	m.asks = kept
}

func (m *model) quit() tea.Cmd {
	m.quitting = true
	m.transcript = nil
	m.holding = false
	if m.run != nil {
		m.run.cancel()
	}
	m.dropAsks()
	return m.flush()
}

func (m *model) ensureTick() tea.Cmd {
	if m.ticking || m.run == nil {
		return nil
	}
	m.ticking = true
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) onKey(k tea.KeyPressMsg) tea.Cmd {
	if m.transcript != nil {
		return m.onTranscriptKey(k)
	}
	if k.String() == "ctrl+o" {
		if m.printing {
			m.openPending = true
			return nil
		}
		m.openTranscript()
		return nil
	}
	if len(m.asks) > 0 {
		return m.onAskKey(k)
	}
	if m.picker != nil {
		return m.onPickerKey(k)
	}
	switch k.String() {
	case "ctrl+c":
		switch {
		case m.run != nil:
			m.interrupt()
		case m.input.Value() != "":
			m.input.Reset()
		default:
			return m.quit()
		}
		return nil
	case "ctrl+d":
		if m.run == nil && m.input.Value() == "" {
			return m.quit()
		}
	case "esc":
		switch {
		case m.browsing:
			m.browsing = false
			m.histPos = len(m.history)
			m.input.Reset()
		case m.run != nil:
			m.interrupt()
		case m.input.Value() != "":
			m.input.Reset()
		}
		return nil
	case "shift+tab":
		m.cycleMode()
		return nil
	case "enter":
		return m.submit()
	case "up":
		if m.paletteActive() {
			m.movePalette(-1)
			return nil
		}
		if m.browse(-1) {
			return nil
		}
	case "down":
		if m.paletteActive() {
			m.movePalette(1)
			return nil
		}
		if m.browse(1) {
			return nil
		}
	case "tab":
		if m.paletteActive() {
			m.completePalette()
			return nil
		}
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.input.Value() != before {
		m.browsing = false
		m.paletteIndex = 0
	}
	return cmd
}

func (m *model) onAskKey(k tea.KeyPressMsg) tea.Cmd {
	r := m.asks[0]
	key := k.String()
	if key == "ctrl+c" {
		m.interrupt()
		return nil
	}
	switch {
	case r.req.Method == agent.AskInput:
		switch key {
		case "esc":
			m.answerAsk(agent.AskResponse{Cancelled: true})
		case "enter":
			if v := strings.TrimSpace(m.answer.Value()); v != "" {
				m.answerAsk(agent.AskResponse{Value: v})
			}
		default:
			var cmd tea.Cmd
			m.answer, cmd = m.answer.Update(k)
			return cmd
		}
		return nil
	case r.req.Method == agent.AskSelect && r.req.Tool == "":
		switch key {
		case "up":
			m.askSel = max(m.askSel-1, 0)
			return nil
		case "down":
			m.askSel = min(m.askSel+1, len(r.req.Options)-1)
			return nil
		case "enter":
			if m.askSel >= 0 {
				m.answerAsk(agent.AskResponse{Value: r.req.Options[m.askSel]})
			}
			return nil
		}
	}
	if resp, ok := answerFor(r.req, key); ok {
		m.answerAsk(resp)
	}
	return nil
}

func (m *model) answerAsk(resp agent.AskResponse) {
	m.asks[0].answer(resp)
	m.asks = m.asks[1:]
	m.refreshTranscript()
}

func answerFor(req agent.AskRequest, key string) (agent.AskResponse, bool) {
	if req.Method != agent.AskSelect {
		switch key {
		case "y", "Y":
			return agent.AskResponse{Confirmed: true}, true
		case "n", "N", "esc":
			return agent.AskResponse{}, true
		}
		return agent.AskResponse{}, false
	}
	pick := func(label string) (agent.AskResponse, bool) {
		for _, o := range req.Options {
			if o == label {
				return agent.AskResponse{Value: o}, true
			}
		}
		return agent.AskResponse{}, false
	}
	switch {
	case key == "esc":
		return agent.AskResponse{Cancelled: true}, true
	case key == "y" || key == "Y":
		return pick("Yes")
	case key == "n" || key == "N":
		return pick("No")
	case len(key) == 1 && key[0] >= '1' && int(key[0]-'0') <= len(req.Options):
		return agent.AskResponse{Value: req.Options[key[0]-'1']}, true
	}
	return agent.AskResponse{}, false
}

func (m *model) cycleMode() {
	if m.opts.Policy == nil {
		return
	}
	cur := m.opts.Policy.Mode()
	next := m.modes[0]
	for i, mode := range m.modes {
		if mode == cur {
			next = m.modes[(i+1)%len(m.modes)]
		}
	}
	m.opts.Policy.SetMode(next)
}

func (m *model) browse(dir int) bool {
	cur := m.input.Value()
	if len(m.history) == 0 {
		return false
	}
	if cur != "" && (m.histPos >= len(m.history) || cur != m.history[m.histPos]) {
		return false
	}
	pos := m.histPos + dir
	m.browsing = true
	switch {
	case pos < 0:
		return true
	case pos >= len(m.history):
		m.histPos = len(m.history)
		m.input.Reset()
	default:
		m.histPos = pos
		m.input.SetValue(m.history[pos])
	}
	return true
}

func (m *model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}
	m.input.Reset()
	if n := len(m.history); n == 0 || m.history[n-1] != text {
		m.history = append(m.history, text)
	}
	m.histPos = len(m.history)
	if strings.HasPrefix(text, "/") {
		return m.command(text)
	}
	return m.send(text)
}

func (m *model) send(text string) tea.Cmd {
	if m.run != nil {
		if m.opts.Steer != nil {
			m.opts.Steer(text)
			m.state.Notice(presentation.LevelInfo, "steering: "+firstLine(text), m.stamp())
			return m.flush()
		}
		m.queue = append(m.queue, text)
		return nil
	}
	return m.start(text)
}

func (m *model) goalCommand(args string) tea.Cmd {
	if m.opts.Goal == nil {
		m.state.Notice(presentation.LevelWarn, "Goal mode is not available in this session", m.stamp())
		return m.flush()
	}
	prompt, notice, err := m.opts.Goal.Command(args)
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Goal: "+err.Error(), m.stamp())
		return m.flush()
	}
	if notice != "" {
		m.state.Notice(presentation.LevelInfo, notice, m.stamp())
	}
	if prompt == "" {
		return m.flush()
	}
	return tea.Batch(m.flush(), m.send(prompt))
}

func (m *model) memoryCommand(args string) tea.Cmd {
	if m.opts.Memory == nil {
		m.state.Notice(presentation.LevelWarn, "Memory is off. Turn it on with memory: {backend: local} in ~/.omp/config.yml", m.stamp())
		return m.flush()
	}
	notice, err := m.opts.Memory.Command(args)
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Memory: "+err.Error(), m.stamp())
		return m.flush()
	}
	m.state.Notice(presentation.LevelInfo, notice, m.stamp())
	return m.flush()
}

const helpText = `Commands
  /new           start a new conversation
  /clear         clear the screen, keep this conversation
  /compact       summarize the conversation, keeping the recent part (/compact <focus>)
  /model         show the current model, or switch (/model <provider>/<id>)
  /plan          enter plan mode (read-only until the plan is approved)
  /goal          work on an objective until it is verifiably done (/goal <objective>, show, pause, resume, budget <tokens|off>, drop)
  /memory        show this project's memory (/memory sync reads past sessions now, /memory clear deletes it)
  /effort        show or set the thinking level (/effort high)
  /resume        pick a saved session with ↑↓ and typing, or switch directly (/resume <id|prefix>)
  /rename        title this session (/rename <title>)
  /export        write the conversation to markdown (/export [path])
  /delete        delete another session (/delete <id>)
  /exit, /quit   leave omp
Keys
  Enter          send, or steer the running turn (queued if the session cannot steer)
  Shift+Enter    new line (also Alt+Enter, Ctrl+J)
  Esc            interrupt the running turn; otherwise dismiss the picker or clear the input
  Shift+Tab      cycle the approval mode (default, acceptEdits, plan)
  Ctrl+O         full transcript: every tool's whole output, and the whole diff an approval would apply
  Up / Down      previous prompts; in a picker or the command list, move the selection
  Ctrl+C         interrupt, clear the input, or exit when empty`

func (m *model) command(text string) tea.Cmd {
	name, _, _ := strings.Cut(text, " ")
	switch name {
	case "/exit", "/quit":
		return m.quit()
	case "/clear":
		if m.run != nil {
			m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before clearing the screen", m.stamp())
			break
		}
		m.collect()
		m.state.Reset()
		m.resetView()
		m.outbox = append(m.outbox, "", styleDim.Render("── screen cleared ──"))
	case "/new":
		if m.run != nil {
			m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before starting a new conversation", m.stamp())
			break
		}
		next, err := m.freshSession()
		if err != nil {
			m.state.Notice(presentation.LevelWarn, "Could not start a new session: "+err.Error(), m.stamp())
			break
		}
		m.collect()
		m.sess = next
		m.state.Reset()
		m.resetView()
		m.outbox = append(m.outbox, "", styleDim.Render("── new conversation ──"))
	case "/help":
		m.state.Notice(presentation.LevelInfo, helpText, m.stamp())
	case "/model":
		_, rest, _ := strings.Cut(text, " ")
		if strings.TrimSpace(rest) == "" {
			m.state.Notice(presentation.LevelInfo, "Model: "+m.opts.Model, m.stamp())
			break
		}
		m.switchModel(strings.TrimSpace(rest))
	case "/resume":
		_, rest, _ := strings.Cut(text, " ")
		if arg := strings.TrimSpace(rest); arg != "" {
			return m.resume(arg)
		}
		return m.openPicker()
	case "/rename":
		_, rest, _ := strings.Cut(text, " ")
		m.renameSession(strings.TrimSpace(rest))
	case "/export":
		_, rest, _ := strings.Cut(text, " ")
		m.exportSession(strings.TrimSpace(rest))
	case "/delete":
		_, rest, _ := strings.Cut(text, " ")
		m.deleteSession(strings.TrimSpace(rest))
	case "/plan":
		if m.opts.Policy == nil {
			m.state.Notice(presentation.LevelWarn, "This session has no approval policy", m.stamp())
			break
		}
		if m.opts.Policy.Mode() == permission.ModePlan {
			m.state.Notice(presentation.LevelInfo, "Already in plan mode; Shift+Tab leaves it", m.stamp())
			break
		}
		m.opts.Policy.SetMode(permission.ModePlan)
		m.state.Notice(presentation.LevelInfo, "Plan mode: reads only, no writes or commands until you approve a plan", m.stamp())
	case "/effort":
		m.setEffort(strings.TrimSpace(strings.TrimPrefix(text, "/effort")))
	case "/goal":
		return m.goalCommand(strings.TrimPrefix(text, "/goal"))
	case "/memory":
		return m.memoryCommand(strings.TrimPrefix(text, "/memory"))
	case "/compact":
		_, rest, _ := strings.Cut(text, " ")
		return m.startCompact(rest)
	default:
		m.state.Notice(presentation.LevelWarn, "Unknown command "+name+", try /help", m.stamp())
	}
	return m.flush()
}

var effortLevels = []llm.ThinkingLevel{
	llm.ThinkingOff, llm.ThinkingMinimal, llm.ThinkingLow, llm.ThinkingMedium,
	llm.ThinkingHigh, llm.ThinkingXHigh, llm.ThinkingMax,
}

func (m *model) renameSession(title string) {
	if m.opts.Rename == nil {
		m.state.Notice(presentation.LevelWarn, "This session cannot be renamed", m.stamp())
		return
	}
	if title == "" {
		m.state.Notice(presentation.LevelWarn, "Usage: /rename <title>", m.stamp())
		return
	}
	if err := m.opts.Rename(title); err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not rename: "+err.Error(), m.stamp())
		return
	}
	m.state.Notice(presentation.LevelInfo, "Session renamed to "+title, m.stamp())
}

func (m *model) exportSession(path string) {
	if m.opts.Export == nil {
		m.state.Notice(presentation.LevelWarn, "This session cannot be exported", m.stamp())
		return
	}
	written, err := m.opts.Export(path)
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not export: "+err.Error(), m.stamp())
		return
	}
	m.state.Notice(presentation.LevelInfo, "Exported to "+written, m.stamp())
}

func (m *model) deleteSession(id string) {
	if m.opts.Delete == nil {
		m.state.Notice(presentation.LevelWarn, "This session cannot delete sessions", m.stamp())
		return
	}
	if id == "" {
		m.state.Notice(presentation.LevelWarn, "Usage: /delete <id> (see /resume)", m.stamp())
		return
	}
	if err := m.opts.Delete(id); err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not delete: "+err.Error(), m.stamp())
		return
	}
	m.state.Notice(presentation.LevelInfo, "Deleted session "+id, m.stamp())
}

func (m *model) switchModel(selector string) {
	if m.run != nil {
		m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before switching models", m.stamp())
		return
	}
	if m.opts.SwitchModel == nil {
		m.state.Notice(presentation.LevelWarn, "This session cannot switch models", m.stamp())
		return
	}
	label, err := m.opts.SwitchModel(selector)
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not switch model: "+err.Error(), m.stamp())
		return
	}
	m.opts.Model = label
	m.state.Notice(presentation.LevelInfo, "Model: "+label, m.stamp())
}

func (m *model) resume(arg string) tea.Cmd {
	if m.run != nil {
		m.state.Notice(presentation.LevelWarn, "A turn is running, press Esc before resuming", m.stamp())
		return m.flush()
	}
	if m.opts.Resume == nil {
		m.state.Notice(presentation.LevelWarn, "No session store in this session", m.stamp())
		return m.flush()
	}
	next, id, err := m.opts.Resume(arg)
	if err != nil {
		m.state.Notice(presentation.LevelWarn, "Could not resume: "+err.Error(), m.stamp())
		return m.flush()
	}
	m.collect()
	m.sess = next
	m.state.Reset()
	m.resetView()
	m.outbox = append(m.outbox, "", styleDim.Render("── resumed "+id+" ──"))
	m.state.Replay(m.sess.Messages())
	return m.flush()
}

func (m *model) setEffort(arg string) {
	if m.opts.Effort == nil {
		m.state.Notice(presentation.LevelWarn, "This session has no effort control", m.stamp())
		return
	}
	cur := m.opts.Effort.Level()
	if arg == "" {
		shown := string(cur)
		if shown == "" {
			shown = "default"
		}
		m.state.Notice(presentation.LevelInfo, "Thinking level: "+shown+" (set one of: off minimal low medium high xhigh max)", m.stamp())
		return
	}
	level := llm.ThinkingLevel(arg)
	valid := false
	for _, l := range effortLevels {
		if l == level {
			valid = true
		}
	}
	if !valid {
		m.state.Notice(presentation.LevelWarn, "Unknown thinking level "+arg+"; try off minimal low medium high xhigh max", m.stamp())
		return
	}
	m.opts.Effort.SetLevel(level)
	m.state.Notice(presentation.LevelInfo, "Thinking level: "+arg, m.stamp())
}

func (m *model) flush() tea.Cmd {
	m.collect()
	if m.printing || m.transcript != nil || m.holding {
		return nil
	}
	if len(m.outbox) == 0 {
		if m.quitting {
			return tea.Quit
		}
		return nil
	}
	text := strings.Join(m.outbox, "\n")
	m.outbox = nil
	m.printing = true
	return m.print(text)
}

func (m *model) collect() {
	items := m.state.Items
	for m.printed < len(items) {
		it := items[m.printed]
		if it.Kind == presentation.KindText {
			if !m.collectText(it) {
				return
			}
		} else {
			m.collectEarly(it)
			if !it.Done {
				return
			}
			m.outbox = append(m.outbox, m.itemRows(it)...)
		}
		m.printed++
		m.offset, m.flow, m.early = 0, flow{}, false
	}
}

func (m *model) resetView() {
	m.printed, m.offset, m.flow, m.early = 0, 0, flow{}, false
}

func (m *model) collectEarly(it presentation.Item) {
	if m.early || it.Kind != presentation.KindTool || it.Tool.Name != "exit_plan_mode" {
		return
	}
	m.early = true
	if plan := planOf(it.Tool.Input); strings.TrimSpace(plan) != "" {
		m.shown = plan
		m.outbox = append(m.outbox, m.planRows(plan)...)
	}
}

func (m *model) collectText(it presentation.Item) bool {
	rest := it.Text[m.offset:]
	if !it.Done {
		rest = rest[:stableCut(rest)]
	}
	if rows, next := textRows(rest, m.flow, m.cols()); len(rows) > 0 {
		m.outbox = append(m.outbox, rows...)
		m.flow = next
	}
	m.offset += len(rest)
	return it.Done
}
