package anthropic

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	anthropic "github.com/anthropics/anthropic-sdk-go/v2"
)

const padContinue = "Continue."

const toolFailedNoOutput = "Tool failed with no output."

// 将中性 Message 转换成 Anthropic Message 序列
func convertMessages(msgs []llm.Message) ([]anthropic.MessageParam, error) {
	var out []anthropic.MessageParam
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if isToolResultTurn(m) {
			var blocks []anthropic.ContentBlockParamUnion
			for i < len(msgs) && isToolResultTurn(msgs[i]) {
				blocks = append(blocks, toolResultBlocks(msgs[i])...)
				i++
			}
			out = append(out, anthropic.NewUserMessage(blocks...))
			continue
		}
		var (
			blocks   []anthropic.ContentBlockParamUnion
			err      error
			newParam func(...anthropic.ContentBlockParamUnion) anthropic.MessageParam
		)
		switch m.Role {
		case llm.RoleUser:
			newParam = anthropic.NewUserMessage
			blocks, err = userBlocks(m)
		case llm.RoleAssistant:
			newParam = anthropic.NewAssistantMessage
			blocks, err = assistantBlocks(m)
		default:
			return nil, fmt.Errorf("anthropic: 未知 Role %q", m.Role)
		}
		if err != nil {
			return nil, err
		}
		if len(blocks) > 0 {
			out = append(out, newParam(blocks...))
		}
		i++
	}
	return fixAdjacency(out), nil
}

// 判断 user 轮是否只有 tool_result 块
func isToolResultTurn(m llm.Message) bool {
	if m.Role != llm.RoleUser || len(m.Content) == 0 {
		return false
	}
	for _, b := range m.Content {
		if b.Type != llm.BlockToolResult {
			return false
		}
	}
	return true
}

// 将中性层 user 内容转换为 Anthropic 块
func userBlocks(m llm.Message) ([]anthropic.ContentBlockParamUnion, error) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))
		case llm.BlockToolResult:
			blocks = append(blocks, toolResultBlock(b))
		default:
			return nil, fmt.Errorf("anthropic: user 轮出现 %s 块", b.Type)
		}
	}
	return blocks, nil
}

// 将中性层 assistant 内容转换为 Anthropic 块
func assistantBlocks(m llm.Message) ([]anthropic.ContentBlockParamUnion, error) {
	var blocks []anthropic.ContentBlockParamUnion
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			if strings.TrimSpace(b.Text) == "" {
				continue
			}
			blocks = append(blocks, anthropic.NewTextBlock(b.Text))
		case llm.BlockThinking:
			switch {
			case strings.TrimSpace(b.Thinking) == "":
				// 空 thinking 一律丢弃
			case b.Signature != "":
				blocks = append(blocks, anthropic.NewThinkingBlock(b.Signature, b.Thinking))
			default:
				blocks = append(blocks, anthropic.NewTextBlock(b.Thinking))
			}
		case llm.BlockRedactedThinking:
			if b.Data != "" {
				blocks = append(blocks, anthropic.NewRedactedThinkingBlock(b.Data))
			}
		case llm.BlockToolUse:
			input := b.Input
			if len(input) == 0 {
				input = json.RawMessage("{}")
			}
			blocks = append(blocks, anthropic.NewToolUseBlock(b.ID, input, b.Name))
		case llm.BlockToolResult:
			return nil, fmt.Errorf("anthropic: assistant 轮出现 tool_result 块")
		default:
			return nil, fmt.Errorf("anthropic: 块 %q 的回放待响应侧落地后实现", b.Type)
		}
	}
	return partitionToolUseTail(blocks), nil
}

// 必要时将 tool_use 稳定移到末尾，避免重放校验失败
func partitionToolUseTail(blocks []anthropic.ContentBlockParamUnion) []anthropic.ContentBlockParamUnion {
	seenToolUse, need := false, false
	for _, u := range blocks {
		switch {
		case u.OfToolUse != nil:
			seenToolUse = true
		case u.OfThinking != nil || u.OfRedactedThinking != nil:
		case seenToolUse:
			need = true
		}
	}
	if !need {
		return blocks
	}
	head := make([]anthropic.ContentBlockParamUnion, 0, len(blocks))
	var tail []anthropic.ContentBlockParamUnion
	for _, u := range blocks {
		if u.OfToolUse != nil {
			tail = append(tail, u)
		} else {
			head = append(head, u)
		}
	}
	return append(head, tail...)
}

