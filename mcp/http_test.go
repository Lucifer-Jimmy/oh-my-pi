package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func serveHTTP(t *testing.T, sse bool) *httptest.Server {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: "remote", Version: "1"}, nil)
	srv.AddTool(&sdk.Tool{
		Name:        "status",
		Description: "reports a status",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
	}, func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "all good"}}}, nil
	})
	var h http.Handler
	if sse {
		h = sdk.NewSSEHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	} else {
		h = sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func TestConnectOverStreamableHTTP(t *testing.T) {
	ts := serveHTTP(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Connect(ctx, "remote", ServerConfig{URL: ts.URL})
	if err != nil {
		t.Fatalf("streamable http connect: %v", err)
	}
	defer s.Close()
	tools := s.Tools()
	if len(tools) != 1 || tools[0].Name() != "mcp__remote_status" {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestConnectOverSSE(t *testing.T) {
	ts := serveHTTP(t, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Connect(ctx, "remote", ServerConfig{URL: ts.URL, SSE: true})
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer s.Close()
	if len(s.Tools()) != 1 {
		t.Fatalf("tools = %d", len(s.Tools()))
	}
}

func TestConnectRejectsAmbiguousOrMissingEndpoints(t *testing.T) {
	if _, err := Connect(context.Background(), "x", ServerConfig{URL: "http://h", Command: "srv"}); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("a url plus a command must be refused: %v", err)
	}
	if _, err := Connect(context.Background(), "x", ServerConfig{}); err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("no endpoint must be refused: %v", err)
	}
}
