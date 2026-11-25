package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type lookupTool struct {
	rows []string
}

func (l *lookupTool) Name() string        { return "lookup" }
func (l *lookupTool) Description() string { return "looks a row up by key" }
func (l *lookupTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}}}`)
}
func (l *lookupTool) IsReadOnly() bool { return true }
func (l *lookupTool) IsConcurrencySafe(json.RawMessage) bool {
	return false
}
func (l *lookupTool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a struct {
		Key string `json:"key"`
	}
	json.Unmarshal(raw, &a)
	for _, r := range l.rows {
		if strings.HasPrefix(r, a.Key+":") {
			return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(r)}}
		}
	}
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("no row for " + a.Key)}, IsError: true}
}

type yesHost struct{ asked int }

func (h *yesHost) Ask(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	h.asked++
	if len(req.Options) > 0 {
		return agent.AskResponse{Value: req.Options[0]}, nil
	}
	return agent.AskResponse{Confirmed: true}, nil
}

type countingCompactor struct{ calls int }

func (c *countingCompactor) Compact(ctx context.Context, req agent.CompactRequest) ([]llm.Message, *session.Compaction, error) {
	c.calls++
	return req.Request.Messages, nil, nil
}

func TestEmbeddingExampleUsesOnlyThePublicPackages(t *testing.T) {
	reg := agent.NewRegistry()
	reg.Register(&lookupTool{rows: []string{"a: row one", "b: row two"}})
	model := &stepModel{steps: []step{
		{id: "l1", tool: "lookup", input: map[string]any{"key": "a"}},
		{text: "a is row one"},
	}}
	host := &yesHost{}
	comp := &countingCompactor{}
	deps := agent.RunDeps{CallModel: model.call, Compactor: comp}
	loop := agent.NewLoop(reg, deps, host, agent.Options{CWD: t.TempDir(), MaxTurns: 4})
	sess := session.NewMemory(nil)

	var textOut string
	var toolResult string
	var events int
	for ev, err := range loop.Run(context.Background(), sess, "what is a?") {
		if err != nil {
			t.Fatal(err)
		}
		events++
		switch ev.Kind {
		case agent.EventToolResult:
			for _, b := range ev.ToolResult.Content {
				toolResult += b.Text
			}
		case agent.EventResult:
			textOut = ev.Result.Text
			if ev.Result.Reason != agent.ReasonCompleted {
				t.Fatalf("reason %s", ev.Result.Reason)
			}
		}
	}
	if events == 0 || !strings.Contains(toolResult, "row one") {
		t.Fatalf("the custom tool must run through the loop: %q", toolResult)
	}
	if textOut != "a is row one" {
		t.Fatalf("answer = %q", textOut)
	}
	if comp.calls == 0 {
		t.Fatal("the custom compactor must be asked before each request")
	}
	if got := sess.Messages(); len(got) < 3 {
		t.Fatalf("the session must record the exchange, got %d messages", len(got))
	}
}
