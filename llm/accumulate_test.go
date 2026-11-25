package llm

import (
	"encoding/json"
	"testing"
)

func feed(t *testing.T, acc *Accumulator, events []StreamEvent) {
	t.Helper()
	for _, ev := range events {
		if err := acc.Add(ev); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
}

func TestAccumulator(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart, Usage: Usage{InputTokens: 500, CacheReadTokens: 200, OutputTokens: 1}}, // 占位 output
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockThinking}},
		{Type: SEThinkingDelta, BlockIndex: 0, Text: "先读文件"},
		{Type: SEThinkingSignature, BlockIndex: 0, Text: "sig1"},
		{Type: SEBlockStop, BlockIndex: 0},
		{Type: SEBlockStart, BlockIndex: 1, Block: ContentBlock{Type: BlockText}},
		{Type: SETextDelta, BlockIndex: 1, Text: "Hel"},
		{Type: SETextDelta, BlockIndex: 1, Text: "lo"},
		{Type: SEBlockStop, BlockIndex: 1},
		{Type: SEBlockStart, BlockIndex: 2, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Bash"}},
		{Type: SEToolInputJSON, BlockIndex: 2, Text: `{"command":`},
		{Type: SEToolInputJSON, BlockIndex: 2, Text: `"ls"}`},
		{Type: SEBlockStop, BlockIndex: 2},
		{Type: SEMessageDelta, StopReason: StopReasonToolUse,
			Usage: Usage{InputTokens: 510, CacheReadTokens: 200, OutputTokens: 99}},
		{Type: SEMessageStop},
	})
	msg := acc.Message()

	if msg.Role != RoleAssistant {
		t.Fatalf("Role = %q", msg.Role)
	}
	if msg.Text() != "Hello" {
		t.Fatalf("Text = %q", msg.Text())
	}
	// thinking 块带签名，按到达序排在 text 之前
	if b := msg.Content[0]; b.Type != BlockThinking || b.Thinking != "先读文件" || b.Signature != "sig1" {
		t.Fatalf("thinking block = %+v", b)
	}
	uses := msg.ToolUses()
	if len(uses) != 1 || uses[0].Name != "Bash" || string(uses[0].Input) != `{"command":"ls"}` {
		t.Fatalf("tooluse = %+v", uses)
	}
	if acc.Usage.InputTokens != 510 || acc.Usage.CacheReadTokens != 200 || acc.Usage.OutputTokens != 99 {
		t.Fatalf("usage = %+v", acc.Usage)
	}
	if acc.StopReason != StopReasonToolUse {
		t.Fatalf("StopReason = %q", acc.StopReason)
	}
}

func TestAccumulatorMessageRestart(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart, Usage: Usage{InputTokens: 500, OutputTokens: 1}},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}},
		{Type: SETextDelta, BlockIndex: 0, Text: "hello"},
		{Type: SEMessageStop},
	})
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart, Usage: Usage{InputTokens: 600, OutputTokens: 1}},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}},
		{Type: SETextDelta, BlockIndex: 0, Text: "world"},
		{Type: SEMessageStop},
	})
	if msg := acc.Message(); msg.Text() != "world" {
		t.Fatalf("message_start 应整包替换: %q", msg.Text())
	}
	if acc.Usage.InputTokens != 600 {
		t.Fatalf("替换应重置 usage: %+v", acc.Usage)
	}
}

func TestAccumulatorMalformedToolInput(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Read"}},
		{Type: SEToolInputJSON, BlockIndex: 0, Text: `{"file_path":"a`}, // 被截断的 JSON
		{Type: SEMessageStop},
	})
	if uses := acc.Message().ToolUses(); len(uses) != 1 || string(uses[0].Input) != "{}" {
		t.Fatalf("invalid json falls back to \"{}\": %+v", uses)
	}
}

func TestAccumulatorInterleavedToolBlocks(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Read"}},
		{Type: SEToolInputJSON, BlockIndex: 0, Text: `{"file_path":`},
		{Type: SEBlockStart, BlockIndex: 1, Block: ContentBlock{Type: BlockToolUse, ID: "t2", Name: "Bash"}},
		{Type: SEToolInputJSON, BlockIndex: 1, Text: `{"command":`},
		{Type: SEToolInputJSON, BlockIndex: 0, Text: `"a.go"}`},
		{Type: SEToolInputJSON, BlockIndex: 1, Text: `"ls"}`},
		{Type: SEMessageStop},
	})
	uses := acc.Message().ToolUses()
	if len(uses) != 2 {
		t.Fatalf("tooluse number = %d, want 2", len(uses))
	}
	if uses[0].ID != "t1" || string(uses[0].Input) != `{"file_path":"a.go"}` {
		t.Fatalf("t1 = %+v", uses[0])
	}
	if uses[1].ID != "t2" || string(uses[1].Input) != `{"command":"ls"}` {
		t.Fatalf("t2 = %+v", uses[1])
	}
}

func TestAccumulatorSignatureAppend(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockThinking}},
		{Type: SEThinkingSignature, BlockIndex: 0, Text: "sig-"},
		{Type: SEThinkingSignature, BlockIndex: 0, Text: "1"},
		{Type: SEMessageStop},
	})
	msg := acc.Message()
	if msg.Content[0].Signature != "sig-1" {
		t.Fatalf("thinking signature 应拼接: %q", msg.Content[0].Signature)
	}
}

