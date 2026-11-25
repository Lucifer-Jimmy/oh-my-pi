package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"strings"
	"sync"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type toolCallModel struct {
	mu    sync.Mutex
	turns int
}

func (m *toolCallModel) callFrom(script func(n int, req llm.CompletionRequest) []llm.StreamEvent) func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			m.mu.Lock()
			m.turns++
			n := m.turns
			m.mu.Unlock()
			evs := append([]llm.StreamEvent{{Type: llm.SEMessageStart}}, script(n, req)...)
			evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStop})
			for _, ev := range evs {
				if !yield(ev, nil) {
					return
				}
			}
		}
	}
}

func hookTextTurn(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
		{Type: llm.SETextDelta, Text: text},
		{Type: llm.SEBlockStop},
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
	}
}

func hookToolTurn(id, name string, input map[string]any) []llm.StreamEvent {
	raw, _ := json.Marshal(input)
	return []llm.StreamEvent{
		{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: raw}},
		{Type: llm.SEBlockStop},
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
	}
}

type recorder struct {
	name  string
	args  json.RawMessage
	panix bool
}

func (r *recorder) Name() string            { return r.name }
func (r *recorder) Description() string     { return "records arguments" }
func (r *recorder) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r *recorder) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	r.args = args
	if r.panix {
		panic("boom")
	}
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("recorded")}}
}

func runLoop(t *testing.T, loop *Loop) ([]Event, *Result) {
	t.Helper()
	var evs []Event
	var res *Result
	for ev, err := range loop.Run(context.Background(), session.NewMemory(nil), "go") {
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev)
		if ev.Kind == EventResult {
			res = ev.Result
		}
	}
	return evs, res
}

type preHook struct {
	block bool
	new   json.RawMessage
	err   error
}

func (h *preHook) BeforeToolCall(ctx context.Context, name string, input json.RawMessage) (Decision, error) {
	if h.err != nil {
		return Decision{}, h.err
	}
	return Decision{Block: h.block, Reason: "no", NewInput: h.new}, nil
}

func TestPreToolHookCanBlock(t *testing.T) {
	rec := &recorder{name: "rec"}
	reg := NewRegistry()
	reg.Register(rec)
	model := &toolCallModel{}
	loop := NewLoop(reg, RunDeps{CallModel: model.callFrom(func(n int, _ llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			return hookToolTurn("t1", "rec", map[string]any{"a": 1})
		}
		return hookTextTurn("done")
	})}, nil, Options{Hooks: Hooks{PreTool: &preHook{block: true}}})

	evs, res := runLoop(t, loop)
	if rec.args != nil {
		t.Fatalf("a blocked tool must not run")
	}
	var blocked bool
	for _, ev := range evs {
		if ev.Kind == EventToolResult && ev.ToolResult != nil && ev.ToolResult.IsError {
			blocked = true
		}
	}
	if !blocked {
		t.Fatalf("the model must see a blocking error result")
	}
	if res == nil || res.Reason != ReasonCompleted {
		t.Fatalf("the run continues: %+v", res)
	}
}

func TestPreToolHookRewriteReachesTheTool(t *testing.T) {
	rec := &recorder{name: "rec"}
	reg := NewRegistry()
	reg.Register(rec)
	model := &toolCallModel{}
	rewritten := json.RawMessage(`{"a":2}`)
	loop := NewLoop(reg, RunDeps{CallModel: model.callFrom(func(n int, _ llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			return hookToolTurn("t1", "rec", map[string]any{"a": 1})
		}
		return hookTextTurn("done")
	})}, nil, Options{Hooks: Hooks{PreTool: &preHook{new: rewritten}}})

	runLoop(t, loop)
	if string(rec.args) != `{"a":2}` && string(rec.args) != `{"a": 2}` {
		t.Fatalf("the hook's rewrite must be what the tool receives, got %s", rec.args)
	}
}

