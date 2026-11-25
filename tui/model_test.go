package tui

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type scriptRunner struct {
	mu      sync.Mutex
	prompts []string
	script  func(ctx context.Context, input string, emit func(agent.Event))
}

func (r *scriptRunner) Run(ctx context.Context, _ session.Session, input string) iter.Seq2[agent.Event, error] {
	r.mu.Lock()
	r.prompts = append(r.prompts, input)
	r.mu.Unlock()
	return func(yield func(agent.Event, error) bool) {
		var seq int64
		r.script(ctx, input, func(ev agent.Event) {
			seq++
			ev.Seq, ev.Timestamp = seq, seq
			yield(ev, nil)
		})
	}
}

func (r *scriptRunner) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.prompts...)
}

func text(s string) agent.Event { return agent.Event{Kind: agent.EventTextDelta, Text: s} }

func done(reason agent.StopReason) agent.Event {
	return agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: reason}}
}

type harness struct {
	t       *testing.T
	m       *model
	printed []string
}

func newHarness(t *testing.T, runner Runner, host *Host) *harness {
	h := &harness{t: t}
	h.m = newModel(context.Background(), Options{Runner: runner, Host: host, Model: "gw/m1", Policy: permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{}), CWD: "/work"})
	h.m.print = func(text string) tea.Cmd {
		h.printed = append(h.printed, ansi.Strip(text))
		return nil
	}
	h.m.Init()
	h.settle()
	h.printed = nil
	return h
}

func (h *harness) send(msg tea.Msg) tea.Cmd {
	_, cmd := h.m.Update(msg)
	h.settle()
	return cmd
}

func (h *harness) settle() {
	for i := 0; h.m.printing && i < 100; i++ {
		h.m.Update(printedMsg{})
	}
}

func (h *harness) step() {
	h.t.Helper()
	if h.m.run == nil {
		h.t.Fatal("no run is active")
	}
	h.send(waitRun(h.m.run.ch)())
}

func (h *harness) pump() {
	h.t.Helper()
	for i := 0; h.m.run != nil; i++ {
		if i > 1000 {
			h.t.Fatal("run did not end")
		}
		h.step()
	}
}

func (h *harness) submit(s string) {
	h.m.input.SetValue(s)
	h.send(keyEnter)
}

func (h *harness) out() string { return strings.Join(h.printed, "\n") }

func (h *harness) view() string { return ansi.Strip(h.m.View().Content) }

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyCtrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
)

func keyRune(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Text: string(r)} }

func assertOrder(t *testing.T, out string, parts ...string) {
	t.Helper()
	at := 0
	for _, p := range parts {
		i := strings.Index(out[at:], p)
		if i < 0 {
			t.Fatalf("missing %q after offset %d in:\n%s", p, at, out)
		}
		at += i + len(p)
	}
}

func TestTurnIsCommittedInOrder(t *testing.T) {
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(text("Hello\nWor"))
		emit(text("ld\n"))
		emit(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 1200, OutputTokens: 30}})
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "bash", ToolInput: json.RawMessage(`{"command":"go test ./..."}`)})
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ok  pkg")}}})
		emit(text("All green."))
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("run the tests")
	h.pump()

	out := h.out()
	assertOrder(t, out, "› run the tests", "● Hello", "World", "● Bash(go test ./...)", "⎿ ok  pkg", "● All green.")
	if strings.Count(out, "Hello") != 1 || strings.Count(out, "All green.") != 1 {
		t.Errorf("text committed more than once:\n%s", out)
	}
	if h.m.state.Running || h.m.run != nil {
		t.Error("run must be finished")
	}
	if !strings.Contains(h.view(), "ctx 1.2k") {
		t.Errorf("status bar must show context size:\n%s", h.view())
	}
}

func TestUnfinishedBlockStaysLive(t *testing.T) {
	release := make(chan struct{})
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(text("Done reading.\n\nfirst line\nsecond"))
		<-release
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("go")
	h.step()

	if !strings.Contains(h.out(), "● Done reading.") || strings.Contains(h.out(), "first line") {
		t.Fatalf("only finished blocks may be committed:\n%s", h.out())
	}
	if !strings.Contains(h.view(), "first line") || !strings.Contains(h.view(), "second") {
		t.Errorf("the unfinished block must be visible in the live area:\n%s", h.view())
	}
	close(release)
	h.pump()
	assertOrder(t, h.out(), "● Done reading.", "first line", "second")
}

