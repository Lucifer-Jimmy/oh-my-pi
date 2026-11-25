package main

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/compaction"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type readerStub struct {
	body string
}

func (readerStub) Name() string            { return "read" }
func (readerStub) Description() string     { return "reads a file" }
func (readerStub) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (r readerStub) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(r.body)}}
}

func TestTwoTimesTheWindowCompletesAcrossTwoCompactions(t *testing.T) {
	const window = 4000
	body := strings.Repeat("row\n", 800)
	model := &growingModel{window: window}
	compactor := compaction.New(compaction.Config{
		Window:           func() int { return window },
		KeepRecentTokens: 400,
		ReserveTokens:    300,
		MinElideTokens:   50,
	}, func(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.Usage, error) {
		return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("## Goal\nkept")}},
			llm.Usage{InputTokens: 10, OutputTokens: 5}, nil
	})
	reg := agent.NewRegistry()
	reg.Register(readerStub{body: body})
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: model.call, Compactor: compactor}, nil, agent.Options{
		CWD:      t.TempDir(),
		MaxTurns: 400,
	})
	sess := session.NewMemory(nil)

	compactions := 0
	var done *agent.Result
	for ev, err := range loop.Run(context.Background(), sess, "read everything") {
		if err != nil {
			t.Fatal(err)
		}
		switch ev.Kind {
		case agent.EventCompaction:
			compactions++
			if ev.Compaction != nil && ev.Compaction.TokensAfter >= ev.Compaction.TokensBefore {
				t.Fatalf("a compaction must shrink the projection: %+v", ev.Compaction)
			}
		case agent.EventResult:
			done = ev.Result
		}
	}
	if done == nil || done.Reason != agent.ReasonCompleted {
		t.Fatalf("the run must finish: %+v", done)
	}
	if compactions < 2 {
		t.Fatalf("the task must cross the threshold twice, got %d compaction(s) over %d turns", compactions, done.Turns)
	}
	if bodyTokens := 40 * 800 / 4; bodyTokens < 2*window {
		t.Fatalf("the scripted task only totals %d tokens, which is not twice the %d-token window", bodyTokens, window)
	}
	if model.peak <= 2*window/3 {
		t.Fatalf("the model must have been sent a context larger than the window, peak %d", model.peak)
	}
}

type growingModel struct {
	window int
	turn   int
	peak   int
}

func (m *growingModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	size := 0
	for _, msg := range req.Messages {
		for _, b := range msg.Content {
			size += len(b.Text)
			for _, c := range b.Content {
				size += len(c.Text)
			}
		}
	}
	if size > m.peak {
		m.peak = size
	}
	n := m.turn
	m.turn++
	return func(yield func(llm.StreamEvent, error) bool) {
		var evs []llm.StreamEvent
		evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStart})
		if n < 40 {
			raw, _ := json.Marshal(map[string]string{"path": fmt.Sprintf("file-%d.txt", n)})
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: fmt.Sprintf("t%d", n), Name: "read", Input: raw}},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
			)
		} else {
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
				llm.StreamEvent{Type: llm.SETextDelta, Text: "finished"},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
			)
		}
		evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStop})
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}