// 将一批工具结果转换为 Anthropic 块
func toolResultBlocks(m llm.Message) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, len(m.Content))
	for i, b := range m.Content {
		out[i] = toolResultBlock(b)
	}
	return out
}

// 将中性层工具结果转为 Anthropic tool_result
func toolResultBlock(b llm.ContentBlock) anthropic.ContentBlockParamUnion {
	var content []anthropic.ToolResultBlockParamContentUnion
	for _, c := range b.Content {
		if c.Type == llm.BlockText && strings.TrimSpace(c.Text) != "" {
			content = append(content, anthropic.ToolResultBlockParamContentUnion{
				OfText: &anthropic.TextBlockParam{Text: c.Text},
			})
		}
	}
	p := anthropic.ToolResultBlockParam{ToolUseID: b.ToolUseID, Content: content}
	if b.IsError {
		p.IsError = anthropic.Bool(true)
		if len(content) == 0 {
			p.Content = []anthropic.ToolResultBlockParamContentUnion{
				{OfText: &anthropic.TextBlockParam{Text: toolFailedNoOutput}},
			}
		}
	}
	return anthropic.ContentBlockParamUnion{OfToolResult: &p}
}

// 避免连续 assistant 信息
func fixAdjacency(in []anthropic.MessageParam) []anthropic.MessageParam {
	out := make([]anthropic.MessageParam, 0, len(in)+1)
	for _, p := range in {
		if n := len(out); n > 0 && out[n-1].OfAssistant != nil && p.OfAssistant != nil {
			out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(padContinue)))
		}
		out = append(out, p)
	}
	if n := len(out); n > 0 && out[n-1].OfAssistant != nil {
		out = append(out, anthropic.NewUserMessage(anthropic.NewTextBlock(padContinue)))
	}
	return out
}

// 在一轮对话末尾添加缓存标记
func applyTailBreakpoint(params []anthropic.MessageParam) {
	for i := len(params) - 1; i >= 0; i-- {
		if isContinuePad(params[i]) {
			continue
		}
		content := messageContent(params[i])
		for j := len(content) - 1; j >= 0; j-- {
			u := content[j]
			switch {
			case u.OfText != nil:
				content[j].OfText.CacheControl = anthropic.NewCacheControlEphemeralParam()
				return
			case u.OfToolUse != nil:
				content[j].OfToolUse.CacheControl = anthropic.NewCacheControlEphemeralParam()
				return
			case u.OfToolResult != nil:
				content[j].OfToolResult.CacheControl = anthropic.NewCacheControlEphemeralParam()
				return
			}
			// thinking/redacted 不可缓存，继续向前找
		}
	}
}

func messageContent(p anthropic.MessageParam) []anthropic.ContentBlockParamUnion {
	switch {
	case p.OfUser != nil:
		return p.OfUser.Content
	case p.OfAssistant != nil:
		return p.OfAssistant.Content
	}
	return nil
}

// 判断是否为 Continue 填充
func isContinuePad(p anthropic.MessageParam) bool {
	return p.OfUser != nil && len(p.OfUser.Content) == 1 &&
		p.OfUser.Content[0].OfText != nil && p.OfUser.Content[0].OfText.Text == padContinue
}

