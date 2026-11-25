package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func marshalResponseParams(t *testing.T, req llm.CompletionRequest) map[string]any {
	t.Helper()
	p := &Provider{model: "gpt-test"}
	params, err := p.buildResponseParams(req)
	if err != nil {
		t.Fatalf("buildResponseParams: %v", err)
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

func inputOf(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, ok := m["input"].([]any)
	if !ok {
		t.Fatalf("input 不是数组: %T", m["input"])
	}
	out := make([]map[string]any, len(raw))
	for i, r := range raw {
		out[i] = r.(map[string]any)
	}
	return out
}

func TestResponseParams_InputItems(t *testing.T) {
	req := llm.CompletionRequest{
		System: []string{"你是助手", "", "遵守规则"},
		Messages: []llm.Message{
			llm.UserText("读一下 a.go"),
			{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.ThinkingBlock("先想想", "sig"),
				llm.TextBlock("好的"),
				{Type: llm.BlockToolUse, ID: "call_1", Name: "read", Input: json.RawMessage(`{"path":"a.go"}`)},
			}},
			{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("call_1", "package main", false)}},
		},
	}
	m := marshalResponseParams(t, req)
	if m["instructions"] != "你是助手\n\n遵守规则" {
		t.Errorf("instructions = %v", m["instructions"])
	}
	if _, ok := m["stream_options"]; ok {
		t.Errorf("responses 不应设 stream_options")
	}
	items := inputOf(t, m)
	if len(items) != 4 {
		t.Fatalf("input 项数 = %d, 期望 4: %v", len(items), items)
	}
	if items[0]["role"] != "user" || items[0]["content"] != "读一下 a.go" {
		t.Errorf("[0] user = %v", items[0])
	}
	if items[1]["role"] != "assistant" || items[1]["content"] != "好的" {
		t.Errorf("[1] assistant 文本 = %v", items[1])
	}
	if items[2]["type"] != "function_call" || items[2]["call_id"] != "call_1" ||
		items[2]["name"] != "read" || items[2]["arguments"] != `{"path":"a.go"}` {
		t.Errorf("[2] function_call = %v", items[2])
	}
	if items[3]["type"] != "function_call_output" || items[3]["call_id"] != "call_1" ||
		items[3]["output"] != "package main" {
		t.Errorf("[3] function_call_output = %v", items[3])
	}
}

func TestResponseParams_Thinking(t *testing.T) {
	cases := map[llm.ThinkingLevel]any{
		llm.ThinkingOff:   nil,
		llm.ThinkingLow:   "low",
		llm.ThinkingHigh:  "high",
		llm.ThinkingXHigh: "xhigh",
		llm.ThinkingMax:   "max",
	}
	for level, want := range cases {
		m := marshalResponseParams(t, llm.CompletionRequest{Thinking: level, Messages: []llm.Message{llm.UserText("x")}})
		reasoning, _ := m["reasoning"].(map[string]any)
		if want == nil {
			if reasoning != nil {
				t.Errorf("thinking=%q 期望省略 reasoning, 得 %v", level, reasoning)
			}
			continue
		}
		if reasoning["effort"] != want {
			t.Errorf("thinking=%q effort = %v, 期望 %v", level, reasoning["effort"], want)
		}
	}
}

func TestResponseParams_Tools(t *testing.T) {
	m := marshalResponseParams(t, llm.CompletionRequest{
		Messages: []llm.Message{llm.UserText("x")},
		Tools:    []llm.ToolSchema{{Name: "read", Description: "读文件", InputSchema: json.RawMessage(`{"type":"object"}`)}},
	})
	tools, ok := m["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v", m["tools"])
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" || tool["name"] != "read" || tool["description"] != "读文件" {
		t.Errorf("function tool = %v", tool)
	}
	if m["max_output_tokens"] != nil {
		t.Errorf("未设 MaxTokens 不应出现 max_output_tokens")
	}
}

func respSSE(events ...string) string {
	var b strings.Builder
	for _, e := range events {
		b.WriteString("data: ")
		b.WriteString(e)
		b.WriteString("\n\n")
	}
	return b.String()
}

// 起桩服务器回放事件流，用 responses 模式跑完 Stream 并累积成完整消息
func collectResponses(t *testing.T, body string) (llm.Message, llm.StopReason, llm.Usage) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test", Api: llm.ProtocolOpenAIResponses})
	acc := llm.NewAccumulator()
	for ev, err := range p.Stream(context.Background(), streamReq()) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
		if err := acc.Add(ev); err != nil {
			t.Fatalf("accumulate: %v", err)
		}
	}
	return acc.Message(), acc.StopReason, acc.Usage
}

// 回放事件流并返回首个流错误，用于验证 fail loud 分支
func responsesStreamErr(t *testing.T, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}))
	defer srv.Close()
	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test", Api: llm.ProtocolOpenAIResponses})
	for _, err := range p.Stream(context.Background(), streamReq()) {
		if err != nil {
			return err
		}
	}
	return nil
}

