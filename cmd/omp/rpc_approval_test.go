package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type writeOnce struct {
	path string
}

func (w *writeOnce) Name() string            { return "slow_write" }
func (w *writeOnce) Description() string     { return "writes one file" }
func (w *writeOnce) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (w *writeOnce) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	if err := os.WriteFile(w.path, []byte("written"), 0o644); err != nil {
		return agent.ToolResult{IsError: true}
	}
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("wrote it")}}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestRPCApprovalRoundTripReleasesTheTool(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "made.txt")
	reg := agent.NewRegistry()
	reg.Register(permission.Wrap(&writeOnce{path: target}, permission.NewPolicy(permission.ModeDefault, permission.Rules{})))

	model := &stepModel{steps: []step{
		{id: "w1", tool: "slow_write", input: map[string]any{}},
		{text: "done"},
	}}
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: model.call}, nil, agent.Options{})
	run := agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return loop.Run(ctx, sess, input)
	})

	host := newRPCHost()
	loop.SetHost(host)
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- runRPC(context.Background(), host, run, session.NewMemory(nil), nil, inR, out, io.Discard)
	}()
	sendCmd := func(v any) {
		b, _ := json.Marshal(v)
		inW.Write(append(b, '\n'))
	}
	sendCmd(map[string]any{"type": "prompt", "text": "write the file"})

	var askID string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && askID == "" {
		for _, line := range strings.Split(out.String(), "\n") {
			var f wireFrame
			if json.Unmarshal([]byte(line), &f) == nil && f.Type == "ask" && f.ID != "" {
				askID = f.ID
			}
		}
		if askID == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if askID == "" {
		t.Fatalf("the approval must reach the client:\n%s", out.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the tool must not run before the answer")
	}
	sendCmd(map[string]any{"type": "answer", "id": askID, "value": "Yes"})
	sendCmd(map[string]any{"type": "quit"})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the rpc loop did not exit")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("an approved tool must run: %v\n%s", err, out.String())
	}
}
