package main

import (
	"context"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const killEnv = "OMP_KILL_HELPER"

func runKillHelper() {
	dir := os.Getenv(killEnv)
	store, err := session.Create(dir, session.NewID(time.Now()), dir, "test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	store.Append(llm.UserText("run the slow command"))
	store.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		{Type: llm.BlockToolUse, ID: "slow-1", Name: "bash", Input: []byte(`{"command":"sleep 600"}`)},
	}})
	if err := store.Close(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(store.Path())
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
}

func startAndKill(t *testing.T, dir string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), killEnv+"="+dir)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		n, _ := out.Read(buf)
		line <- strings.TrimSpace(string(buf[:n]))
	}()
	select {
	case path := <-line:
		if err := cmd.Process.Kill(); err != nil {
			t.Fatalf("kill: %v", err)
		}
		cmd.Wait()
		return path
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal("the helper never reported its session path")
		return ""
	}
}

type captureModel struct {
	first *llm.CompletionRequest
}

func (c *captureModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	if c.first == nil {
		cp := req
		cp.Messages = append([]llm.Message(nil), req.Messages...)
		c.first = &cp
	}
	return (&stepModel{steps: []step{{text: "carried on"}}}).call(ctx, req)
}

func TestKilledMidToolCallResumesWithAnOutcomeUnknownResult(t *testing.T) {
	dir := t.TempDir()
	path := startAndKill(t, dir)

	store, report, err := session.Load(path)
	if err != nil {
		t.Fatalf("the killed session must load: %v", err)
	}
	defer store.Close()
	if !report.Repaired || report.Synthetic == 0 {
		t.Fatalf("the dangling call must be closed with a synthetic result: %+v", report)
	}
	msgs := store.Messages()
	if len(msgs) < 3 {
		t.Fatalf("expected the user turn, the call and the synthetic result, got %d messages", len(msgs))
	}
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser || len(last.Content) != 1 || last.Content[0].Type != llm.BlockToolResult {
		t.Fatalf("the repair must append a tool_result, got %+v", last)
	}
	if last.Content[0].ToolUseID != "slow-1" || !strings.Contains(last.Content[0].Content[0].Text, "outcome unknown") {
		t.Fatalf("the synthetic result must name the call and say what happened: %+v", last.Content[0])
	}

	model := &captureModel{}
	loop := agent.NewLoop(agent.NewRegistry(), agent.RunDeps{CallModel: model.call}, nil, agent.Options{})
	for ev, err := range loop.Run(context.Background(), store, "carry on") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == agent.EventResult && ev.Result.Reason != agent.ReasonCompleted {
			t.Fatalf("the continued run must finish: %+v", ev.Result)
		}
	}
	if model.first == nil {
		t.Fatal("the resumed session must reach the model")
	}
	if !sawToolResultFor(model.first.Messages, "slow-1") {
		t.Fatal("the request must pair every tool call with a result")
	}
}

func TestContinuePicksTheRepairedSessionAndSaysSo(t *testing.T) {
	root := t.TempDir()
	dir := session.ProjectDir(root, root)
	startAndKill(t, dir)
	cfg := config{cont: true, sessionsDir: root}
	conv, err := openConversation(cfg, root)
	if err != nil {
		t.Fatalf("--continue must find the killed session: %v", err)
	}
	defer conv.Close()
	if !strings.Contains(conv.note, "repaired") {
		t.Fatalf("the shell must tell the user the session was repaired, note = %q", conv.note)
	}
	if !sawToolResult(conv.sess.Messages(), "slow-1") {
		t.Fatal("the loaded session must carry the synthetic result")
	}
}

func sawToolResult(msgs []llm.Message, id string) bool {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolResult && b.ToolUseID == id && !b.IsError {
				return true
			}
		}
	}
	return sawToolResultFor(msgs, id)
}

func sawToolResultFor(msgs []llm.Message, id string) bool {
	for _, m := range msgs {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolResult && b.ToolUseID == id {
				return true
			}
		}
	}
	return false
}