func TestResponsesText(t *testing.T) {
	msg, stop, usage := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"Hello "}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"world"}`,
		`{"type":"response.output_text.done","output_index":0}`,
		`{"type":"response.output_item.done","output_index":0}`,
		`{"type":"response.completed","response":{"status":"completed","output":[],"usage":{"input_tokens":10,"output_tokens":7,"input_tokens_details":{"cached_tokens":3}}}}`,
	))
	if msg.Text() != "Hello world" {
		t.Errorf("text = %q", msg.Text())
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
		t.Errorf("usage = %+v", usage)
	}
}

// 文本块先于工具调用时，工具参数须路由到工具块而非文本块，验证到达序重编号
func TestResponsesToolCallF1(t *testing.T) {
	msg, stop, _ := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.output_text.delta","output_index":0,"delta":"Let me check"}`,
		`{"type":"response.output_item.done","output_index":0}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_a","name":"read"}}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"{\"path\":"}`,
		`{"type":"response.function_call_arguments.delta","output_index":1,"delta":"\"a.go\"}"}`,
		`{"type":"response.output_item.done","output_index":1}`,
		`{"type":"response.completed","response":{"status":"completed","output":[{"type":"function_call","call_id":"call_a","name":"read"}],"usage":{}}}`,
	))
	if msg.Text() != "Let me check" {
		t.Errorf("text = %q", msg.Text())
	}
	uses := msg.ToolUses()
	if len(uses) != 1 || uses[0].ID != "call_a" || uses[0].Name != "read" {
		t.Fatalf("tool_use = %+v", uses)
	}
	if string(uses[0].Input) != `{"path":"a.go"}` {
		t.Errorf("F1 回归, 工具参数应路由到工具块, 得 %s", uses[0].Input)
	}
	if stop != llm.StopReasonToolUse {
		t.Errorf("stop = %q", stop)
	}
}

func TestResponsesRefusal(t *testing.T) {
	msg, stop, _ := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.refusal.delta","output_index":0,"delta":"I cannot help"}`,
		`{"type":"response.output_item.done","output_index":0}`,
		`{"type":"response.completed","response":{"status":"completed","output":[],"usage":{}}}`,
	))
	if msg.Text() != "I cannot help" {
		t.Errorf("refusal = %q", msg.Text())
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
}

func TestResponsesIncompleteAndFailed(t *testing.T) {
	_, stop, _ := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.incomplete","response":{"status":"incomplete","output":[],"usage":{}}}`,
	))
	if stop != llm.StopReasonLength {
		t.Errorf("incomplete -> %q, 期望 length", stop)
	}
	_, stop2, _ := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.failed","response":{"status":"failed","output":[],"usage":{}}}`,
	))
	if stop2 != llm.StopReasonError {
		t.Errorf("failed -> %q, 期望 error", stop2)
	}
}

// reasoning 项与其增量应被吞掉，不产出 thinking 块
func TestResponsesReasoningSwallowed(t *testing.T) {
	msg, _, _ := collectResponses(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning"}}`,
		`{"type":"response.reasoning_text.delta","output_index":0,"delta":"thinking hard"}`,
		`{"type":"response.output_item.added","output_index":1,"item":{"type":"message","role":"assistant"}}`,
		`{"type":"response.output_text.delta","output_index":1,"delta":"answer"}`,
		`{"type":"response.output_item.done","output_index":1}`,
		`{"type":"response.completed","response":{"status":"completed","output":[],"usage":{}}}`,
	))
	if msg.Text() != "answer" {
		t.Errorf("text = %q", msg.Text())
	}
	for _, b := range msg.Content {
		if b.Type == llm.BlockThinking {
			t.Errorf("reasoning 应被吞, 不应出现 thinking 块")
		}
	}
}

func TestResponsesServerToolFailLoud(t *testing.T) {
	err := responsesStreamErr(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.output_item.added","output_index":0,"item":{"type":"file_search_call"}}`,
	))
	if err == nil || !strings.Contains(err.Error(), "file_search_call") {
		t.Fatalf("server tool 输出项应 fail loud, 得 %v", err)
	}
}

func TestResponsesErrorEventFailLoud(t *testing.T) {
	err := responsesStreamErr(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"error","code":"server_error","message":"boom"}`,
	))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error 事件应 fail loud, 得 %v", err)
	}
}

func TestResponsesUnknownEventFailLoud(t *testing.T) {
	err := responsesStreamErr(t, respSSE(
		`{"type":"response.created","response":{}}`,
		`{"type":"response.totally_new_event","output_index":0}`,
	))
	if err == nil || !strings.Contains(err.Error(), "totally_new_event") {
		t.Fatalf("未知事件应 fail loud, 得 %v", err)
	}
}

func TestResponsesCancelAborted(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(respSSE(`{"type":"response.created","response":{}}`)))
		w.(http.Flusher).Flush()
		<-released
	}))
	defer srv.Close()
	defer close(released)

	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test", Api: llm.ProtocolOpenAIResponses})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var gotErr error
	var aborted bool
	for ev, err := range p.Stream(ctx, streamReq()) {
		if err != nil {
			gotErr = err
			break
		}
		if ev.StopReason == llm.StopReasonAborted {
			aborted = true
			break
		}
		cancel()
	}
	if gotErr != nil {
		t.Fatalf("取消不应产生错误, 得到 %v", gotErr)
	}
	if !aborted {
		t.Fatal("取消应产生 aborted 终态事件")
	}
}
