package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/anthropics/anthropic-sdk-go/v2"
)

// 将消息压成 "role:block+block" 结构串，# 表示缓存标记
func shape(params []anthropic.MessageParam) []string {
	var out []string
	for _, p := range params {
		var parts []string
		for _, b := range messageContent(p) {
			parts = append(parts, blockShape(b))
		}
		role := ""
		switch {
		case p.OfUser != nil:
			role = "user"
		case p.OfAssistant != nil:
			role = "assistant"
		case p.OfSystem != nil:
			role = "system"
		}
		out = append(out, role+":"+strings.Join(parts, "+"))
	}
	return out
}

func blockShape(b anthropic.ContentBlockParamUnion) string {
	mark := func(c anthropic.CacheControlEphemeralParam) string {
		if c.Type != "" {
			return "#"
		}
		return ""
	}
	switch {
	case b.OfText != nil:
		return "text:" + b.OfText.Text + mark(b.OfText.CacheControl)
	case b.OfThinking != nil:
		return "thinking"
	case b.OfRedactedThinking != nil:
		return "redacted"
	case b.OfToolUse != nil:
		return "tool_use:" + b.OfToolUse.Name + mark(b.OfToolUse.CacheControl)
	case b.OfToolResult != nil:
		return "tool_result:" + b.OfToolResult.ToolUseID + mark(b.OfToolResult.CacheControl)
	default:
		return "?"
	}
}

func assertShape(t *testing.T, got []string, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("shape 不符\n got: %q\nwant: %q", got, want)
	}
}

func user(s string) llm.Message { return llm.UserText(s) }
func asst(bs ...llm.ContentBlock) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: bs}
}
func toolUse(id, name string) llm.ContentBlock {
	return llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(`{}`)}
}
func result(id, text string, isErr bool) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText(id, text, isErr)}}
}