func TestPreToolHookErrorDoesNotBlock(t *testing.T) {
	rec := &recorder{name: "rec"}
	reg := NewRegistry()
	reg.Register(rec)
	model := &toolCallModel{}
	loop := NewLoop(reg, RunDeps{CallModel: model.callFrom(func(n int, _ llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			return hookToolTurn("t1", "rec", map[string]any{"a": 1})
		}
		return hookTextTurn("done")
	})}, nil, Options{Hooks: Hooks{PreTool: &preHook{err: errors.New("hook broke")}}})

	evs, _ := runLoop(t, loop)
	if rec.args == nil {
		t.Fatalf("a failing observer hook must not stop the tool")
	}
	var notified bool
	for _, ev := range evs {
		if ev.Kind == EventNotification && strings.Contains(ev.Message, "pre-tool hook failed") {
			notified = true
		}
	}
	if !notified {
		t.Errorf("a hook failure must be visible")
	}
}

type postHook struct{}

func (postHook) AfterToolCall(ctx context.Context, name string, res ToolResult) (*ToolResult, error) {
	return &ToolResult{Content: []llm.ContentBlock{llm.TextBlock("rewritten by hook")}}, nil
}

func TestPostToolHookRewritesTheResult(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&recorder{name: "rec"})
	model := &toolCallModel{}
	loop := NewLoop(reg, RunDeps{CallModel: model.callFrom(func(n int, _ llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			return hookToolTurn("t1", "rec", map[string]any{})
		}
		return hookTextTurn("done")
	})}, nil, Options{Hooks: Hooks{PostTool: postHook{}}})

	evs, _ := runLoop(t, loop)
	var got string
	for _, ev := range evs {
		if ev.Kind == EventToolResult && ev.ToolResult != nil {
			for _, b := range ev.ToolResult.Content {
				got += b.Text
			}
		}
	}
	if !strings.Contains(got, "rewritten by hook") {
		t.Fatalf("result = %q", got)
	}
}

type stopHook struct {
	mu sync.Mutex
	n  int
}

func (h *stopHook) OnStop(ctx context.Context) (StopDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	return StopDecision{Prevent: h.n == 1, Reason: "one more thing"}, nil
}

func TestStopHookPreventsTheFirstCompletion(t *testing.T) {
	model := &toolCallModel{}
	loop := NewLoop(NewRegistry(), RunDeps{CallModel: model.callFrom(func(n int, _ llm.CompletionRequest) []llm.StreamEvent {
		return hookTextTurn("answer")
	})}, nil, Options{Hooks: Hooks{Stop: &stopHook{}}})

	_, res := runLoop(t, loop)
	if model.turns != 2 {
		t.Fatalf("a prevented stop must cause another model call, got %d", model.turns)
	}
	if res == nil || res.Reason != ReasonCompleted {
		t.Fatalf("second stop is honoured: %+v", res)
	}
}

type promptHook struct {
	mu sync.Mutex
	n  int
}

func (h *promptHook) OnPrompt(ctx context.Context) ([]llm.Message, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	if h.n > 1 {
		return nil, nil
	}
	return []llm.Message{llm.UserText("remember: be brief")}, nil
}

func TestPromptHookInjectsBeforeTheFirstRequest(t *testing.T) {
	model := &toolCallModel{}
	var first []llm.Message
	loop := NewLoop(NewRegistry(), RunDeps{CallModel: model.callFrom(func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			first = req.Messages
		}
		return hookTextTurn("ok")
	})}, nil, Options{Hooks: Hooks{Prompt: &promptHook{}}})

	runLoop(t, loop)
	found := false
	for _, m := range first {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "be brief") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("the injected message must be in the first request: %+v", first)
	}
}

type failingPromptHook struct{}

func (failingPromptHook) OnPrompt(ctx context.Context) ([]llm.Message, error) {
	return []llm.Message{llm.UserText("delivered before the failure")}, errors.New("second hook broke")
}

func TestAFailingPromptHookKeepsWhatEarlierHooksReturned(t *testing.T) {
	model := &toolCallModel{}
	var first []llm.Message
	loop := NewLoop(NewRegistry(), RunDeps{CallModel: model.callFrom(func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			first = req.Messages
		}
		return hookTextTurn("ok")
	})}, nil, Options{Hooks: Hooks{Prompt: failingPromptHook{}}})

	var notes []string
	for ev, err := range loop.Run(context.Background(), session.NewMemory(nil), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == EventNotification {
			notes = append(notes, ev.Message)
		}
	}
	found := false
	for _, m := range first {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "delivered before the failure") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("messages returned with the error must still be injected: %+v", first)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "second hook broke") {
		t.Fatalf("the failure is still reported once: %v", notes)
	}
}
