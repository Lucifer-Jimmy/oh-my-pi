package main

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func extRunner(loop *agent.Loop) agent.Runner {
	return agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return loop.Run(ctx, sess, input)
	})
}

func TestExternalToolCallsTheHostAndReturnsItsAnswer(t *testing.T) {
	reg := agent.NewRegistry()
	reg.Register(&agent.RemoteTool{
		ToolName:  "deploy_status",
		ToolDesc:  "asks the host for a deployment status",
		InputJSON: json.RawMessage(`{"type":"object","properties":{"app":{"type":"string"}}}`),
		ReadOnly:  true,
		OpenWorld: true,
	})
	model := &stepModel{steps: []step{
		{id: "x1", tool: "deploy_status", input: map[string]any{"app": "api"}},
		{text: "the host answered"},
	}}
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: model.call}, nil, agent.Options{})
	host := newRPCHost()
	loop.SetHost(host)
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- runRPC(context.Background(), host, extRunner(loop), session.NewMemory(nil), nil, inR, out, io.Discard)
	}()
	send := func(v any) {
		b, _ := json.Marshal(v)
		inW.Write(append(b, '\n'))
	}
	send(map[string]any{"type": "prompt", "text": "ask the host"})

	var callID, callName string
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && callID == "" {
		for _, line := range strings.Split(out.String(), "\n") {
			var f wireFrame
			if json.Unmarshal([]byte(line), &f) == nil && f.Type == "tool_call" {
				callID, callName = f.ID, f.Call.Name
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if callID == "" {
		t.Fatalf("the external call must reach the client:\n%s", out.String())
	}
	if callName != "deploy_status" {
		t.Fatalf("the frame must name the tool, got %q", callName)
	}
	send(map[string]any{"type": "tool_result", "id": callID, "text": "api is green"})
	send(map[string]any{"type": "quit"})
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the rpc loop did not exit")
	}
	if !strings.Contains(out.String(), "api is green") {
		t.Fatalf("the host's answer must reach the model:\n%s", out.String())
	}
}

func TestRemoteToolWithoutAHostIsAnError(t *testing.T) {
	rt := &agent.RemoteTool{ToolName: "x", InputJSON: json.RawMessage(`{}`)}
	res := rt.Execute(context.Background(), &noExtCtx{}, nil)
	if !res.IsError || !strings.Contains(res.Content[0].Text, "host") {
		t.Fatalf("a remote tool with no host must say so: %+v", res)
	}
}

type noExtCtx struct{}

func (noExtCtx) CWD() string          { return "" }
func (noExtCtx) Workspaces() []string { return nil }
func (noExtCtx) Emit(agent.Event)     {}
func (noExtCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, agent.ErrNoUI
}
func (noExtCtx) RecentMessages() []llm.Message { return nil }

func TestExternalToolManifestIsLoadedAndValidated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tools.json")
	body := `[{"name":"deploy_status","description":"asks the host","schema":{"type":"object"},"read_only":true}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	tools, err := loadExternalTools(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name() != "deploy_status" {
		t.Fatalf("manifest = %+v", tools)
	}
	if got := tools[0].(agent.ApprovalDeclarer).Approval(nil).Tier; got != agent.TierRead {
		t.Fatalf("a read-only declaration must be read tier, got %q", got)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`[{"description":"no name"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadExternalTools(bad); err == nil || !strings.Contains(err.Error(), "no name") {
		t.Fatalf("an unnamed tool must be rejected: %v", err)
	}
	if _, err := loadExternalTools(""); err != nil {
		t.Fatalf("no manifest is not an error: %v", err)
	}
}