// system 提示词转换成 api 接受的格式，添加缓存标记
func convertSystem(system []string, boundary int) ([]anthropic.TextBlockParam, error) {
	var blocks []anthropic.TextBlockParam
	for _, s := range system {
		if strings.TrimSpace(s) == "" {
			continue
		}
		blocks = append(blocks, anthropic.TextBlockParam{Text: s})
	}
	switch {
	case boundary <= 0:
	case boundary > len(blocks):
		return nil, fmt.Errorf("anthropic: CacheBoundary %d 越界, system 有效块 %d", boundary, len(blocks))
	default:
		blocks[boundary-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return blocks, nil
}

// 转换 wire 成中性事件
func emit(ev anthropic.MessageStreamEventUnion, yield func(llm.StreamEvent, error) bool) bool {
	switch ev.Type {
	case "message_start":
		return yield(llm.StreamEvent{Type: llm.SEMessageStart, Usage: usageFromStart(ev.Message.Usage)}, nil)
	case "content_block_start":
		return yield(llm.StreamEvent{Type: llm.SEBlockStart, BlockIndex: int(ev.Index), Block: startBlock(ev.ContentBlock)}, nil)
	case "content_block_delta":
		se, err := deltaEvent(ev)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return false
		}
		return yield(se, nil)
	case "content_block_stop":
		return yield(llm.StreamEvent{Type: llm.SEBlockStop, BlockIndex: int(ev.Index)}, nil)
	case "message_delta":
		return yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: mapStopReason(ev.Delta.StopReason), Usage: usageFromDelta(ev.Usage)}, nil)
	case "message_stop":
		return yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
	default:
		yield(llm.StreamEvent{}, fmt.Errorf("anthropic: 未知流事件 %q", ev.Type))
		return false
	}
}

func deltaEvent(ev anthropic.MessageStreamEventUnion) (llm.StreamEvent, error) {
	i := int(ev.Index)
	switch ev.Delta.Type {
	case "text_delta":
		return llm.StreamEvent{Type: llm.SETextDelta, BlockIndex: i, Text: ev.Delta.Text}, nil
	case "thinking_delta":
		return llm.StreamEvent{Type: llm.SEThinkingDelta, BlockIndex: i, Text: ev.Delta.Thinking}, nil
	case "signature_delta":
		return llm.StreamEvent{Type: llm.SEThinkingSignature, BlockIndex: i, Text: ev.Delta.Signature}, nil
	case "input_json_delta":
		return llm.StreamEvent{Type: llm.SEToolInputJSON, BlockIndex: i, Text: ev.Delta.PartialJSON}, nil
	default:
		return llm.StreamEvent{}, fmt.Errorf("anthropic: 未映射的 delta 类型 %q", ev.Delta.Type)
	}
}

func startBlock(cb anthropic.ContentBlockStartEventContentBlockUnion) llm.ContentBlock {
	switch cb.Type {
	case "text":
		return llm.ContentBlock{Type: llm.BlockText, Text: cb.Text}
	case "thinking":
		return llm.ContentBlock{Type: llm.BlockThinking, Thinking: cb.Thinking, Signature: cb.Signature}
	case "redacted_thinking":
		return llm.ContentBlock{Type: llm.BlockRedactedThinking, Data: cb.Data}
	case "tool_use":
		return llm.ContentBlock{Type: llm.BlockToolUse, ID: cb.ID, Name: cb.Name, Input: json.RawMessage("{}")}
	default:
		return llm.ContentBlock{Type: llm.BlockType(cb.Type), Raw: json.RawMessage(cb.RawJSON())}
	}
}

func mapStopReason(sr anthropic.StopReason) llm.StopReason {
	switch sr {
	case "":
		return ""
	case "end_turn", "stop_sequence", "pause_turn", "refusal":
		return llm.StopReasonStop
	case "max_tokens", "model_context_window_exceeded":
		return llm.StopReasonLength
	case "tool_use":
		return llm.StopReasonToolUse
	default:
		return llm.StopReasonError
	}
}

func usageFromStart(u anthropic.Usage) llm.Usage {
	return llm.Usage{
		InputTokens:      int(u.InputTokens),
		OutputTokens:     int(u.OutputTokens),
		CacheReadTokens:  int(u.CacheReadInputTokens),
		CacheWriteTokens: int(u.CacheCreationInputTokens),
	}
}

func usageFromDelta(u anthropic.MessageDeltaUsage) llm.Usage {
	return llm.Usage{
		InputTokens:      int(u.InputTokens),
		OutputTokens:     int(u.OutputTokens),
		CacheReadTokens:  int(u.CacheReadInputTokens),
		CacheWriteTokens: int(u.CacheCreationInputTokens),
	}
}
