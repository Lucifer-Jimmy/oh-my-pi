package openai

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/openai/openai-go/v3"
)

const toolFailedNoOutput = "Tool failed with no output."

func convertMessages(system []string, msgs []llm.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion
	if s := joinSystem(system); s != "" {
		out = append(out, openai.SystemMessage(s))
	}
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser:
			ums, err := userMessages(m)
			if err != nil {
				return nil, err
			}
			out = append(out, ums...)
		case llm.RoleAssistant:
			am, ok, err := assistantMessage(m)
			if err != nil {
				return nil, err
			}
			if ok {
				out = append(out, am)
			}
		default:
			return nil, fmt.Errorf("openai: 未知 Role %q", m.Role)
		}
	}
	return out, nil
}

func joinSystem(system []string) string {
	var parts []string
	for _, s := range system {
		if strings.TrimSpace(s) != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func userMessages(m llm.Message) ([]openai.ChatCompletionMessageParamUnion, error) {
	var out []openai.ChatCompletionMessageParamUnion
	var text strings.Builder
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			text.WriteString(b.Text)
		case llm.BlockToolResult:
			out = append(out, toolMessage(b))
		default:
			return nil, fmt.Errorf("openai: user 轮出现 %s 块", b.Type)
		}
	}
	if strings.TrimSpace(text.String()) != "" {
		out = append(out, openai.UserMessage(text.String()))
	}
	return out, nil
}

func assistantMessage(m llm.Message) (openai.ChatCompletionMessageParamUnion, bool, error) {
	var text strings.Builder
	var toolCalls []openai.ChatCompletionMessageToolCallUnionParam
	for _, b := range m.Content {
		switch b.Type {
		case llm.BlockText:
			text.WriteString(b.Text)
		case llm.BlockThinking, llm.BlockRedactedThinking:
		case llm.BlockToolUse:
			args := strings.TrimSpace(string(b.Input))
			if args == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
				OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
					ID: b.ID,
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      b.Name,
						Arguments: args,
					},
				},
			})
		case llm.BlockToolResult:
			return openai.ChatCompletionMessageParamUnion{}, false,
				fmt.Errorf("openai: assistant 轮出现 tool_result 块")
		default:
			return openai.ChatCompletionMessageParamUnion{}, false,
				fmt.Errorf("openai: 块 %q 的回放未实现", b.Type)
		}
	}
	if text.Len() == 0 && len(toolCalls) == 0 {
		return openai.ChatCompletionMessageParamUnion{}, false, nil
	}
	am := &openai.ChatCompletionAssistantMessageParam{ToolCalls: toolCalls}
	if text.Len() > 0 {
		am.Content.OfString = openai.String(text.String())
	}
	return openai.ChatCompletionMessageParamUnion{OfAssistant: am}, true, nil
}

func toolMessage(b llm.ContentBlock) openai.ChatCompletionMessageParamUnion {
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
	return openai.ToolMessage(content, b.ToolUseID)
}

func convertTools(tools []llm.ToolSchema) ([]openai.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		fn := openai.FunctionDefinitionParam{Name: t.Name}
		if t.Description != "" {
			fn.Description = openai.String(t.Description)
		}
		if len(t.InputSchema) > 0 {
			var params openai.FunctionParameters
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("openai: 工具 %q 的 InputSchema 解析失败: %w", t.Name, err)
			}
			fn.Parameters = params
		}
		out = append(out, openai.ChatCompletionFunctionTool(fn))
	}
	return out, nil
}

type scalarKind int

const (
	scalarContent scalarKind = iota
	scalarRefusal
)

type completionSynth struct {
	started     bool
	next        int
	tools       map[int64]int
	toolIDs     map[int64]string
	scalarOpen  bool
	scalarKind  scalarKind
	scalarIndex int
	openTools   []int
	stop        llm.StopReason
	usage       llm.Usage
	finished    bool
}

func newCompletionSynth() *completionSynth {
	return &completionSynth{tools: map[int64]int{}, toolIDs: map[int64]string{}}
}

func (s *completionSynth) feed(chunk openai.ChatCompletionChunk) ([]llm.StreamEvent, error) {
	var evs []llm.StreamEvent
	if !s.started {
		s.started = true
		evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStart})
	}
	if chunk.JSON.Usage.Valid() {
		s.usage = usageFromCompletion(chunk.Usage)
	}
	if len(chunk.Choices) == 0 {
		return evs, nil
	}
	choice := chunk.Choices[0]
	if choice.Index != 0 {
		return nil, fmt.Errorf("openai: 不支持的 choice index %d", choice.Index)
	}
	d := choice.Delta
	if d.JSON.FunctionCall.Valid() {
		return nil, fmt.Errorf("openai: 不支持废弃的 function_call 通道")
	}
	if s.finished && (d.Content != "" || d.Refusal != "" || len(d.ToolCalls) > 0) {
		return nil, fmt.Errorf("openai: finish 之后仍收到增量数据")
	}
	if d.Content != "" {
		evs = append(evs, s.scalar(scalarContent, d.Content)...)
	}
	if d.Refusal != "" {
		evs = append(evs, s.scalar(scalarRefusal, d.Refusal)...)
	}
	if len(d.ToolCalls) > 0 {
		if s.scalarOpen {
			evs = append(evs, llm.StreamEvent{Type: llm.SEBlockStop, BlockIndex: s.scalarIndex})
			s.scalarOpen = false
		}
		for _, tc := range d.ToolCalls {
			tev, err := s.toolFragment(tc)
			if err != nil {
				return nil, err
			}
			evs = append(evs, tev...)
		}
	}
	if choice.FinishReason != "" {
		evs = append(evs, s.closeAll()...)
		s.stop = mapCompletionStop(choice.FinishReason)
		s.finished = true
	}
	return evs, nil
}

