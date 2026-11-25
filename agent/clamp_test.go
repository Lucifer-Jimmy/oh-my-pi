package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type fatTool struct {
	body  string
	limit int
}

func (f *fatTool) Name() string            { return "fat" }
func (f *fatTool) Description() string     { return "returns a large body" }
func (f *fatTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fatTool) IsReadOnly() bool        { return true }
func (f *fatTool) IsConcurrencySafe(json.RawMessage) bool {
	return false
}
func (f *fatTool) Metadata(json.RawMessage) Metadata {
	return Metadata{MaxResultSize: f.limit}
}
func (f *fatTool) Execute(ctx context.Context, tc ToolContext, raw json.RawMessage) ToolResult {
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(f.body)}}
}

type silentTool struct{ body string }

func (s *silentTool) Name() string            { return "silent" }
func (s *silentTool) Description() string     { return "declares nothing" }
func (s *silentTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *silentTool) Execute(ctx context.Context, tc ToolContext, raw json.RawMessage) ToolResult {
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(s.body)}}
}

func runOne(t *testing.T, tool Tool) agentResult {
	t.Helper()
	reg := NewRegistry()
	reg.Register(tool)
	model := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", tool.Name(), `{}`, llm.StopReasonToolUse),
		textTurn("done"),
	}}
	loop := NewLoop(reg, RunDeps{CallModel: model.call}, nil, Options{})
	var out agentResult
	for ev, err := range loop.Run(context.Background(), session.NewMemory(nil), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == EventToolResult {
			out.text = ""
			for _, b := range ev.ToolResult.Content {
				out.text += b.Text
			}
			out.isErr = ev.ToolResult.IsError
		}
	}
	return out
}

type agentResult struct {
	text  string
	isErr bool
}

func TestADeclaredResultLimitIsEnforcedCentrally(t *testing.T) {
	body := strings.Repeat("x", 10_000)
	got := runOne(t, &fatTool{body: body, limit: 1000})
	if len(got.text) > 1200 {
		t.Fatalf("a declared limit must be enforced, got %d bytes", len(got.text))
	}
	if !strings.Contains(got.text, "truncated at 1000 bytes") {
		t.Fatalf("the cut must be explained:\n%s", got.text[len(got.text)-200:])
	}
	if got.isErr {
		t.Fatal("truncation is not an error")
	}
}

func TestAnUndeclaredToolGetsTheConservativeLimit(t *testing.T) {
	body := strings.Repeat("y", defaultMaxResultSize+5000)
	got := runOne(t, &silentTool{body: body})
	if len(got.text) > defaultMaxResultSize+300 {
		t.Fatalf("an undeclared tool must fall back to %d, got %d", defaultMaxResultSize, len(got.text))
	}
}

func TestASmallResultIsUntouched(t *testing.T) {
	got := runOne(t, &fatTool{body: "short", limit: 1000})
	if got.text != "short" {
		t.Fatalf("a result under the limit must be untouched, got %q", got.text)
	}
}

func TestTokenBudgetStopsAtTheTurnBoundary(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&fatTool{body: "small", limit: 1000})
	spend := func(id string) []llm.StreamEvent {
		turn := toolTurn(id, "fat", `{}`, llm.StopReasonToolUse)
		turn[0].Usage = llm.Usage{InputTokens: 100, OutputTokens: 20}
		return turn
	}
	model := &scriptedModel{turns: [][]llm.StreamEvent{
		spend("t1"), spend("t2"), spend("t3"), textTurn("never"),
	}}
	loop := NewLoop(reg, RunDeps{CallModel: model.call}, nil, Options{TokenBudget: 100})
	var got *Result
	seen := 0
	for ev, err := range loop.Run(context.Background(), session.NewMemory(nil), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == EventToolResult {
			seen++
		}
		if ev.Kind == EventResult {
			got = ev.Result
		}
	}
	if got == nil || got.Reason != ReasonTokenBudget {
		t.Fatalf("the budget must stop the run, got %+v", got)
	}
	if seen != 1 {
		t.Fatalf("the turn in flight must finish, then the next must not start, saw %d results", seen)
	}
}