// 连续纯 tool_result 轮合并为一条 user 消息
func TestConvertMessagesToolResultMerge(t *testing.T) {
	params, err := convertMessages([]llm.Message{
		asst(toolUse("t1", "grep")),
		result("t1", "ra", false),
		result("t2", "rb", false),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(params), []string{
		"assistant:tool_use:grep",
		"user:tool_result:t1+tool_result:t2",
	})
}

// 签名 thinking 与 redacted 原样回放，无签名 thinking 降级为文本
func TestAssistantThinkingReplay(t *testing.T) {
	params, err := convertMessages([]llm.Message{
		asst(
			llm.ThinkingBlock("reason", "s1"),
			llm.ThinkingBlock("loose", ""),
			llm.TextBlock("hi"),
			llm.ContentBlock{Type: llm.BlockRedactedThinking, Data: "d"},
		),
		user("go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(params), []string{
		"assistant:thinking+text:loose+text:hi+redacted",
		"user:text:go",
	})
}

// tool_use 后的文本触发稳定收尾，thinking 不单独触发重排
func TestAssistantPartition(t *testing.T) {
	partitioned, err := convertMessages([]llm.Message{
		asst(llm.TextBlock("a"), toolUse("t1", "grep"), llm.TextBlock("b")),
		user("go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(partitioned), []string{
		"assistant:text:a+text:b+tool_use:grep",
		"user:text:go",
	})

	exempt, err := convertMessages([]llm.Message{
		asst(toolUse("t1", "grep"), llm.ThinkingBlock("x", "s")),
		user("go"),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(exempt), []string{
		"assistant:tool_use:grep+thinking",
		"user:text:go",
	})
}

// 空轮跳过，相邻 assistant 之间和末尾 assistant 后补 Continue 垫片
func TestContinuePad(t *testing.T) {
	params, err := convertMessages([]llm.Message{
		asst(llm.TextBlock("x")),
		user("   "),
		asst(llm.TextBlock("y")),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(params), []string{
		"assistant:text:x",
		"user:text:Continue.",
		"assistant:text:y",
		"user:text:Continue.",
	})
}

// 空轮被跳过，不进入输出序列
func TestEmptyTurnsSkipped(t *testing.T) {
	params, err := convertMessages([]llm.Message{
		user("hi"),
		asst(),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertShape(t, shape(params), []string{"user:text:hi"})
}

// 尾部断点跳过垫片和不可缓存块，落在最近的 text，tool_use 或 tool_result 上
func TestTailBreakpoint(t *testing.T) {
	cases := []struct {
		name  string
		msgs  []llm.Message
		shape []string
	}{
		{
			name:  "tool_result 尾",
			msgs:  []llm.Message{user("hi"), asst(toolUse("t1", "grep")), result("t1", "r", false)},
			shape: []string{"user:text:hi", "assistant:tool_use:grep", "user:tool_result:t1#"},
		},
		{
			name:  "结尾垫片跳过，锚前面 assistant",
			msgs:  []llm.Message{asst(llm.TextBlock("x"))},
			shape: []string{"assistant:text:x#", "user:text:Continue."},
		},
		{
			name:  "thinking 尾不可挂，锚 tool_use",
			msgs:  []llm.Message{asst(toolUse("t1", "grep"), llm.ThinkingBlock("x", "s"))},
			shape: []string{"assistant:tool_use:grep#+thinking", "user:text:Continue."},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			params, err := convertMessages(c.msgs)
			if err != nil {
				t.Fatal(err)
			}
			applyTailBreakpoint(params)
			assertShape(t, shape(params), c.shape)
		})
	}
}

// 边界落在剔空后的块上，为零不打断点，越界报错
func TestConvertSystemBoundary(t *testing.T) {
	blocks, err := convertSystem([]string{"a", "b", "", "c"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 || blocks[1].CacheControl.Type != "ephemeral" ||
		blocks[0].CacheControl.Type != "" || blocks[2].CacheControl.Type != "" {
		t.Fatalf("断点位置不符: %+v", blocks)
	}

	none, err := convertSystem([]string{"a"}, 0)
	if err != nil || (len(none) == 1 && none[0].CacheControl.Type != "") {
		t.Fatalf("boundary ≤ 0 不应打断点: %+v err=%v", none, err)
	}

	if _, err := convertSystem([]string{"a"}, 2); err == nil {
		t.Fatal("越界 boundary 应报错")
	}
}

// 空错误结果补固定文案，非空错误结果保留原内容
func TestToolResultErrorEmpty(t *testing.T) {
	empty, err := convertMessages([]llm.Message{result("t1", "", true)})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(empty)
	if !strings.Contains(string(wire), `"is_error":true`) ||
		!strings.Contains(string(wire), toolFailedNoOutput) {
		t.Fatalf("空 error result 应填文案并置 is_error: %s", wire)
	}

	filled, err := convertMessages([]llm.Message{result("t1", "boom", true)})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ = json.Marshal(filled)
	if !strings.Contains(string(wire), "boom") || strings.Contains(string(wire), toolFailedNoOutput) {
		t.Fatalf("非空 error result 不应改写内容: %s", wire)
	}
}

// user 或 assistant 轮出现非法块时返回错误
func TestInvariantsFail(t *testing.T) {
	if _, err := convertMessages([]llm.Message{{
		Role:    llm.RoleUser,
		Content: []llm.ContentBlock{toolUse("t1", "grep")},
	}}); err == nil {
		t.Fatal("user 轮出现 tool_use 应报错")
	}
	if _, err := convertMessages([]llm.Message{{
		Role:    llm.RoleAssistant,
		Content: []llm.ContentBlock{llm.ToolResultText("t1", "r", false)},
	}}); err == nil {
		t.Fatal("assistant 轮出现 tool_result 应报错")
	}
}

func sseServer(sse string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse))
	}))
}

func streamReq() llm.CompletionRequest {
	return llm.CompletionRequest{Messages: []llm.Message{llm.UserText("hi")}, MaxTokens: 100}
}

func collectFull(t *testing.T, sse string) (llm.Message, llm.StopReason, llm.Usage) {
	t.Helper()
	srv := sseServer(sse)
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, Model: "m"})
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

func sse(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

// 文本流拼接正文，映射终止原因，合并首末两处用量
func TestStreamText(t *testing.T) {
	msg, stop, usage := collectFull(t, sse(
		`event: message_start`,
		`data: {"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":10,"cache_read_input_tokens":3}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"world"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	))
	if msg.Text() != "Hello world" {
		t.Errorf("text = %q", msg.Text())
	}
	if stop != llm.StopReasonStop {
		t.Errorf("stop = %q", stop)
	}
	if usage.InputTokens != 10 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 {
		t.Errorf("usage = %+v", usage)
	}
}

// 占位输入被首个增量替换，分片拼回完整 JSON
func TestStreamToolUse(t *testing.T) {
	msg, stop, _ := collectFull(t, sse(
		`event: message_start`,
		`data: {"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":5}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"t1","name":"Bash","input":{}}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"cmd\":"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"ls\"}"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":9}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	))
	uses := msg.ToolUses()
	if len(uses) != 1 || uses[0].ID != "t1" || uses[0].Name != "Bash" {
		t.Fatalf("tool_use = %+v", uses)
	}
	if string(uses[0].Input) != `{"cmd":"ls"}` {
		t.Errorf("input = %s", uses[0].Input)
	}
	if stop != llm.StopReasonToolUse {
		t.Errorf("stop = %q", stop)
	}
}

// thinking 正文与签名各自拼接，签名只服务 thinking 块
func TestStreamThinking(t *testing.T) {
	msg, _, _ := collectFull(t, sse(
		`event: message_start`,
		`data: {"type":"message_start","message":{"role":"assistant","usage":{}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"let me "}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"think"}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig123"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	))
	b := msg.Content[0]
	if b.Type != llm.BlockThinking || b.Thinking != "let me think" || b.Signature != "sig123" {
		t.Fatalf("thinking block = %+v", b)
	}
}

// 未映射的 delta 类型让流报错，不被静默吞掉
func TestStreamCitationsFailLoud(t *testing.T) {
	srv := sseServer(sse(
		`event: message_start`,
		`data: {"type":"message_start","message":{"role":"assistant","usage":{}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"citations_delta","citation":{}}}`,
		``,
	))
	defer srv.Close()
	p := New(Config{BaseURL: srv.URL, Model: "m"})
	var gotErr error
	for _, err := range p.Stream(context.Background(), streamReq()) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "citations_delta") {
		t.Fatalf("citations_delta 应 fail loud，得到 %v", gotErr)
	}
}

// 取消作为 aborted 终态产出，不表现为传输错误
func TestStreamCancelAborted(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte(sse(
			`event: message_start`,
			`data: {"type":"message_start","message":{"role":"assistant","usage":{"input_tokens":5}}}`,
			``,
		)))
		w.(http.Flusher).Flush()
		<-released
	}))
	defer srv.Close()
	defer close(released)

	p := New(Config{BaseURL: srv.URL, Model: "m"})
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
		t.Fatalf("取消不应产生错误，得到 %v", gotErr)
	}
	if !aborted {
		t.Fatal("取消应产生 aborted 终态事件")
	}
}

func captureBody(t *testing.T, newProvider func(baseURL string) llm.Provider, req llm.CompletionRequest, stubSSE string) map[string]any {
	t.Helper()
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		raw, _ = io.ReadAll(r.Body)
		w.Write([]byte(stubSSE))
	}))
	defer srv.Close()
	for _, err := range newProvider(srv.URL).Stream(context.Background(), req) {
		if err != nil {
			t.Fatalf("stream error: %v", err)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal request body: %v\n%s", err, raw)
	}
	return m
}

// 实际发出的请求体带 max_tokens，模型名，系统断点，effort，尾部断点
func TestCaptureRequestBody(t *testing.T) {
	minimalSSE := sse(
		`event: message_start`,
		`data: {"type":"message_start","message":{"role":"assistant","usage":{}}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	)
	body := captureBody(t,
		func(baseURL string) llm.Provider { return New(Config{BaseURL: baseURL, Model: "claude-opus-5-5"}) },
		llm.CompletionRequest{
			System:        []string{"you are a bot"},
			CacheBoundary: 1,
			Messages:      []llm.Message{llm.UserText("hi")},
			MaxTokens:     1024,
			Thinking:      llm.ThinkingHigh,
		},
		minimalSSE,
	)

	if body["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens = %v", body["max_tokens"])
	}
	if body["model"] != "claude-opus-5-5" {
		t.Errorf("model = %v", body["model"])
	}
	system, ok := body["system"].([]any)
	if !ok || len(system) != 1 {
		t.Fatalf("system = %v", body["system"])
	}
	if _, ok := system[0].(map[string]any)["cache_control"]; !ok {
		t.Errorf("system 第 1 块应带 cache_control: %v", system[0])
	}
	oc, ok := body["output_config"].(map[string]any)
	if !ok || oc["effort"] != "high" {
		t.Errorf("output_config.effort = %v", body["output_config"])
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 1 {
		t.Fatalf("messages = %v", body["messages"])
	}
	content := msgs[0].(map[string]any)["content"].([]any)
	last := content[len(content)-1].(map[string]any)
	if _, ok := last["cache_control"]; !ok {
		t.Errorf("末尾 message 块应带 tail cache_control: %v", last)
	}
}

// 本地时间戳只用于回放 不能出现在发给模型的载荷里
func TestTimestampNotOnWire(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock("hi")}, Timestamp: 1794467680906},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("yo")}, Timestamp: 1794467681000},
	}
	params, err := convertMessages(msgs)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	wire, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(wire), "timestamp") || strings.Contains(string(wire), "1794467680") {
		t.Errorf("timestamp leaked into the wire payload: %s", wire)
	}
}
