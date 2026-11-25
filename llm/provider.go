package llm

import (
	"context"
	"encoding/json"
	"iter"
)

type StreamEventType string

const (
	SEMessageStart      StreamEventType = "message_start"
	SEBlockStart        StreamEventType = "block_start"
	SETextDelta         StreamEventType = "text_delta"
	SEThinkingDelta     StreamEventType = "thinking_delta"
	SEThinkingSignature StreamEventType = "thinking_signature"
	SEToolInputJSON     StreamEventType = "tool_input_delta"
	SEBlockStop         StreamEventType = "block_stop"
	SEMessageDelta      StreamEventType = "message_delta"
	SEMessageStop       StreamEventType = "message_stop"
)

type StreamEvent struct {
	Type       StreamEventType
	Text       string       // text / thinking / signature / tool_input 增量片段
	BlockIndex int          // 块路由键，自 0 连续递增
	StopReason StopReason   // 终止原因
	Usage      Usage        // 用量
	Block      ContentBlock // block_start 载荷，整块类型即全部内容
}

type ToolSchema struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

type CompletionRequest struct {
	System        []string
	CacheBoundary int
	Messages      []Message
	Tools         []ToolSchema
	MaxTokens     int
	Thinking      ThinkingLevel
}

type Provider interface {
	Stream(ctx context.Context, req CompletionRequest) iter.Seq2[StreamEvent, error]
	Complete(ctx context.Context, req CompletionRequest) (Message, StopReason, Usage, error)
}
