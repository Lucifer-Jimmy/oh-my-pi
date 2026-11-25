package openai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

func (p *Provider) buildResponseParams(req llm.CompletionRequest) (responses.ResponseNewParams, error) {
	input, err := convertResponseInput(req.Messages)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := convertResponseTools(req.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(p.model),
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Tools: tools,
	}
	if s := joinSystem(req.System); s != "" {
		params.Instructions = openai.String(s)
	}
	if req.MaxTokens > 0 {
		params.MaxOutputTokens = openai.Int(int64(req.MaxTokens))
	}
	applyResponseThinking(&params, req.Thinking)
	return params, nil
}

func convertResponseInput(msgs []llm.Message) (responses.ResponseInputParam, error) {
	var out responses.ResponseInputParam
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			items, err := responseUserItems(m)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		case llm.RoleAssistant:
			items, err := responseAssistantItems(m)
			if err != nil {
				return nil, err
			}
			out = append(out, items...)
		default:
			return nil, fmt.Errorf("openai: 未知 Role %q", m.Role)
		}
	}
	return out, nil
}

func responseUserItems(m llm.Message) ([]responses.ResponseInputItemUnionParam, error) {
	var out []responses.ResponseInputItemUnionParam
	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			text.WriteString(b.Text)
		case llm.BlockToolResult:
			out = append(out, responseToolOutput(b))
		default:
			return nil, fmt.Errorf("openai: user 轮出现 %s 块", b.Type)
		}
	}
	if strings.TrimSpace(text.String()) != "" {
		out = append(out, responses.ResponseInputItemParamOfMessage(text.String(), responses.EasyInputMessageRoleUser))
	}
	return out, nil
}

// 翻译 assistant 轮，遇工具调用前先冲刷累积文本，保持文本与工具调用的原始先后顺序
func responseAssistantItems(m llm.Message) ([]responses.ResponseInputItemUnionParam, error) {
	var out []responses.ResponseInputItemUnionParam
	var text strings.Builder
	flushText := func() {
		if strings.TrimSpace(text.String()) != "" {
			out = append(out, responses.ResponseInputItemParamOfMessage(text.String(), responses.EasyInputMessageRoleAssistant))
		}
		text.Reset()
	}
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			text.WriteString(b.Text)
		case llm.BlockThinking, llm.BlockRedactedThinking:
		case llm.BlockToolUse:
			flushText()
			args := strings.TrimSpace(string(b.Input))
			if args == "" {
				args = "{}"
			}
			out = append(out, responses.ResponseInputItemUnionParam{
				OfFunctionCall: &responses.ResponseFunctionToolCallParam{
					CallID:    b.ID,
					Name:      b.Name,
					Arguments: args,
				},
			})
		case llm.BlockToolResult:
			return nil, fmt.Errorf("openai: assistant 轮出现 tool_result 块")
		default:
			return nil, fmt.Errorf("openai: 块 %q 的回放未实现", b.Type)
		}
	}
	flushText()
	return out, nil
}

// 翻译工具结果，错误且无输出时补占位文案
func responseToolOutput(b llm.ContentBlock) responses.ResponseInputItemUnionParam {
	var sb strings.Builder
	for _, c := range b.Content {
		if c.Type == llm.BlockText {
			sb.WriteString(c.Text)
		}
	}
	content := sb.String()
	if b.IsError && strings.TrimSpace(content) == "" {
		content = toolFailedNoOutput
	}
	return responses.ResponseInputItemUnionParam{
		OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
			CallID: openai.String(b.ToolUseID),
			Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{OfString: openai.String(content)},
		},
	}
}

func convertResponseTools(tools []llm.ToolSchema) ([]responses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var params map[string]any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("openai: 工具 %q 的 InputSchema 解析失败: %w", t.Name, err)
			}
		}
		tool := responses.ToolParamOfFunction(t.Name, params, false)
		if t.Description != "" {
			tool.OfFunction.Description = openai.String(t.Description)
		}
		out = append(out, tool)
	}
	return out, nil
}

// 映射 thinking 档位到 reasoning effort，off 与未设省略其余直传
func applyResponseThinking(params *responses.ResponseNewParams, level llm.ThinkingLevel) {
	switch level {
	case llm.ThinkingOff, "":
	default:
		params.Reasoning.Effort = shared.ReasoningEffort(level)
	}
}

// 跑 responses 流式请求并产出中性事件，ctx 取消作为 aborted 终态产出，不作传输错误
func (p *Provider) streamResponses(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		params, err := p.buildResponseParams(req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		stream := p.client.Responses.NewStreaming(ctx, params)
		defer stream.Close()
		synth := newResponseSynth()
		for stream.Next() {
			events, err := synth.feed(stream.Current())
			if err != nil {
				yield(llm.StreamEvent{}, err)
				return
			}
			for _, ev := range events {
				if !yield(ev, nil) {
					return
				}
			}
		}
		if err := stream.Err(); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				for _, ev := range synth.abort() {
					if !yield(ev, nil) {
						return
					}
				}
				return
			}
			yield(llm.StreamEvent{}, classifyHTTP(withServerMessage(err)))
		}
	}
}