func TestAccumulatorRedactedBlock(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockRedactedThinking, Data: "sealed"}},
		{Type: SEBlockStop, BlockIndex: 0},
		{Type: SEMessageStop},
	})
	if b := acc.Message().Content[0]; b.Type != BlockRedactedThinking || b.Data != "sealed" {
		t.Fatalf("redacted block 应原样透传: %+v", b)
	}
}

func TestAccumulatorEmptyToolInput(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "WebFetch"}},
		{Type: SEBlockStop, BlockIndex: 0},
		{Type: SEMessageStop},
	})
	if uses := acc.Message().ToolUses(); len(uses) != 1 || string(uses[0].Input) != "{}" {
		t.Fatalf("无 input 增量应回退 \"{}\": %+v", uses)
	}
}

func TestAccumulatorPlaceholderInput(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Bash",
			Input: json.RawMessage("{}")}}, // wire start 块占位
		{Type: SEToolInputJSON, BlockIndex: 0, Text: `{"command":`},
		{Type: SEToolInputJSON, BlockIndex: 0, Text: `"ls"}`},
		{Type: SEMessageStop},
	})
	if uses := acc.Message().ToolUses(); len(uses) != 1 || string(uses[0].Input) != `{"command":"ls"}` {
		t.Fatalf("占位 input 应被首段增量替换: %+v", uses)
	}
}

func TestAccumulatorNonObjectInput(t *testing.T) {
	for _, raw := range []string{" null ", "5", `"ls"`, "[]", "true", ""} {
		acc := NewAccumulator()
		feed(t, acc, []StreamEvent{
			{Type: SEMessageStart},
			{Type: SEBlockStart, BlockIndex: 0,
				Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Bash", Input: json.RawMessage(raw)}},
			{Type: SEMessageStop},
		})
		if uses := acc.Message().ToolUses(); string(uses[0].Input) != "{}" {
			t.Fatalf("非对象 input %q 应回退 \"{}\"", raw)
		}
	}
}

func TestAccumulatorStopPayload(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart, Usage: Usage{InputTokens: 500, OutputTokens: 1}}, // 占位 output
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}},
		{Type: SETextDelta, BlockIndex: 0, Text: "hi"},
		{Type: SEMessageStop, StopReason: StopReasonStop,
			Usage: Usage{InputTokens: 510, OutputTokens: 99}},
	})
	if acc.Usage.InputTokens != 510 || acc.Usage.OutputTokens != 99 {
		t.Fatalf("message_stop 载荷应生效: %+v", acc.Usage)
	}
	if acc.StopReason != StopReasonStop {
		t.Fatalf("StopReason = %q", acc.StopReason)
	}
}

func TestAccumulatorMessageMidStream(t *testing.T) {
	acc := NewAccumulator()
	feed(t, acc, []StreamEvent{
		{Type: SEMessageStart},
		{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}},
		{Type: SETextDelta, BlockIndex: 0, Text: "Hel"},
	})
	partial := acc.Message() // 流中快照
	if partial.Text() != "Hel" {
		t.Fatalf("partial Text = %q", partial.Text())
	}
	feed(t, acc, []StreamEvent{
		{Type: SETextDelta, BlockIndex: 0, Text: "lo"},
		{Type: SEBlockStart, BlockIndex: 1, Block: ContentBlock{Type: BlockToolUse, ID: "t1", Name: "Bash"}},
		{Type: SEToolInputJSON, BlockIndex: 1, Text: `{"command":`},
	})
	_ = acc.Message() // 流中快照不破坏内部状态
	feed(t, acc, []StreamEvent{
		{Type: SEToolInputJSON, BlockIndex: 1, Text: `"ls"}`},
		{Type: SEMessageStop},
	})

	if partial.Text() != "Hel" { // 快照不被后续增量改写
		t.Fatalf("快照被改写: %q", partial.Text())
	}
	final := acc.Message()
	if final.Text() != "Hello" {
		t.Fatalf("final Text = %q", final.Text())
	}
	if uses := final.ToolUses(); len(uses) != 1 || string(uses[0].Input) != `{"command":"ls"}` {
		t.Fatalf("final tooluse = %+v", uses)
	}
}

func TestAccumulatorErrors(t *testing.T) {
	tests := []struct {
		name   string
		events []StreamEvent
	}{
		{"block_start 索引不连续", []StreamEvent{
			{Type: SEMessageStart},
			{Type: SEBlockStart, BlockIndex: 1, Block: ContentBlock{Type: BlockText}}}},
		{"block_start 索引回退", []StreamEvent{
			{Type: SEMessageStart},
			{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}},
			{Type: SEBlockStart, BlockIndex: 0, Block: ContentBlock{Type: BlockText}}}},
		{"增量越界", []StreamEvent{
			{Type: SEMessageStart},
			{Type: SETextDelta, BlockIndex: 3, Text: "x"}}},
		{"block_stop 越界", []StreamEvent{
			{Type: SEMessageStart},
			{Type: SEBlockStop, BlockIndex: 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := NewAccumulator()
			var err error
			for _, ev := range tt.events {
				if err = acc.Add(ev); err != nil {
					break
				}
			}
			if err == nil {
				t.Fatalf("want error")
			}
		})
	}
}
