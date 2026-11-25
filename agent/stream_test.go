package agent

import (
	"context"
	"encoding/json"
	"iter"
	"sync"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func signalTool(name string, started chan<- struct{}, release <-chan struct{}, mu *sync.Mutex, log *[]string) funcTool {
	return funcTool{name: name, exec: func() ToolResult {
		mu.Lock()
		*log = append(*log, "start "+name)
		mu.Unlock()
		if started != nil {
			started <- struct{}{}
		}
		if release != nil {
			<-release
		}
		mu.Lock()
		*log = append(*log, "end "+name)
		mu.Unlock()
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(name + " ok")}}
	}}
}

func TestToolStartsBeforeTheStreamEnds(t *testing.T) {
	started := make(chan struct{}, 1)
	gate := make(chan struct{})
	var mu sync.Mutex
	var log []string
	reg := NewRegistry()
	reg.Register(signalTool("scan", started, gate, &mu, &log))

	turn := []llm.StreamEvent{
		{Type: llm.SEMessageStart},
		{Type: llm.SEBlockStart, BlockIndex: 0, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: "t1", Name: "scan", Input: json.RawMessage(`{}`)}},
		{Type: llm.SEBlockStop, BlockIndex: 0},
	}
	m := &stagedModel{turns: [][]llm.StreamEvent{turn, textTurn("done")}}
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	done := make(chan []Event, 1)
	go func() {
		evs, _ := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
		done <- evs
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the tool did not start while the message was still streaming")
	}
	m.continueTurn()
	close(gate)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the run did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(log) != 2 || log[0] != "start scan" || log[1] != "end scan" {
		t.Errorf("tool log = %v", log)
	}
}

func TestExclusiveToolBlocksTheQueue(t *testing.T) {
	var mu sync.Mutex
	var log []string
	gate := make(chan struct{})
	reg := NewRegistry()
	slow := signalTool("write", nil, gate, &mu, &log)
	reg.Register(slow)
	reg.Register(signalTool("read", nil, nil, &mu, &log))

	m := &scriptedModel{turns: [][]llm.StreamEvent{
		multiToolTurn(toolUse("w1", "write"), toolUse("r1", "read")),
		textTurn("done"),
	}}
	l := NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{})
	go func() {
		for i := 0; i < 100; i++ {
			mu.Lock()
			startedRead := len(log) > 1
			mu.Unlock()
			if startedRead {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		close(gate)
	}()
	_, _ = collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))

	mu.Lock()
	defer mu.Unlock()
	want := []string{"start write", "end write", "start read", "end read"}
	if len(log) != len(want) {
		t.Fatalf("tool log = %v", log)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("tool log = %v, want %v", log, want)
		}
	}
}

// stagedModel 在一条消息中途停住，第二步由测试放行，用来观察消息还没收完时工具是否已经在跑
type stagedModel struct {
	mu      sync.Mutex
	turns   [][]llm.StreamEvent
	calls   int
	release chan struct{}
}

func (m *stagedModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	m.mu.Lock()
	turn := m.turns[m.calls]
	m.calls++
	first := m.calls == 1
	m.mu.Unlock()
	if first {
		m.release = make(chan struct{})
	}
	gate := m.release
	return func(yield func(llm.StreamEvent, error) bool) {
		for i, ev := range turn {
			if !yield(ev, nil) {
				return
			}
			if first && i == len(turn)-1 {
				<-gate
			}
		}
		if first {
			for _, ev := range []llm.StreamEvent{
				{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
				{Type: llm.SEMessageStop},
			} {
				if !yield(ev, nil) {
					return
				}
			}
		}
	}
}

func (m *stagedModel) continueTurn() {
	m.mu.Lock()
	gate := m.release
	m.mu.Unlock()
	if gate != nil {
		close(gate)
	}
}