func (s *completionSynth) scalar(kind scalarKind, text string) []llm.StreamEvent {
	var evs []llm.StreamEvent
	if s.scalarOpen && s.scalarKind != kind {
		evs = append(evs, llm.StreamEvent{Type: llm.SEBlockStop, BlockIndex: s.scalarIndex})
		s.scalarOpen = false
	}
	if !s.scalarOpen {
		s.scalarIndex = s.next
		s.next++
		s.scalarKind = kind
		s.scalarOpen = true
		evs = append(evs, llm.StreamEvent{Type: llm.SEBlockStart, BlockIndex: s.scalarIndex, Block: llm.ContentBlock{Type: llm.BlockText}})
	}
	evs = append(evs, llm.StreamEvent{Type: llm.SETextDelta, BlockIndex: s.scalarIndex, Text: text})
	return evs
}

func (s *completionSynth) toolFragment(tc openai.ChatCompletionChunkChoiceDeltaToolCall) ([]llm.StreamEvent, error) {
	if tc.Type != "" && tc.Type != "function" {
		return nil, fmt.Errorf("openai: 不支持的工具类型 %q", tc.Type)
	}
	var evs []llm.StreamEvent
	bi, seen := s.tools[tc.Index]
	if !seen {
		if tc.ID == "" || tc.Function.Name == "" {
			return nil, fmt.Errorf("openai: 工具调用分片首现缺少 id 或 name, wire index %d", tc.Index)
		}
		bi = s.next
		s.next++
		s.tools[tc.Index] = bi
		s.toolIDs[tc.Index] = tc.ID
		s.openTools = append(s.openTools, bi)
		evs = append(evs, llm.StreamEvent{
			Type:       llm.SEBlockStart,
			BlockIndex: bi,
			Block:      llm.ContentBlock{Type: llm.BlockToolUse, ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage("{}")},
		})
	} else if tc.ID != "" && tc.ID != s.toolIDs[tc.Index] {
		return nil, fmt.Errorf("openai: 工具调用 wire index %d 的 id 前后不一致", tc.Index)
	}
	if tc.Function.Arguments != "" {
		evs = append(evs, llm.StreamEvent{Type: llm.SEToolInputJSON, BlockIndex: bi, Text: tc.Function.Arguments})
	}
	return evs, nil
}

func (s *completionSynth) closeAll() []llm.StreamEvent {
	var idx []int
	if s.scalarOpen {
		idx = append(idx, s.scalarIndex)
		s.scalarOpen = false
	}
	idx = append(idx, s.openTools...)
	s.openTools = nil
	sort.Ints(idx)
	evs := make([]llm.StreamEvent, 0, len(idx))
	for _, i := range idx {
		evs = append(evs, llm.StreamEvent{Type: llm.SEBlockStop, BlockIndex: i})
	}
	return evs
}

func (s *completionSynth) abort() []llm.StreamEvent {
	evs := s.closeAll()
	evs = append(evs, llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonAborted})
	evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStop})
	return evs
}

func (s *completionSynth) finalize() []llm.StreamEvent {
	var evs []llm.StreamEvent
	if !s.finished {
		evs = append(evs, s.closeAll()...)
		if s.stop == "" {
			s.stop = llm.StopReasonError
		}
	}
	evs = append(evs, llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: s.stop, Usage: s.usage})
	evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStop})
	return evs
}

func usageFromCompletion(u openai.CompletionUsage) llm.Usage {
	cached := int(u.PromptTokensDetails.CachedTokens)
	return llm.Usage{
		InputTokens:      int(u.PromptTokens) - cached,
		OutputTokens:     int(u.CompletionTokens),
		CacheReadTokens:  cached,
		CacheWriteTokens: 0,
	}
}

func mapCompletionStop(reason string) llm.StopReason {
	switch reason {
	case "stop":
		return llm.StopReasonStop
	case "length":
		return llm.StopReasonLength
	case "tool_calls", "function_call":
		return llm.StopReasonToolUse
	default:
		return llm.StopReasonError
	}
}