type responseSynth struct {
	items    map[int64]int
	openText map[int]bool
}

func newResponseSynth() *responseSynth {
	return &responseSynth{items: map[int64]int{}, openText: map[int]bool{}}
}

// 把 wire 的 output_index 首现登记为连续 BlockIndex，后续同 index 复用同一块号
func (s *responseSynth) block(outputIndex int64) int {
	if bi, ok := s.items[outputIndex]; ok {
		return bi
	}
	bi := len(s.items)
	s.items[outputIndex] = bi
	return bi
}

// 产出文本增量，该块首次出现时先补一个 block_start
func (s *responseSynth) textDelta(outputIndex int64, text string) []llm.StreamEvent {
	bi := s.block(outputIndex)
	var evs []llm.StreamEvent
	if !s.openText[bi] {
		s.openText[bi] = true
		evs = append(evs, llm.StreamEvent{Type: llm.SEBlockStart, BlockIndex: bi, Block: llm.ContentBlock{Type: llm.BlockText}})
	}
	evs = append(evs, llm.StreamEvent{Type: llm.SETextDelta, BlockIndex: bi, Text: text})
	return evs
}

// 把一个 responses 流事件翻成中性事件，核心数据事件映射，生命周期噪音吞掉，server tool 与未知事件报错
func (s *responseSynth) feed(ev responses.ResponseStreamEventUnion) ([]llm.StreamEvent, error) {
	switch ev.Type {
	case "response.created":
		return []llm.StreamEvent{{Type: llm.SEMessageStart}}, nil

	case "response.output_item.added":
		item := ev.Item
		switch item.Type {
		case "message", "reasoning":
			return nil, nil
		case "function_call":
			bi := s.block(ev.OutputIndex)
			return []llm.StreamEvent{{
				Type:       llm.SEBlockStart,
				BlockIndex: bi,
				Block:      llm.ContentBlock{Type: llm.BlockToolUse, ID: item.CallID, Name: item.Name, Input: json.RawMessage("{}")},
			}}, nil
		default:
			return nil, fmt.Errorf("openai: 不支持的 responses 输出项 %q", item.Type)
		}

	case "response.output_text.delta", "response.refusal.delta":
		return s.textDelta(ev.OutputIndex, ev.Delta), nil

	case "response.function_call_arguments.delta":
		return []llm.StreamEvent{{Type: llm.SEToolInputJSON, BlockIndex: s.block(ev.OutputIndex), Text: ev.Delta}}, nil

	case "response.output_item.done":
		return []llm.StreamEvent{{Type: llm.SEBlockStop, BlockIndex: s.block(ev.OutputIndex)}}, nil

	case "response.completed", "response.incomplete", "response.failed":
		return []llm.StreamEvent{
			{Type: llm.SEMessageDelta, StopReason: mapResponseStop(ev.Type, ev.Response), Usage: usageFromResponse(ev.Response)},
			{Type: llm.SEMessageStop},
		}, nil

	case "error":
		return nil, fmt.Errorf("openai: responses 流错误 %s %s", ev.Code, ev.Message)

	case "response.in_progress", "response.queued",
		"response.content_part.added", "response.content_part.done",
		"response.output_text.done", "response.refusal.done",
		"response.function_call_arguments.done",
		"response.reasoning_summary_part.added", "response.reasoning_summary_part.done",
		"response.reasoning_summary_text.delta", "response.reasoning_summary_text.done",
		"response.reasoning_text.delta", "response.reasoning_text.done",
		"response.output_text.annotation.added":
		return nil, nil

	default:
		return nil, fmt.Errorf("openai: 未处理的 responses 流事件 %q", ev.Type)
	}
}

func (s *responseSynth) abort() []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonAborted},
		{Type: llm.SEMessageStop},
	}
}

// 换算 responses 用量到中性计费桶，输入扣除缓存命中部分
func usageFromResponse(r responses.Response) llm.Usage {
	cached := int(r.Usage.InputTokensDetails.CachedTokens)
	return llm.Usage{
		InputTokens:      int(r.Usage.InputTokens) - cached,
		OutputTokens:     int(r.Usage.OutputTokens),
		CacheReadTokens:  cached,
		CacheWriteTokens: 0,
	}
}

// 推断终止原因，failed 与 incomplete 直判，正常结束时有工具调用即 toolUse 否则 stop
func mapResponseStop(eventType string, r responses.Response) llm.StopReason {
	switch eventType {
	case "response.failed":
		return llm.StopReasonError
	case "response.incomplete":
		return llm.StopReasonLength
	default:
		for _, item := range r.Output {
			if item.Type == "function_call" {
				return llm.StopReasonToolUse
			}
		}
		return llm.StopReasonStop
	}
}
