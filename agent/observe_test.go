package agent

import (
	"context"
	"encoding/json"
	"iter"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func TestToolResultCarriesItsOwnExecutionTime(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("slow", "slow"), toolUse("quick", "quick")),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(funcTool{name: "slow", exec: func() ToolResult {
		time.Sleep(120 * time.Millisecond)
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("slow")}}
	}})
	reg.Register(funcTool{name: "quick", exec: func() ToolResult {
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("quick")}}
	}})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	took := map[string]int64{}
	for _, ev := range evs {
		if ev.Kind == EventToolResult {
			took[ev.ToolUseID] = ev.DurationMs
		}
	}
	if took["slow"] < 120 {
		t.Errorf("the slow tool ran for at least 120ms, reported %dms", took["slow"])
	}
	if took["quick"] >= 100 {
		t.Errorf("the quick tool queued behind the slow one; its duration must not include the wait: %dms", took["quick"])
	}
}

func TestUsageEventsCarryStopReasonAndModelTime(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "echo", "{}", llm.StopReasonLength),
		textTurn("done"),
	}}
	call := func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		time.Sleep(40 * time.Millisecond)
		return m.call(ctx, req)
	}
	reg := NewRegistry()
	reg.Register(&echoTool{readOnly: true, concSafe: true})
	l := NewLoop(reg, RunDeps{CallModel: call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	var usages []Event
	for _, ev := range evs {
		if ev.Kind == EventUsage {
			usages = append(usages, ev)
		}
	}
	if len(usages) != 2 {
		t.Fatalf("want one usage event per model response, got %d", len(usages))
	}
	if usages[0].StopReason != llm.StopReasonLength || usages[1].StopReason != llm.StopReasonStop {
		t.Errorf("each response must say why it stopped: %q, %q", usages[0].StopReason, usages[1].StopReason)
	}
	for i, u := range usages {
		if u.DurationMs < 40 {
			t.Errorf("response %d took at least 40ms, reported %dms", i, u.DurationMs)
		}
	}
}

type emitTool struct{}

func (emitTool) Name() string            { return "emit" }
func (emitTool) Description() string     { return "emits events" }
func (emitTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (emitTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	tc.Emit(Event{Kind: EventProgress, Message: "halfway"})
	tc.Emit(Event{Kind: EventProgress, ToolUseID: "elsewhere", Message: "relayed"})
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ok")}}
}

func TestToolEmittedEventsCarryTheCallingToolID(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "emit", "{}", llm.StopReasonToolUse),
		textTurn("done"),
	}}
	reg := NewRegistry()
	reg.Register(emitTool{})
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, ev := range evs {
		if ev.Kind == EventProgress {
			got[ev.Message] = ev.ToolUseID
		}
	}
	if got["halfway"] != "t1" {
		t.Errorf("a tool's own event must be attributed to its call: %q", got["halfway"])
	}
	if got["relayed"] != "elsewhere" {
		t.Errorf("an id the tool set itself must be kept: %q", got["relayed"])
	}
}

func TestVerifierReportsTheWholeRunAcrossAttempts(t *testing.T) {
	calls := 0
	inner := runFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
		return func(yield func(Event, error) bool) {
			calls++
			yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonCompleted, Turns: 2, Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}}}, nil)
		}
	})
	v := VerifierRunner{Runner: inner, Checks: []Verifier{failing(1)}, Attempts: 3}
	_, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "go"))
	if calls != 2 || result.Reason != ReasonCompleted {
		t.Fatalf("want a pass on the second attempt, got %d calls and %+v", calls, result)
	}
	if result.Turns != 4 || result.Usage.InputTokens != 20 || result.Usage.OutputTokens != 4 {
		t.Errorf("turns and usage must cover every attempt: turns=%d usage=%+v", result.Turns, result.Usage)
	}
}

func TestVerifierCountsEarlierAttemptsWhenALaterOneFails(t *testing.T) {
	calls := 0
	inner := runFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
		return func(yield func(Event, error) bool) {
			calls++
			r := &Result{Reason: ReasonCompleted, Turns: 2, Usage: llm.Usage{InputTokens: 10}}
			if calls == 2 {
				r = &Result{Reason: ReasonMaxTurns, Err: "turn limit", Turns: 3, Usage: llm.Usage{InputTokens: 7}}
			}
			yield(Event{Kind: EventResult, Result: r}, nil)
		}
	})
	v := VerifierRunner{Runner: inner, Checks: []Verifier{failing(99)}, Attempts: 3}
	_, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "go"))
	if result.Reason != ReasonMaxTurns || result.Err != "turn limit" || !result.Complete {
		t.Fatalf("the failed attempt decides the outcome: %+v", result)
	}
	if result.Turns != 5 || result.Usage.InputTokens != 17 {
		t.Errorf("the first attempt's work must still be counted: turns=%d usage=%+v", result.Turns, result.Usage)
	}
}
