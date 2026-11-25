package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

var errScriptExhausted = errors.New("scripted model exhausted")

type scriptedModel struct {
	turns [][]llm.StreamEvent
	calls int
}

func (m *scriptedModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if m.calls >= len(m.turns) {
			yield(llm.StreamEvent{}, errScriptExhausted)
			return
		}
		turn := m.turns[m.calls]
		m.calls++
		for _, ev := range turn {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func textTurn(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEMessageStart},
		{Type: llm.SEBlockStart, BlockIndex: 0, Block: llm.ContentBlock{Type: llm.BlockText}},
		{Type: llm.SETextDelta, BlockIndex: 0, Text: text},
		{Type: llm.SEBlockStop, BlockIndex: 0},
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
		{Type: llm.SEMessageStop},
	}
}

func toolTurn(id, name, input string, reason llm.StopReason) []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEMessageStart},
		{Type: llm.SEBlockStart, BlockIndex: 0, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(input)}},
		{Type: llm.SEBlockStop, BlockIndex: 0},
		{Type: llm.SEMessageDelta, StopReason: reason},
		{Type: llm.SEMessageStop},
	}
}

type echoTool struct {
	readOnly bool
	concSafe bool
	called   int
}

func (e *echoTool) Name() string                                 { return "echo" }
func (e *echoTool) Description() string                          { return "echo tool" }
func (e *echoTool) Schema() json.RawMessage                      { return json.RawMessage(`{"type":"object"}`) }
func (e *echoTool) IsReadOnly() bool                             { return e.readOnly }
func (e *echoTool) IsConcurrencySafe(input json.RawMessage) bool { return e.concSafe }
func (e *echoTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	e.called++
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("echoed")}}
}

func collectEvents(seq iter.Seq2[Event, error]) ([]Event, error) {
	var evs []Event
	for ev, err := range seq {
		if err != nil {
			return evs, err
		}
		evs = append(evs, ev)
	}
	return evs, nil
}

func lastResult(evs []Event) *Result {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind == EventResult {
			return evs[i].Result
		}
	}
	return nil
}

func TestLoopTextOnly(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{textTurn("hi")}}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "hello"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("want Completed, got %+v", r)
	}
	if r.Text != "hi" {
		t.Errorf("want text hi, got %q", r.Text)
	}
	if r.Turns != 0 {
		t.Errorf("want 0 turns, got %d", r.Turns)
	}
}

func TestLoopSingleToolTurn(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "echo", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	reg := NewRegistry()
	echo := &echoTool{readOnly: true, concSafe: true}
	reg.Register(echo)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if echo.called != 1 {
		t.Errorf("want echo called once, got %d", echo.called)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted || r.Text != "done" {
		t.Fatalf("want Completed/done, got %+v", r)
	}
	if r.Turns != 1 {
		t.Errorf("want 1 turn, got %d", r.Turns)
	}
	var found bool
	for _, ev := range evs {
		if ev.Kind == EventToolResult && ev.ToolUseID == "t1" {
			found = true
		}
	}
	if !found {
		t.Error("no tool_result event for t1")
	}
}

func TestLoopTruncationGate(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "echo", "{bad", llm.StopReasonLength),
		textTurn("recovered"),
	}}
	reg := NewRegistry()
	echo := &echoTool{readOnly: true, concSafe: true}
	reg.Register(echo)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if echo.called != 0 {
		t.Errorf("truncated tool must not execute, called %d", echo.called)
	}
	var isErr bool
	for _, ev := range evs {
		if ev.Kind == EventToolResult && ev.ToolUseID == "t1" && ev.ToolResult != nil && ev.ToolResult.IsError {
			isErr = true
		}
	}
	if !isErr {
		t.Error("want synthesized isError tool_result for truncated tool")
	}
	if text, _, _ := toolResultOf(evs, "t1"); text != truncatedToolMsg {
		t.Errorf("want omp truncation text, got %q", text)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted || r.Text != "recovered" {
		t.Fatalf("want recovered Completed, got %+v", r)
	}
	if r.Turns != 0 {
		t.Errorf("truncation retry must not count turns, got %d", r.Turns)
	}
}