func TestRunningTaskCardShowsTheSubagentsActivity(t *testing.T) {
	emitted := make(chan struct{})
	release := make(chan struct{})
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "task", ToolInput: json.RawMessage(`{"description":"hunt the bug","prompt":"x"}`)})
		emit(agent.Event{Kind: agent.EventProgress, ToolUseID: "t1", ToolName: "grep", ToolInput: json.RawMessage(`{"pattern":"truncat"}`), Usage: &llm.Usage{InputTokens: 1000, OutputTokens: 200}})
		close(emitted)
		<-release
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("found it")}}, DurationMs: 2500})
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("delegate")
	<-emitted
	h.step()
	if v := h.view(); !strings.Contains(v, "⎿ Grep(truncat) · 1.2k tokens") {
		t.Fatalf("the running card must show what the subagent is doing:\n%s", v)
	}
	close(release)
	h.pump()
	out := h.out()
	if strings.Contains(out, "Grep(truncat)") {
		t.Errorf("progress is live only and must not reach the scrollback:\n%s", out)
	}
	if !strings.Contains(out, "· 2.5s") {
		t.Errorf("a finished card shows the tool's measured execution time:\n%s", out)
	}
}

func TestStatusBarShowsContextAsAShareOfTheWindow(t *testing.T) {
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 50000, OutputTokens: 4000}})
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.m.opts.Window = func() int { return 200_000 }
	h.submit("go")
	h.pump()
	if v := h.view(); !strings.Contains(v, "ctx 27.0%/200k") {
		t.Errorf("with a known window the status bar shows the share used:\n%s", v)
	}
}

func TestBlankLinesAreTrimmedAtTheEdges(t *testing.T) {
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(text("\n\nHello\n\n\nWorld\n\n"))
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("hi")
	h.pump()
	if !strings.Contains(h.out(), "● Hello\n\n  World") || strings.Contains(h.out(), "Hello\n\n\n") {
		t.Errorf("paragraphs are separated by one blank row, edges trimmed:\n%q", h.out())
	}
	if strings.HasSuffix(h.out(), "\n") {
		t.Errorf("trailing blank lines must be dropped:\n%q", h.out())
	}
}

