package openai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func sse(chunks ...string) string {
	var b strings.Builder
	for _, c := range chunks {
		b.WriteString("data: ")
		b.WriteString(c)
		b.WriteString("\n\n")
	}
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func sseServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(body))
	}))
}

func streamReq() llm.CompletionRequest {
	return llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}, MaxTokens: 100}
}

func collectFull(t *testing.T, body string) (llm.Message, llm.StopReason, llm.Usage) {
	t.Helper()
	srv := sseServer(body)
	defer srv.Close()
	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test"})
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

func streamErr(t *testing.T, body string) error {
	t.Helper()
	srv := sseServer(body)
	defer srv.Close()
	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test"})
	for _, err := range p.Stream(context.Background(), streamReq()) {
		if err != nil {
			return err
		}
	}
	return nil
}

func TestStreamText(t *testing.T) {
	msg, stop, usage := collectFull(t, sse(
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hello "}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"world"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":3}}}`,
	))
	if msg.Text() != "Hello world" {
		t.Errorf("text = %q", msg.Text())
	}
	if len(msg.Content) != 1 {
		t.Errorf("空 content 首片不应开块, 得 %d 块", len(msg.Content))
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
		t.Errorf("usage = %+v", usage)
	}
}

func TestStreamToolCallF1(t *testing.T) {
	msg, stop, _ := collectFull(t, sse(
		`{"choices":[{"index":0,"delta":{"content":"Let me check"}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"a.go\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	))
	if msg.Text() != "Let me check" {
		t.Errorf("text = %q", msg.Text())
	}
	uses := msg.ToolUses()
	if len(uses) != 1 || uses[0].ID != "call_a" || uses[0].Name != "read" {
		t.Fatalf("tool_use = %+v", uses)
	}
	if string(uses[0].Input) != `{"path":"a.go"}` {
		t.Errorf("F1 回归, 工具参数应路由到工具块而非标量块, 得 %s", uses[0].Input)
	}
	if stop != llm.StopReasonToolUse {
		t.Errorf("stop = %q", stop)
	}
}

func TestStreamParallelTools(t *testing.T) {
	msg, _, _ := collectFull(t, sse(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c0","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"c1","type":"function","function":{"name":"grep","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"p\":\"a\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"q\":\"b\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	))
	uses := msg.ToolUses()
	if len(uses) != 2 {
		t.Fatalf("uses = %+v", uses)
	}
	if uses[0].Name != "read" || string(uses[0].Input) != `{"p":"a"}` {
		t.Errorf("tool0 串块, 得 %+v", uses[0])
	}
	if uses[1].Name != "grep" || string(uses[1].Input) != `{"q":"b"}` {
		t.Errorf("tool1 串块, 得 %+v", uses[1])
	}
}

func TestStreamRefusal(t *testing.T) {
	msg, stop, _ := collectFull(t, sse(
		`{"choices":[{"index":0,"delta":{"refusal":"I cannot"}}]}`,
		`{"choices":[{"index":0,"delta":{"refusal":" help"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))
	if msg.Text() != "I cannot help" {
		t.Errorf("refusal 文本 = %q", msg.Text())
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
}

func TestStreamStopReasons(t *testing.T) {
	cases := map[string]llm.StopReason{
		"stop":           llm.StopReasonStop,
		"length":         llm.StopReasonLength,
		"tool_calls":     llm.StopReasonToolUse,
		"content_filter": llm.StopReasonError,
		"weird":          llm.StopReasonError,
	}
	for fr, want := range cases {
		_, stop, _ := collectFull(t, sse(
			`{"choices":[{"index":0,"delta":{"content":"x"}}]}`,
			`{"choices":[{"index":0,"delta":{},"finish_reason":"`+fr+`"}]}`,
		))
		if stop != want {
			t.Errorf("finish %q -> %q, 期望 %q", fr, stop, want)
		}
	}
}

func TestStreamEmptyStop(t *testing.T) {
	msg, stop, _ := collectFull(t, sse(
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	))
	if len(msg.Content) != 0 {
		t.Errorf("空内容纯 stop 应无块, 得 %+v", msg.Content)
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
}

func TestStreamCancelAborted(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi\"}}]}\n\n"))
		w.(http.Flusher).Flush()
		<-released
	}))
	defer srv.Close()
	defer close(released)

	p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test"})
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

func TestStreamFunctionCallFailLoud(t *testing.T) {
	err := streamErr(t, sse(
		`{"choices":[{"index":0,"delta":{"function_call":{"name":"f","arguments":"{}"}}}]}`,
	))
	if err == nil || !strings.Contains(err.Error(), "function_call") {
		t.Fatalf("废弃 function_call 应 fail loud, 得 %v", err)
	}
}

func TestStreamChoiceIndexFailLoud(t *testing.T) {
	err := streamErr(t, sse(
		`{"choices":[{"index":1,"delta":{"content":"x"}}]}`,
	))
	if err == nil || !strings.Contains(err.Error(), "choice index") {
		t.Fatalf("choice index>0 应 fail loud, 得 %v", err)
	}
}

func TestStreamToolMissingID(t *testing.T) {
	err := streamErr(t, sse(
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"type":"function","function":{"name":"read","arguments":"{}"}}]}}]}`,
	))
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("工具首现缺 id 应 fail loud, 得 %v", err)
	}
}

func TestStreamAPIErrorShowsServerMessage(t *testing.T) {
	for _, api := range []llm.Protocol{llm.ProtocolOpenAICompletions, llm.ProtocolOpenAIResponses} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":{"message":"model gpt-test does not exist","type":"invalid_request_error","code":"model_not_found"}}`))
		}))
		p := New(Config{APIKey: "test", BaseURL: srv.URL, Model: "gpt-test", Api: api})
		var got error
		for _, err := range p.Stream(context.Background(), streamReq()) {
			if err != nil {
				got = err
			}
		}
		srv.Close()
		if got == nil || !strings.Contains(got.Error(), "404") || !strings.Contains(got.Error(), "model gpt-test does not exist") {
			t.Errorf("%s: got %v", api, got)
		}
	}
}