func TestLoopMaxTurns(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "echo", "{}", llm.StopReasonToolUse),
		toolTurn("t2", "echo", "{}", llm.StopReasonToolUse),
		toolTurn("t3", "echo", "{}", llm.StopReasonToolUse),
	}}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{MaxTurns: 2})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonMaxTurns {
		t.Fatalf("want MaxTurns, got %+v", r)
	}
	if r.Turns != 2 {
		t.Errorf("want 2 turns, got %d", r.Turns)
	}
}

type funcTool struct {
	name string
	exec func() ToolResult
}

func (f funcTool) Name() string            { return f.name }
func (f funcTool) Description() string     { return f.name }
func (f funcTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f funcTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	return f.exec()
}

func toolUse(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func multiToolTurn(uses ...llm.ContentBlock) []llm.StreamEvent {
	evs := []llm.StreamEvent{{Type: llm.SEMessageStart}}
	for i, u := range uses {
		evs = append(evs,
			llm.StreamEvent{Type: llm.SEBlockStart, BlockIndex: i, Block: u},
			llm.StreamEvent{Type: llm.SEBlockStop, BlockIndex: i},
		)
	}
	return append(evs,
		llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
		llm.StreamEvent{Type: llm.SEMessageStop},
	)
}

func toolResultOf(evs []Event, id string) (string, bool, bool) {
	for _, ev := range evs {
		if ev.Kind != EventToolResult || ev.ToolUseID != id || ev.ToolResult == nil {
			continue
		}
		var sb strings.Builder
		for _, b := range ev.ToolResult.Content {
			sb.WriteString(b.Text)
		}
		return sb.String(), ev.ToolResult.IsError, true
	}
	return "", false, false
}

func TestLoopToolPanicIsolated(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("t1", "boom"), toolUse("t2", "echo")),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(funcTool{name: "boom", exec: func() ToolResult { panic("kaboom") }})
	echo := &echoTool{readOnly: true, concSafe: true}
	reg.Register(echo)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	text, isErr, ok := toolResultOf(evs, "t1")
	if !ok || !isErr || text != "Tool boom panicked: kaboom" {
		t.Errorf("want isError panic result, got ok=%v isErr=%v text=%q", ok, isErr, text)
	}
	if echo.called != 1 {
		t.Errorf("sibling tool must still run, called %d", echo.called)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted || r.Text != "done" {
		t.Fatalf("want Completed/done, got %+v", r)
	}
}

func TestLoopUnknownToolSuggests(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "Echo", "{}", llm.StopReasonToolUse),
		textTurn("ok"),
	}}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	text, isErr, _ := toolResultOf(evs, "t1")
	if !isErr || text != "Tool Echo not found. Did you mean echo?" {
		t.Errorf("got isErr=%v text=%q", isErr, text)
	}
}

func TestLoopEmptyErrorResult(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "mute", "{}", llm.StopReasonToolUse),
		textTurn("ok"),
	}}
	reg := NewRegistry()
	reg.Register(funcTool{name: "mute", exec: func() ToolResult {
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("  ")}, IsError: true}
	}})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	text, isErr, _ := toolResultOf(evs, "t1")
	if !isErr || text != emptyErrorText {
		t.Errorf("got isErr=%v text=%q", isErr, text)
	}
}

