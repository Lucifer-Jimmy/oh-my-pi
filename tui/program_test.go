package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

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

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestProgramEndToEnd(t *testing.T) {
	host := NewHost()
	answered := make(chan agent.AskResponse, 1)
	runner := &scriptRunner{script: func(ctx context.Context, _ string, emit func(agent.Event)) {
		emit(text("Hello from the model\n"))
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "bash", ToolInput: json.RawMessage(`{"command":"go test ./..."}`)})
		r, _ := host.Ask(ctx, agent.AskRequest{Method: agent.AskConfirm, Title: "Approve bash", Message: "exec tier requires approval"})
		answered <- r
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("PASS")}}})
		emit(text("All done."))
		emit(done(agent.ReasonCompleted))
	}}

	in, keys := io.Pipe()
	defer keys.Close()
	out := &syncBuffer{}
	m := newModel(context.Background(), Options{Runner: runner, Host: host, Model: "gw/m1", Policy: permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{}), CWD: "/work"})
	p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithWindowSize(80, 24), tea.WithoutSignals())
	exited := make(chan error, 1)
	go func() {
		_, err := p.Run()
		exited <- err
	}()
	screen := func() string { return ansi.Strip(out.String()) }
	typeKeys := func(s string) {
		go keys.Write([]byte(s))
	}

	waitFor(t, "banner", func() bool { return strings.Contains(screen(), "Enter to send") })
	typeKeys("hi\r")
	waitFor(t, "approval dock", func() bool { return strings.Contains(screen(), "Approve bash") })
	typeKeys("y")
	select {
	case r := <-answered:
		if !r.Confirmed {
			t.Fatalf("y must approve, got %+v", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("approval never reached the tool")
	}
	waitFor(t, "final text", func() bool { return strings.Contains(screen(), "All done.") })
	typeKeys("/exit\r")

	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("program error: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("program did not exit")
	}
	assertOrder(t, screen(), "› hi", "● Hello from the model", "Bash(go test ./...)", "⎿ PASS", "● All done.")
}