func TestEnterWhileRunningQueuesTheMessage(t *testing.T) {
	release := make(chan struct{})
	runner := &scriptRunner{script: func(_ context.Context, input string, emit func(agent.Event)) {
		emit(text("working on " + input))
		<-release
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("one")
	h.step()
	h.submit("two")

	if len(h.m.queue) != 1 || !strings.Contains(h.view(), "queued: two") {
		t.Fatalf("message must be queued and shown:\n%s", h.view())
	}
	close(release)
	h.pump()
	if got := runner.seen(); len(got) != 2 || got[1] != "two" {
		t.Errorf("queued message must run after the turn: %q", got)
	}
}

func TestInterruptReturnsQueuedMessagesToTheComposer(t *testing.T) {
	runner := &scriptRunner{script: func(ctx context.Context, _ string, emit func(agent.Event)) {
		emit(text("thinking hard"))
		<-ctx.Done()
		emit(done(agent.ReasonAborted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("one")
	h.step()
	h.submit("two")
	h.send(keyEsc)
	h.pump()

	if got := h.m.input.Value(); got != "two" {
		t.Errorf("queued text must return to the composer, got %q", got)
	}
	if len(runner.seen()) != 1 {
		t.Error("an interrupted turn must not start the queued message")
	}
	assertOrder(t, h.out(), "thinking hard", "⚠ Interrupted")
}

func TestApprovalDock(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	ask := func() chan agent.AskResponse {
		res := make(chan agent.AskResponse, 1)
		go func() {
			r, _ := host.Ask(context.Background(), agent.AskRequest{
				Method: agent.AskConfirm, Title: "Approve bash", Message: "exec tier\n\n{\"command\":\"go test\"}",
			})
			res <- r
		}()
		h.send(waitAsk(host)())
		return res
	}

	res := ask()
	if v := h.view(); !strings.Contains(v, "Approve bash") || !strings.Contains(v, "y approve") {
		t.Fatalf("dock must show the request:\n%s", v)
	}
	h.m.input.SetValue("draft")
	h.send(keyRune('x'))
	if h.m.input.Value() != "draft" {
		t.Error("keys must not reach the composer while approval is pending")
	}
	h.send(keyRune('y'))
	if r := <-res; !r.Confirmed {
		t.Errorf("y must approve: %+v", r)
	}

	res = ask()
	h.send(keyEsc)
	if r := <-res; r.Confirmed || r.Cancelled {
		t.Errorf("esc must deny: %+v", r)
	}
	if len(h.m.asks) != 0 || h.m.input.Value() != "draft" {
		t.Error("dock must close and the draft must survive")
	}
}

func TestHostAskEndsWithItsContext(t *testing.T) {
	host := NewHost()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := host.Ask(ctx, agent.AskRequest{Method: agent.AskConfirm, Title: "x"})
	if err != nil || !r.Cancelled {
		t.Errorf("cancelled context must cancel the ask: %+v %v", r, err)
	}
	if _, err := host.Ask(context.Background(), agent.AskRequest{Method: "bogus"}); err == nil {
		t.Error("unsupported ask methods must fail closed")
	}
	if _, err := host.Ask(context.Background(), agent.AskRequest{Method: agent.AskSelect}); err == nil {
		t.Error("a select without options must fail closed")
	}
}

func TestCtrlC(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.input.SetValue("half typed")
	if cmd := h.send(keyCtrlC); cmd != nil || h.m.quitting || h.m.input.Value() != "" {
		t.Fatal("first ctrl+c must clear the composer")
	}
	cmd := h.send(keyCtrlC)
	if cmd == nil {
		t.Fatal("ctrl+c on an empty composer must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok || !h.m.quitting {
		t.Error("expected a quit command")
	}
	if h.m.View().Content != "" {
		t.Error("the live area must be cleared on exit")
	}
}

func TestSlashCommands(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/help")
	h.submit("/bogus")
	h.submit("/model")
	assertOrder(t, h.out(), "Commands", "Unknown command /bogus", "Model: gw/m1")

	sess := h.m.sess
	created := 0
	h.m.opts.NewSession = func() (session.Session, error) {
		created++
		return session.NewMemory(nil), nil
	}
	h.submit("/clear")
	if h.m.sess != sess {
		t.Error("/clear must keep the conversation on disk and in memory")
	}
	if created != 0 {
		t.Errorf("/clear must not create a session, calls = %d", created)
	}
	if len(h.m.state.Items) != 0 || h.m.printed != 0 {
		t.Error("/clear must clear the transcript")
	}
	if !strings.Contains(h.out(), "screen cleared") {
		t.Errorf("/clear must say what it did:\n%s", h.out())
	}
	h.submit("/new")
	if h.m.sess == sess || len(h.m.state.Items) != 0 {
		t.Error("/new must start a new session and transcript")
	}
	if created != 1 {
		t.Errorf("/new must go through NewSession, calls = %d", created)
	}
	if !strings.Contains(h.out(), "new conversation") {
		t.Errorf("/new must mark the boundary:\n%s", h.out())
	}
}

func TestNewSessionFailureKeepsTheConversation(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	sess := h.m.sess
	h.m.opts.NewSession = func() (session.Session, error) {
		return nil, errors.New("disk full")
	}
	h.submit("/new")
	if h.m.sess != sess {
		t.Error("a failed /new must keep the current session")
	}
	if !strings.Contains(h.out(), "disk full") {
		t.Errorf("the reason must be shown:\n%s", h.out())
	}
}

func TestBannerAnnouncesAResumedSession(t *testing.T) {
	dir := t.TempDir()
	s, err := session.Create(dir, "20261117-102030-abcd", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("earlier work"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := session.Load(filepath.Join(dir, "20261117-102030-abcd"+session.FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()

	h := &harness{t: t}
	h.m = newModel(context.Background(), Options{Runner: &scriptRunner{}, Session: loaded, Model: "gw/m1", CWD: "/work"})
	h.m.print = func(text string) tea.Cmd {
		h.printed = append(h.printed, ansi.Strip(text))
		return nil
	}
	h.m.Init()
	h.settle()
	if !strings.Contains(h.out(), "Resumed session with 1 messages") || !strings.Contains(h.out(), "20261117-102030-abcd") {
		t.Errorf("the banner must say what was resumed:\n%s", h.out())
	}
}

func TestHistory(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/help")
	h.submit("/model")
	for _, step := range []struct {
		key  tea.KeyPressMsg
		want string
	}{{keyUp, "/model"}, {keyUp, "/help"}, {keyUp, "/help"}, {keyDown, "/model"}, {keyDown, ""}} {
		h.send(step.key)
		if got := h.m.input.Value(); got != step.want {
			t.Fatalf("after %s want %q, got %q", step.key, step.want, got)
		}
	}
}

func TestToolTarget(t *testing.T) {
	cases := []struct {
		name, input, want string
	}{
		{"bash", `{"command":"go test ./...\necho done"}`, "go test ./... ..."},
		{"read", `{"path":"agent/loop.go","offset":120}`, "agent/loop.go:120"},
		{"edit", `{"path":"a.go","old_string":"x","new_string":"y"}`, "a.go"},
		{"grep", `{"pattern":"func Run","path":"agent"}`, "func Run in agent"},
		{"glob", `{"pattern":"**/*.go"}`, "**/*.go"},
		{"mcp__x", `{"q":"z"}`, `{"q":"z"}`},
		{"exit_plan_mode", `{"plan":"# Plan\n1. step"}`, ""},
		{"bash", `not json`, ""},
	}
	for _, c := range cases {
		if got := toolTarget(c.name, json.RawMessage(c.input)); got != c.want {
			t.Errorf("%s %s: got %q, want %q", c.name, c.input, got, c.want)
		}
	}
}

func TestEditCardShowsDiff(t *testing.T) {
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "e1", ToolName: "edit",
			ToolInput: json.RawMessage(`{"path":"a.go","old_string":"x := 1","new_string":"x := 2"}`)})
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "e1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("Edited a.go")}}})
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("fix")
	h.pump()
	assertOrder(t, h.out(), "● Edit(a.go)", "⎿ Edited a.go", "- x := 1", "+ x := 2")
}

func TestRowsFitTheTerminal(t *testing.T) {
	long := strings.Repeat("wide 宽字符 text ", 40)
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(text(long + "\n" + long))
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "bash", ToolInput: json.RawMessage(`{"command":"` + long + `"}`)})
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(long)}, IsError: true}})
		emit(done(agent.ReasonError))
	}}
	h := newHarness(t, runner, nil)
	h.send(tea.WindowSizeMsg{Width: 60, Height: 20})
	h.submit(long)
	h.pump()
	h.m.queue = []string{long}
	for _, block := range append(h.printed, h.view()) {
		for _, row := range strings.Split(block, "\n") {
			if w := ansi.StringWidth(row); w > 59 {
				t.Errorf("row is %d cells wide, terminal is 60: %q", w, row)
			}
		}
	}
}

func TestSelectApprovalDock(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	options := []string{"Yes", "Yes, and don't ask again this session for bash(go test *)", "No"}
	ask := func() chan agent.AskResponse {
		res := make(chan agent.AskResponse, 1)
		go func() {
			r, _ := host.Ask(context.Background(), agent.AskRequest{
				Method: agent.AskSelect, Title: "Approve bash", Message: "command: go test ./...", Options: options,
			})
			res <- r
		}()
		h.send(waitAsk(host)())
		return res
	}

	res := ask()
	v := h.view()
	for _, want := range []string{"Approve bash", "command: go test ./...", "1 Yes", "2 Yes, and don't ask again", "3 No", "press 1-3"} {
		if !strings.Contains(v, want) {
			t.Errorf("dock missing %q:\n%s", want, v)
		}
	}
	h.send(keyRune('2'))
	if r := <-res; r.Value != options[1] {
		t.Errorf("2 must pick the second option: %+v", r)
	}

	res = ask()
	h.send(keyRune('y'))
	if r := <-res; r.Value != "Yes" {
		t.Errorf("y must pick Yes: %+v", r)
	}

	res = ask()
	h.send(keyRune('n'))
	if r := <-res; r.Value != "No" {
		t.Errorf("n must pick No: %+v", r)
	}

	res = ask()
	h.send(keyRune('7'))
	if len(h.m.asks) != 1 {
		t.Fatal("an out-of-range digit must be ignored")
	}
	h.send(keyEsc)
	if r := <-res; !r.Cancelled {
		t.Errorf("esc must cancel a select: %+v", r)
	}
}

func TestPlanIsPrintedBeforeTheApproval(t *testing.T) {
	host := NewHost()
	plan := "# Retry plan\n1. Wrap the client\n\n2. Verify with go test"
	options := []string{"Yes, and auto-accept edits", "Yes, and manually approve edits", "No, keep planning"}
	answer := make(chan agent.AskResponse, 1)
	runner := &scriptRunner{script: func(ctx context.Context, _ string, emit func(agent.Event)) {
		input, _ := json.Marshal(map[string]string{"plan": plan})
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "p1", ToolName: "exit_plan_mode", ToolInput: input})
		r, _ := host.Ask(ctx, agent.AskRequest{Method: agent.AskSelect, Title: "Approve the plan", Message: plan, Options: options})
		answer <- r
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "p1", ToolResult: &agent.ToolResult{
			Content: []llm.ContentBlock{llm.TextBlock("Plan approved, mode is now acceptEdits.\nPlan mode is over. Make the changes now.")},
		}})
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, host)
	h.submit("plan it")
	h.step()
	assertOrder(t, h.out(), "› plan it", "◆ Plan", "Retry plan", "1. Wrap the client", "2. Verify with go test")
	if strings.Contains(h.out(), "# Retry plan") {
		t.Errorf("the plan is rendered as markdown:\n%s", h.out())
	}

	h.send(waitAsk(host)())
	if v := h.view(); !strings.Contains(v, "Approve the plan") || !strings.Contains(v, "1 Yes, and auto-accept edits") ||
		strings.Contains(v, "Wrap the client") {
		t.Errorf("the dock must hold the choice without repeating the plan:\n%s", v)
	}
	h.send(keyRune('1'))
	if r := <-answer; r.Value != options[0] {
		t.Errorf("1 must approve: %+v", r)
	}
	h.pump()

	out := h.out()
	if strings.Count(out, "Wrap the client") != 1 {
		t.Errorf("the plan must be printed exactly once:\n%s", out)
	}
	assertOrder(t, out, "Retry plan", "● Exit plan mode", "⎿ Plan approved, mode is now acceptEdits.")
	if strings.Contains(out, "Make the changes now") || strings.Contains(out, `"plan"`) {
		t.Errorf("the card must not show the instruction meant for the model or the raw input:\n%s", out)
	}
}