func TestToolNotFoundMessage(t *testing.T) {
	names := []string{"read", "grep", "mcp__github_create_issue", "mcp__x_read"}
	cases := []struct{ name, want string }{
		{"zzz", "Tool zzz not found"},
		{"Grep", "Tool Grep not found. Did you mean grep?"},
		{"mcp__github__create_issue", "Tool mcp__github__create_issue not found. Did you mean mcp__github_create_issue?"},
		{"fs_read", "Tool fs_read not found. Closest available: read, mcp__x_read"},
		{"get_id", "Tool get_id not found"},
	}
	for _, c := range cases {
		if got := toolNotFound(c.name, names); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	many := []string{"a_read", "b_read", "c_read", "d_read"}
	if got, want := toolNotFound("x_read", many), "Tool x_read not found. Closest available: a_read, b_read, c_read"; got != want {
		t.Errorf("cap: got %q, want %q", got, want)
	}
}

func TestDefaultsUnchanged(t *testing.T) {
	if defaultMaxTruncRetries != 3 || minSuggestSegment != 3 || maxSuggestions != 3 {
		t.Errorf("defaults drifted: truncRetries=%d minSegment=%d maxSuggestions=%d",
			defaultMaxTruncRetries, minSuggestSegment, maxSuggestions)
	}
	if emptyErrorText != "Tool failed with no output." {
		t.Errorf("emptyErrorText drifted: %q", emptyErrorText)
	}
	if toolAborted != "Tool execution was aborted." {
		t.Errorf("toolAborted drifted: %q", toolAborted)
	}
}

func TestLoopStreamCancelIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 先吐出一段文字再取消，模拟用户在模型输出途中按停止
	call := func(callCtx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			for _, ev := range textTurn("partial")[:3] {
				if !yield(ev, nil) {
					return
				}
			}
			cancel()
			yield(llm.StreamEvent{}, callCtx.Err())
		}
	}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: call}, nil, Options{})
	evs, err := collectEvents(l.Run(ctx, session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonAborted || r.Err != context.Canceled.Error() {
		t.Fatalf("want Aborted/context canceled, got %+v", r)
	}
}

func TestLoopAdapterAbortIsAborted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 照真实 adapter 的做法，取消后不报错，只补一个停止原因为 aborted 的事件
	call := func(_ context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			for _, ev := range textTurn("partial")[:3] {
				if !yield(ev, nil) {
					return
				}
			}
			cancel()
			yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonAborted}, nil)
		}
	}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: call}, nil, Options{})
	evs, err := collectEvents(l.Run(ctx, session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonAborted || r.Err != context.Canceled.Error() {
		t.Fatalf("want Aborted/context canceled, got %+v", r)
	}
}

type meetTool struct{ wg sync.WaitGroup }

func (m *meetTool) Name() string                           { return "meet" }
func (m *meetTool) Description() string                    { return "meet" }
func (m *meetTool) Schema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (m *meetTool) IsReadOnly() bool                       { return true }
func (m *meetTool) IsConcurrencySafe(json.RawMessage) bool { return true }
func (m *meetTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	m.wg.Done()
	met := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(met)
	}()
	// 两次调用都走到这里才算同时在跑，等不到对方说明被串行执行了
	select {
	case <-met:
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("met")}}
	case <-time.After(time.Second):
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("alone")}, IsError: true}
	}
}

func TestLoopDefaultConcurrencyUnbounded(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("t1", "meet"), toolUse("t2", "meet")),
		textTurn("done"),
	}}
	meet := &meetTool{}
	meet.wg.Add(2)
	reg := NewRegistry()
	reg.Register(meet)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	for _, id := range []string{"t1", "t2"} {
		if text, isErr, _ := toolResultOf(evs, id); isErr || text != "met" {
			t.Errorf("%s: want met, got isErr=%v text=%q", id, isErr, text)
		}
	}
}

// 判断能否并行时会崩溃的工具，用来验证崩溃后按不能并行处理
type shakyTool struct{ called int }

func (s *shakyTool) Name() string                           { return "shaky" }
func (s *shakyTool) Description() string                    { return "shaky" }
func (s *shakyTool) Schema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (s *shakyTool) IsReadOnly() bool                       { return true }
func (s *shakyTool) IsConcurrencySafe(json.RawMessage) bool { panic("bad input") }
func (s *shakyTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	s.called++
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ran")}}
}

