package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type fakeCtx struct{}

func (fakeCtx) CWD() string          { return "" }
func (fakeCtx) Workspaces() []string { return nil }
func (fakeCtx) Emit(agent.Event)     {}
func (fakeCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, agent.ErrNoUI
}
func (fakeCtx) RecentMessages() []llm.Message { return nil }

func startServer(t *testing.T, name string, handler sdk.ToolHandler) *Server {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: name, Version: "1"}, nil)
	srv.AddTool(&sdk.Tool{
		Name:        "screenshot",
		Description: "take a screenshot",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
			"required":   []string{"url"},
		},
	}, handler)
	clientTransport, serverTransport := sdk.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = srv.Run(ctx, serverTransport) }()

	s, err := connectTransport(ctx, name, clientTransport)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBridgeListsAndCallsATool(t *testing.T) {
	var gotArgs map[string]any
	s := startServer(t, "browser", func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		_ = json.Unmarshal(req.Params.Arguments, &gotArgs)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "shot taken"}}}, nil
	})
	tools := s.Tools()
	if len(tools) != 1 {
		t.Fatalf("tools = %d", len(tools))
	}
	tool := tools[0]
	if tool.Name() != "mcp__browser_screenshot" {
		t.Errorf("name = %q", tool.Name())
	}
	if tool.Description() != "take a screenshot" {
		t.Errorf("description = %q", tool.Description())
	}
	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil || schema["type"] != "object" {
		t.Errorf("schema = %s (%v)", tool.Schema(), err)
	}

	res := tool.Execute(context.Background(), fakeCtx{}, json.RawMessage(`{"url":"https://x"}`))
	text := ""
	for _, b := range res.Content {
		text += b.Text
	}
	if res.IsError || text != "shot taken" {
		t.Fatalf("got isErr=%v text=%q", res.IsError, text)
	}
	if gotArgs["url"] != "https://x" {
		t.Errorf("arguments did not reach the server: %v", gotArgs)
	}
}

func TestBridgePropagatesToolErrors(t *testing.T) {
	s := startServer(t, "s", func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{
			IsError: true,
			Content: []sdk.Content{&sdk.TextContent{Text: "no such page"}},
		}, nil
	})
	res := s.Tools()[0].Execute(context.Background(), fakeCtx{}, json.RawMessage(`{"url":"x"}`))
	if !res.IsError {
		t.Fatalf("a tool error must stay an error")
	}
	if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "no such page") {
		t.Errorf("the server's message must reach the model: %+v", res.Content)
	}
}

func TestBridgeRejectsNonObjectArguments(t *testing.T) {
	s := startServer(t, "s", func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ran"}}}, nil
	})
	res := s.Tools()[0].Execute(context.Background(), fakeCtx{}, json.RawMessage(`[1,2]`))
	if !res.IsError || !strings.Contains(res.Content[0].Text, "JSON object") {
		t.Fatalf("got %+v", res)
	}
}

func TestToolDefaultsToWriteAndOpenWorld(t *testing.T) {
	s := startServer(t, "s", func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{}, nil
	})
	tool, ok := s.Tools()[0].(*tool)
	if !ok {
		t.Fatalf("an MCP tool must be the concrete bridge type")
	}
	if tool.Approval(nil).Tier != agent.TierWrite {
		t.Errorf("an MCP tool must default to the write tier")
	}
	if !tool.Metadata(nil).OpenWorld {
		t.Errorf("an MCP tool reaches the outside world and must say so")
	}
	if _, declared := any(tool).(agent.Schedulable); declared {
		t.Errorf("an MCP tool must not declare scheduling safety it cannot know")
	}
}

func TestToolNameSanitisesAndCaps(t *testing.T) {
	cases := []struct{ server, remote, want string }{
		{"browser", "screenshot", "mcp__browser_screenshot"},
		{"My Server!", "Take-Shot", "mcp__my_server_take_shot"},
		{"", "", "mcp__tool_tool"},
	}
	for _, c := range cases {
		if got := ToolName(c.server, c.remote); got != c.want {
			t.Errorf("ToolName(%q,%q) = %q, want %q", c.server, c.remote, got, c.want)
		}
	}
	long := ToolName(strings.Repeat("server", 5), strings.Repeat("tool", 30))
	if len(long) > maxNameLen {
		t.Errorf("name longer than %d: %d %q", maxNameLen, len(long), long)
	}
	if !strings.HasPrefix(long, "mcp__") {
		t.Errorf("prefix must survive truncation: %q", long)
	}
}

func TestConnectWithoutCommandFailsClearly(t *testing.T) {
	if _, err := Connect(context.Background(), "x", ServerConfig{}); err == nil || !strings.Contains(err.Error(), "command or url is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestConnectToAMissingBinaryFailsWithTheServerName(t *testing.T) {
	_, err := Connect(context.Background(), "ghost", ServerConfig{Command: "definitely-not-a-real-binary-xyz"})
	if err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v", err)
	}
}
