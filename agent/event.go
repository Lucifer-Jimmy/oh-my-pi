package agent

import (
	"encoding/json"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type EventKind string

const (
	EventTextDelta     EventKind = "text_delta"
	EventThinkingDelta EventKind = "thinking_delta"
	EventToolUse       EventKind = "tool_use"
	EventToolResult    EventKind = "tool_result"
	EventProgress      EventKind = "progress"
	EventUsage         EventKind = "usage"
	EventNotification  EventKind = "notification"
	EventCompaction    EventKind = "compaction"
	EventResult        EventKind = "result"
)

type Event struct {
	// 本次运行内单调递增 由 loop 分配 供回放和排序使用
	Seq        int64               `json:"seq"`
	Timestamp  int64               `json:"timestamp,omitempty"`
	Kind       EventKind           `json:"kind"`
	Text       string              `json:"text,omitempty"`
	ToolUseID  string              `json:"tool_use_id,omitempty"`
	ToolName   string              `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage     `json:"tool_input,omitempty"`
	Approval   *Approval           `json:"approval,omitempty"`
	ToolResult *ToolResult         `json:"tool_result,omitempty"`
	Usage      *llm.Usage          `json:"usage,omitempty"`
	StopReason llm.StopReason      `json:"stop_reason,omitempty"`
	DurationMs int64               `json:"duration_ms,omitempty"`
	Message    string              `json:"message,omitempty"`
	Result     *Result             `json:"result,omitempty"`
	Compaction *session.Compaction `json:"compaction,omitempty"`
}

type Result struct {
	Reason   StopReason    `json:"reason"`
	Err      string        `json:"err,omitempty"`
	Usage    llm.Usage     `json:"usage"`
	Turns    int           `json:"turns"`
	Messages []llm.Message `json:"messages"`
	Text     string        `json:"text"`
	// 外层驱动用 中间的轮次不对外宣布结果
	Complete bool `json:"complete,omitempty"`
}

type StopReason string

const (
	ReasonCompleted   StopReason = "completed"
	ReasonMaxTurns    StopReason = "max_turns"
	ReasonTokenBudget StopReason = "token_budget"
	ReasonStopHook    StopReason = "stop_hook"
	ReasonVerifier    StopReason = "verifier"
	ReasonAborted     StopReason = "aborted"
	ReasonError       StopReason = "error"
)