func TestShiftTabCyclesApprovalMode(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	shiftTab := tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	for _, want := range []permission.Mode{permission.ModePlan, permission.ModeDefault, permission.ModeAcceptEdits} {
		h.send(shiftTab)
		if got := h.m.opts.Policy.Mode(); got != want {
			t.Fatalf("want %s, got %s", want, got)
		}
		if !strings.Contains(h.view(), string(want)+" · ctx") {
			t.Errorf("status bar must show %s:\n%s", want, h.view())
		}
	}

	bypass := newModel(context.Background(), Options{Policy: permission.NewPolicy(permission.ModeBypass, permission.Rules{})})
	if len(bypass.modes) != 4 {
		t.Errorf("bypass stays reachable only when the session started in it: %v", bypass.modes)
	}
}

func TestCompactCommandCompactsAndKeepsTheConversation(t *testing.T) {
	runner := &scriptRunner{script: func(_ context.Context, input string, emit func(agent.Event)) {
		emit(text("answered " + input))
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("one")
	h.pump()
	if !strings.Contains(h.out(), "answered one") {
		t.Fatalf("the first turn must be visible:\n%s", h.out())
	}

	var focus string
	h.m.opts.Compact = func(_ context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error] {
		focus = instructions
		return func(yield func(agent.Event, error) bool) {
			if sess == nil {
				t.Error("the compactor must be given the live session")
			}
			yield(agent.Event{Kind: agent.EventCompaction, Compaction: &session.Compaction{
				Trigger: session.TriggerManual, Elided: 2, Summarized: 4, TokensBefore: 120_000, TokensAfter: 30_000,
			}}, nil)
			yield(done(agent.ReasonCompleted), nil)
		}
	}

	h.submit("/compact keep the failing test")
	h.pump()

	if focus != "keep the failing test" {
		t.Errorf("the focus text reaches the compactor, got %q", focus)
	}
	if got := strings.Join(strings.Fields(h.out()), " "); !strings.Contains(got, "Context compacted: 120.0k → 30.0k tokens") ||
		!strings.Contains(got, "2 tool results elided") || !strings.Contains(got, "4 messages summarized") {
		t.Errorf("compaction is reported with its numbers:\n%s", h.out())
	}
	if got := h.out(); !strings.Contains(got, "answered one") {
		t.Errorf("the earlier conversation must stay on screen:\n%s", got)
	}
}

const nothingToCompactText = "Nothing to compact yet: the conversation already fits in the recent window."

func TestCompactCommandSaysWhenThereIsNothingToDo(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.opts.Compact = func(context.Context, session.Session, string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			yield(agent.Event{Kind: agent.EventCompaction, Message: nothingToCompactText}, nil)
			yield(done(agent.ReasonCompleted), nil)
		}
	}
	h.submit("/compact")
	h.pump()
	if !strings.Contains(h.out(), nothingToCompactText) {
		t.Errorf("the reason must be shown:\n%s", h.out())
	}
}

func TestCompactCommandRefusesWhileATurnRuns(t *testing.T) {
	release := make(chan struct{})
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(text("working"))
		<-release
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("one")
	h.step()
	h.submit("/compact")
	if v := h.view(); !strings.Contains(v, "press Esc before compacting") {
		t.Errorf("a running turn blocks compaction:\n%s", v)
	}
	close(release)
	h.pump()
}

func TestCompactCommandWithoutACompactor(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/compact")
	if got := h.out(); !strings.Contains(got, "Compaction is not available") {
		t.Errorf("without a compactor the command explains itself:\n%s", got)
	}
}
