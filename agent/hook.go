package agent

import (
	"context"
	"encoding/json"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type PreToolCallHook interface {
	BeforeToolCall(ctx context.Context, name string, input json.RawMessage) (Decision, error)
}

type Decision struct {
	Block    bool
	Reason   string
	NewInput json.RawMessage
}

type StopHook interface {
	OnStop(ctx context.Context) (StopDecision, error)
}

type StopDecision struct {
	Prevent  bool
	Blocking bool
	Reason   string
}

type PostToolCallHook interface {
	AfterToolCall(ctx context.Context, name string, result ToolResult) (*ToolResult, error)
}

type PromptInjectHook interface {
	OnPrompt(ctx context.Context) ([]llm.Message, error)
}
