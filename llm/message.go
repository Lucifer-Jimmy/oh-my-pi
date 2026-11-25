package llm

import (
	"encoding/json"
	"strings"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockType string

const (
	BlockText             BlockType = "text"
	BlockThinking         BlockType = "thinking"
	BlockToolUse          BlockType = "tool_use"
	BlockToolResult       BlockType = "tool_result"
	BlockRedactedThinking BlockType = "redacted_thinking" // Anthropic feature
)

type ContentBlock struct {
	Type BlockType `json:"type"`
	Text string    `json:"text,omitempty"`

	// thinking block
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// tool_use block
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result block
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   []ContentBlock `json:"content,omitempty"`
	IsError   bool           `json:"is_error,omitempty"`

	// redacted_thinking block
	// Data 原样透传
	Data string `json:"data,omitempty"`

	// 未知块整块透传
	Raw json.RawMessage `json:"raw,omitempty"`
}

func TextBlock(text string) ContentBlock { return ContentBlock{Type: BlockText, Text: text} }

func ThinkingBlock(thinking, signature string) ContentBlock {
	return ContentBlock{
		Type:      BlockThinking,
		Thinking:  thinking,
		Signature: signature,
	}
}

func ToolResultText(toolUseID, text string, isError bool) ContentBlock {
	return ContentBlock{
		Type:      BlockToolResult,
		ToolUseID: toolUseID,
		Content:   []ContentBlock{TextBlock(text)},
		IsError:   isError,
	}
}

type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
	// 本地记录的时刻 Unix 毫秒 只用于会话回放和展示 不发给模型
	Timestamp int64  `json:"timestamp,omitempty"`
	Usage     *Usage `json:"usage,omitempty"`
}

func UserText(s string) Message {
	return Message{Role: RoleUser, Content: []ContentBlock{TextBlock(s)}}
}

func (m Message) ToolUses() []ContentBlock {
	var out []ContentBlock
	for _, b := range m.Content {
		if b.Type == BlockToolUse {
			out = append(out, b)
		}
	}
	return out
}

func (m Message) Text() string {
	var sb strings.Builder
	for _, b := range m.Content {
		if b.Type == BlockText {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}
