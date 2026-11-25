package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func marshalParams(t *testing.T, req llm.CompletionRequest) map[string]any {
	t.Helper()
	p := &Provider{model: "gpt-test"}
	params, err := p.buildParams(req)
	if err != nil {
		t.Fatalf("buildParams: %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

func messagesOf(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["messages"].([]any)
	if !ok {
		t.Fatalf("messages 不是数组: %T", m["messages"])
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func TestBuildParams_FourMessageTypes(t *testing.T) {
	req := llm.CompletionRequest{
		System: []string{"你是助手", "", "遵守规则"},
		Messages: []llm.Message{
			llm.UserText("读一下 a.go"),
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ThinkingBlock("先想想", "sig"),
				llm.TextBlock("好的"),
				{Type: llm.BlockToolUse, ID: "call_1", Name: "read", Input: json.RawMessage(`{"path":"a.go"}`)},
			}},
			{Role: llm.RoleUser, Content: []llm.ContentBlock{
				llm.ToolResultText("call_1", "package main", false),
			}},
		},
	}
	msgs := messagesOf(t, marshalParams(t, req))
	if len(msgs) != 4 {
		t.Fatalf("消息数 = %d, 期望 4: %v", len(msgs), msgs)
	}
	if msgs[0]["role"] != "system" {
		t.Errorf("[0].role = %v, 期望 system", msgs[0]["role"])
	}
	if got := msgs[0]["content"]; got != "你是助手\n\n遵守规则" {
		t.Errorf("[0].content = %q", got)
	}
	if msgs[1]["role"] != "user" || msgs[1]["content"] != "读一下 a.go" {
		t.Errorf("[1] = %v", msgs[1])
	}
	if msgs[2]["role"] != "assistant" || msgs[2]["content"] != "好的" {
		t.Errorf("[2] content = %v", msgs[2])
	}
	tcs, ok := msgs[2]["tool_calls"].([]any)
	if !ok || len(tcs) != 1 {
		t.Fatalf("[2].tool_calls = %v", msgs[2]["tool_calls"])
	}
	fn := tcs[0].(map[string]any)["function"].(map[string]any)
	if tcs[0].(map[string]any)["id"] != "call_1" || fn["name"] != "read" || fn["arguments"] != `{"path":"a.go"}` {
		t.Errorf("tool_call 形状错: %v", tcs[0])
	}
	if msgs[3]["role"] != "tool" || msgs[3]["tool_call_id"] != "call_1" || msgs[3]["content"] != "package main" {
		t.Errorf("[3] = %v", msgs[3])
	}
}

func TestBuildParams_SkipsEmptyAssistant(t *testing.T) {
	req := llm.CompletionRequest{Messages: []llm.Message{
		llm.UserText("hi"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.ThinkingBlock("想...", "sig")}},
	}}
	msgs := messagesOf(t, marshalParams(t, req))
	if len(msgs) != 1 || msgs[0]["role"] != "user" {
		t.Fatalf("空 assistant 未被跳过: %v", msgs)
	}
}

func TestBuildParams_ThinkingLevels(t *testing.T) {
	cases := map[llm.ThinkingLevel]any{
		llm.ThinkingOff:     nil,
		"":                  nil,
		llm.ThinkingMinimal: "minimal",
		llm.ThinkingLow:     "low",
		llm.ThinkingMedium:  "medium",
		llm.ThinkingHigh:    "high",
		llm.ThinkingXHigh:   "xhigh",
		llm.ThinkingMax:     "max",
	}
	for level, want := range cases {
		m := marshalParams(t, llm.CompletionRequest{Thinking: level, Messages: []llm.Message{llm.UserText("x")}})
		got, present := m["reasoning_effort"]
		if want == nil {
			if present {
				t.Errorf("thinking=%q: 期望省略 reasoning_effort, 得 %v", level, got)
			}
			continue
		}
		if got != want {
			t.Errorf("thinking=%q: reasoning_effort = %v, 期望 %v", level, got, want)
		}
	}
}

func TestBuildParams_MaxTokensAndStreamOptions(t *testing.T) {
	m := marshalParams(t, llm.CompletionRequest{MaxTokens: 1024, Messages: []llm.Message{llm.UserText("x")}})
	if got := m["max_completion_tokens"]; got != float64(1024) {
		t.Errorf("max_completion_tokens = %v", got)
	}
	if _, ok := m["max_tokens"]; ok {
		t.Errorf("不应出现弃用的 max_tokens")
	}
	so, ok := m["stream_options"].(map[string]any)
	if !ok || so["include_usage"] != true {
		t.Errorf("stream_options.include_usage 应恒为 true, 得 %v", m["stream_options"])
	}
}

func TestBuildParams_Tools(t *testing.T) {
	req := llm.CompletionRequest{
		Messages: []llm.Message{llm.UserText("x")},
		Tools: []llm.ToolSchema{{
			Name:        "read",
			Description: "读文件",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	}
	m := marshalParams(t, req)
	tools, ok := m["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", m["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Errorf("tool.type = %v, 期望 function", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "read" || fn["description"] != "读文件" {
		t.Errorf("function 元数据错: %v", fn)
	}
	if _, ok := fn["parameters"].(map[string]any); !ok {
		t.Errorf("parameters 未装配: %v", fn["parameters"])
	}
}

// 本地时间戳只用于回放 不能出现在发给模型的载荷里
func TestTimestampNotOnWire(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock("hi")}, Timestamp: 1794467680906},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("yo")}, Timestamp: 1794467681000},
	}
	params, err := convertMessages(nil, msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "timestamp") || strings.Contains(string(raw), "1794467680") {
		t.Errorf("timestamp leaked into the wire payload: %s", raw)
	}
}
