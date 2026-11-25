package llm

type Protocol string

const (
	ProtocolOpenAICompletions Protocol = "openai-completions"
	ProtocolOpenAIResponses   Protocol = "openai-responses"
	ProtocolAnthropicMessages Protocol = "anthropic-messages"
)

type ThinkingLevel string

const (
	ThinkingOff     ThinkingLevel = "off"
	ThinkingMinimal ThinkingLevel = "minimal"
	ThinkingLow     ThinkingLevel = "low"
	ThinkingMedium  ThinkingLevel = "medium"
	ThinkingHigh    ThinkingLevel = "high"
	ThinkingXHigh   ThinkingLevel = "xhigh"
	ThinkingMax     ThinkingLevel = "max"
)

type StopReason string

const (
	StopReasonStop    StopReason = "stop"    // 模型终止
	StopReasonLength  StopReason = "length"  // 截断恢复
	StopReasonToolUse StopReason = "toolUse" // 工具调用
	StopReasonError   StopReason = "error"   // 模型异常
	StopReasonAborted StopReason = "aborted" // 用户终止
)