func TestLoopSafetyCheckPanicRunsExclusive(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "shaky", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	shaky := &shakyTool{}
	reg := NewRegistry()
	reg.Register(shaky)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	if l.makeSafe()(toolUse("t1", "shaky")) {
		t.Error("panicking safety check must count as not safe")
	}
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if shaky.called != 1 {
		t.Errorf("tool must still run once, called %d", shaky.called)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted || r.Text != "done" {
		t.Fatalf("want Completed/done, got %+v", r)
	}
}

type approvingTool struct {
	tier   Tier
	panics bool
	called int
}

func (a *approvingTool) Name() string                           { return "approving" }
func (a *approvingTool) Description() string                    { return "approving" }
func (a *approvingTool) Schema() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (a *approvingTool) IsReadOnly() bool                       { return true }
func (a *approvingTool) IsConcurrencySafe(json.RawMessage) bool { return true }
func (a *approvingTool) Approval(json.RawMessage) Approval {
	if a.panics {
		panic("bad approval")
	}
	return Approval{Tier: a.tier, Reason: "needs review"}
}
func (a *approvingTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	a.called++
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ran")}}
}

func approvalOf(evs []Event, id string) *Approval {
	for _, ev := range evs {
		if ev.Kind == EventToolUse && ev.ToolUseID == id {
			return ev.Approval
		}
	}
	return nil
}

// 声明了档位的工具 派发事件带出该档位
func TestToolUseEventCarriesApproval(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "approving", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(&approvingTool{tier: TierWrite})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	a := approvalOf(evs, "t1")
	if a == nil || a.Tier != TierWrite || a.Reason != "needs review" {
		t.Errorf("want write tier with reason, got %+v", a)
	}
}

// 未声明档位的工具按 exec 处理 未知工具不带档位
func TestApprovalDefaultsToExec(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("t1", "echo"), toolUse("t2", "nope")),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a := approvalOf(evs, "t1"); a == nil || a.Tier != TierExec {
		t.Errorf("undeclared tool must default to exec, got %+v", a)
	}
	if a := approvalOf(evs, "t2"); a != nil {
		t.Errorf("unknown tool must carry no approval, got %+v", a)
	}
}

// Approval 本身崩溃时按 exec 处理 运行照常完成
func TestApprovalPanicFallsBackToExec(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "approving", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	tool := &approvingTool{tier: TierRead, panics: true}
	reg := NewRegistry()
	reg.Register(tool)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a := approvalOf(evs, "t1"); a == nil || a.Tier != TierExec {
		t.Errorf("panicking approval must fall back to exec, got %+v", a)
	}
	if tool.called != 1 {
		t.Errorf("tool must still run once, called %d", tool.called)
	}
	r := lastResult(evs)
	if r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("want Completed, got %+v", r)
	}
}

func TestEventSeqIsMonotonicAndUnique(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "echo", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(evs) == 0 {
		t.Fatal("no events")
	}
	prev := int64(0)
	for i, ev := range evs {
		if ev.Seq <= prev {
			t.Fatalf("event %d: Seq %d not greater than previous %d", i, ev.Seq, prev)
		}
		prev = ev.Seq
		if ev.Timestamp <= 0 {
			t.Errorf("event %d: missing timestamp", i)
		}
	}
}

func TestEventSeqUniqueUnderConcurrency(t *testing.T) {
	meet := &meetTool{}
	meet.wg.Add(2)
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("t1", "meet"), toolUse("t2", "meet")),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(meet)
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	seen := map[int64]bool{}
	for _, ev := range evs {
		if seen[ev.Seq] {
			t.Fatalf("duplicate Seq %d under concurrent tool execution", ev.Seq)
		}
		seen[ev.Seq] = true
	}
}

func TestMessageTimestampsRecorded(t *testing.T) {
	fixed := time.Date(2026, 11, 12, 10, 30, 0, 0, time.UTC)
	m := &scriptedModel{turns: [][]llm.StreamEvent{toolTurn("t1", "echo", "{}", llm.StopReasonToolUse), textTurn("done")}}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	sess := session.NewMemory(nil)
	l := NewLoop(reg, RunDeps{CallModel: m.call, Now: func() time.Time { return fixed }}, nil, Options{})
	if _, err := collectEvents(l.Run(context.Background(), sess, "go")); err != nil {
		t.Fatalf("err: %v", err)
	}
	msgs := sess.Messages()
	if len(msgs) == 0 {
		t.Fatal("no messages")
	}
	for i, msg := range msgs {
		if msg.Timestamp == 0 {
			t.Errorf("message %d (%s) has no timestamp", i, msg.Role)
		}
		if msg.Timestamp != fixed.UnixMilli() {
			t.Errorf("message %d: want injected clock %d, got %d", i, fixed.UnixMilli(), msg.Timestamp)
		}
	}
}
