package agent

import (
	"context"
	"iter"
	"strings"
	"sync"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type steerModel struct {
	mu    sync.Mutex
	calls [][]llm.Message
}

func (m *steerModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		m.mu.Lock()
		m.calls = append(m.calls, req.Messages)
		n := len(m.calls)
		m.mu.Unlock()
		evs := []llm.StreamEvent{{Type: llm.SEMessageStart}}
		evs = append(evs,
			llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
			llm.StreamEvent{Type: llm.SETextDelta, Text: "reply " + string(rune('0'+n))},
			llm.StreamEvent{Type: llm.SEBlockStop},
			llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
			llm.StreamEvent{Type: llm.SEMessageStop},
		)
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestSteeringExtendsTheTurnInsteadOfCompleting(t *testing.T) {
	model := &steerModel{}
	var mu sync.Mutex
	asks := 0
	steer := func() []string {
		mu.Lock()
		defer mu.Unlock()
		asks++
		if asks == 1 {
			return nil
		}
		if asks == 2 {
			return []string{"also check the tests"}
		}
		return nil
	}
	loop := NewLoop(NewRegistry(), RunDeps{CallModel: model.call, Steer: steer}, nil, Options{})
	sess := session.NewMemory(nil)

	var notes []string
	var result *Result
	for ev, err := range loop.Run(context.Background(), sess, "start") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == EventNotification {
			notes = append(notes, ev.Message)
		}
		if ev.Kind == EventResult {
			result = ev.Result
		}
	}
	if len(model.calls) != 2 {
		t.Fatalf("a steering message must cause another model call, got %d", len(model.calls))
	}
	second := model.calls[1]
	found := false
	for _, m := range second {
		if m.Role == llm.RoleUser && strings.Contains(strings.Join(texts(m), " "), "also check the tests") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the steering message must reach the second request: %+v", second)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "also check the tests") {
		t.Errorf("the shell must be told steering happened: %v", notes)
	}
	if result == nil || result.Reason != ReasonCompleted {
		t.Fatalf("the run still ends completed: %+v", result)
	}
}

func texts(m llm.Message) []string {
	var out []string
	for _, b := range m.Content {
		out = append(out, b.Text)
	}
	return out
}

func TestNoSteerFnIsUnchanged(t *testing.T) {
	model := &steerModel{}
	loop := NewLoop(NewRegistry(), RunDeps{CallModel: model.call}, nil, Options{})
	for range loop.Run(context.Background(), session.NewMemory(nil), "hi") {
	}
	if len(model.calls) != 1 {
		t.Fatalf("without a Steer function the loop runs exactly one call, got %d", len(model.calls))
	}
}
