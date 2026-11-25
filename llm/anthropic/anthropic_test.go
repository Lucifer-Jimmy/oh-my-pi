package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func TestNewBaseURLWithV1(t *testing.T) {
	cases := []struct{ suffix, path string }{
		{"", "/v1/messages"},
		{"/", "/v1/messages"},
		{"/v1", "/v1/messages"},
		{"/v1/", "/v1/messages"},
		{"/api", "/api/v1/messages"},
		{"/api/v1", "/api/v1/messages"},
	}
	for _, c := range cases {
		var path string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path = r.URL.Path
			w.Header().Set("Content-Type", "text/event-stream")
		}))
		for range New(Config{BaseURL: srv.URL + c.suffix, Model: "m"}).Stream(context.Background(), streamReq()) {
		}
		srv.Close()
		if path != c.path {
			t.Errorf("baseUrl suffix %q: request path %q, want %q", c.suffix, path, c.path)
		}
	}
}

func TestBuildParams(t *testing.T) {
	p := New(Config{Model: "claude-opus-5-5", APIKey: "x"})
	params, err := p.buildParams(llm.CompletionRequest{
		System:    []string{"you are a bot"},
		Messages:  []llm.Message{llm.UserText("hi")},
		MaxTokens: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if params.Model != "claude-opus-5-5" {
		t.Errorf("model = %q", params.Model)
	}
	if params.MaxTokens != 1024 {
		t.Errorf("max_tokens = %d", params.MaxTokens)
	}
	if len(params.Messages) != 1 || len(params.System) != 1 {
		t.Errorf("messages/system 装配不符: %d / %d", len(params.Messages), len(params.System))
	}
}

func TestBuildParamsDefaultMaxTokens(t *testing.T) {
	p := New(Config{Model: "claude-opus-5-5", APIKey: "x"})
	params, err := p.buildParams(llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}})
	if err != nil {
		t.Fatal(err)
	}
	if params.MaxTokens != unknownModelMaxTokens {
		t.Errorf("max_tokens = %d, want %d", params.MaxTokens, unknownModelMaxTokens)
	}
}

func TestBuildParamsThinking(t *testing.T) {
	cases := []struct {
		level llm.ThinkingLevel
		want  string
	}{
		{llm.ThinkingOff, ""},
		{"", ""},
		{llm.ThinkingMinimal, "low"},
		{llm.ThinkingLow, "low"},
		{llm.ThinkingMedium, "medium"},
		{llm.ThinkingHigh, "high"},
		{llm.ThinkingXHigh, "xhigh"},
		{llm.ThinkingMax, "max"},
	}
	p := New(Config{Model: "m"})
	for _, c := range cases {
		params, err := p.buildParams(llm.CompletionRequest{
			Messages:  []llm.Message{llm.UserText("hi")},
			MaxTokens: 100,
			Thinking:  c.level,
		})
		if err != nil {
			t.Fatalf("%q: %v", c.level, err)
		}
		if got := string(params.OutputConfig.Effort); got != c.want {
			t.Errorf("thinking %q --> effort %q，应为 %q", c.level, got, c.want)
		}
	}
}

// 有 system 挂 system 和 tail 两处，无 system 只挂 tail 一处
func TestBuildParamsCacheBreakpoints(t *testing.T) {
	p := New(Config{Model: "m"})
	count := func(req llm.CompletionRequest) int {
		params, err := p.buildParams(req)
		if err != nil {
			t.Fatal(err)
		}
		wire, _ := json.Marshal(params)
		return strings.Count(string(wire), `"cache_control"`)
	}
	if n := count(llm.CompletionRequest{
		System: []string{"a", "b"}, CacheBoundary: 1,
		Messages: []llm.Message{llm.UserText("hi")}, MaxTokens: 100,
	}); n != 2 {
		t.Errorf("system+tail 应 2 处断点，得 %d", n)
	}
	if n := count(llm.CompletionRequest{
		Messages: []llm.Message{llm.UserText("hi")}, MaxTokens: 100,
	}); n != 1 {
		t.Errorf("仅 tail 应 1 处断点，得 %d", n)
	}
}

// 测试 convertSystem 越界 boundary / convertMessages 不变式破坏
func TestBuildParamsErrors(t *testing.T) {
	p := New(Config{Model: "m"})
	if _, err := p.buildParams(llm.CompletionRequest{
		System: []string{"a"}, CacheBoundary: 2, // system 仅 1 块，越界
		Messages: []llm.Message{llm.UserText("hi")}, MaxTokens: 100,
	}); err == nil {
		t.Error("越界 CacheBoundary 应报错")
	}
	bad := llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{
		{Type: llm.BlockToolUse, ID: "t1", Name: "x", Input: json.RawMessage(`{}`)},
	}}
	if _, err := p.buildParams(llm.CompletionRequest{
		Messages: []llm.Message{bad}, MaxTokens: 100,
	}); err == nil {
		t.Error("user 轮含 tool_use 应报错")
	}
}

func TestBuildParamsTools(t *testing.T) {
	p := New(Config{Model: "m", APIKey: "x"})
	params, err := p.buildParams(llm.CompletionRequest{
		Messages: []llm.Message{llm.UserText("hi")},
		Tools: []llm.ToolSchema{
			{Name: "read", Description: "读文件", InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)},
			{Name: "noop", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"input_schema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Tools) != 2 {
		t.Fatalf("tools must reach the wire, got %s", b)
	}
	read := wire.Tools[0]
	if read.Name != "read" || read.Description != "读文件" {
		t.Errorf("name/description wrong: %+v", read)
	}
	s := read.InputSchema
	if s["type"] != "object" || s["properties"] == nil || s["additionalProperties"] != false {
		t.Errorf("input_schema must keep type, properties and extra keywords: %v", s)
	}
	if req, _ := s["required"].([]any); len(req) != 1 || req[0] != "path" {
		t.Errorf("required lost: %v", s["required"])
	}
	if wire.Tools[1].InputSchema["type"] != "object" {
		t.Errorf("a bare schema still declares type object: %v", wire.Tools[1].InputSchema)
	}

	if _, err := p.buildParams(llm.CompletionRequest{Tools: []llm.ToolSchema{{Name: "bad", InputSchema: json.RawMessage(`{`)}}}); err == nil {
		t.Error("an unparseable schema must fail loudly")
	}
}
