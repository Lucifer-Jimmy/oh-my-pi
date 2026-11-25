package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Accumulator struct {
	message    Message
	StopReason StopReason
	Usage      Usage
}

// 返回空累积器
func NewAccumulator() *Accumulator {
	return &Accumulator{message: Message{Role: RoleAssistant}}
}

func (a *Accumulator) Add(ev StreamEvent) error {
	switch ev.Type {
	case SEMessageStart:
		a.message = Message{Role: RoleAssistant}
		a.StopReason = ""
		a.Usage = ev.Usage
	case SEBlockStart:
		if ev.BlockIndex != len(a.message.Content) {
			return fmt.Errorf("ai: block_start BlockIndex %d, want %d", ev.BlockIndex, len(a.message.Content))
		}
		a.message.Content = append(a.message.Content, ev.Block)
	case SETextDelta:
		b, err := a.block(ev)
		if err != nil {
			return err
		}
		b.Text += ev.Text
	case SEThinkingDelta:
		b, err := a.block(ev)
		if err != nil {
			return err
		}
		b.Thinking += ev.Text
	case SEThinkingSignature:
		b, err := a.block(ev)
		if err != nil {
			return err
		}
		b.Signature += ev.Text
	case SEToolInputJSON:
		b, err := a.block(ev)
		if err != nil {
			return err
		}
		if string(b.Input) == "{}" {
			b.Input = []byte(ev.Text)
		} else {
			b.Input = append(b.Input, ev.Text...)
		}
	case SEBlockStop:
		if ev.BlockIndex < 0 || ev.BlockIndex >= len(a.message.Content) {
			return fmt.Errorf("ai: block_stop BlockIndex %d 越界", ev.BlockIndex)
		}
	case SEMessageDelta, SEMessageStop:
		if ev.StopReason != "" {
			a.StopReason = ev.StopReason
		}
		a.Usage.MergeEcho(ev.Usage)
	}
	return nil
}

func (a *Accumulator) block(ev StreamEvent) (*ContentBlock, error) {
	if ev.BlockIndex < 0 || ev.BlockIndex >= len(a.message.Content) {
		return nil, fmt.Errorf("ai: %s BlockIndex %d 越界", ev.Type, ev.BlockIndex)
	}
	return &a.message.Content[ev.BlockIndex], nil
}

// Message 返回组装好的 Assistant 消息
func (a *Accumulator) Message() Message {
	out := make([]ContentBlock, len(a.message.Content))
	copy(out, a.message.Content)
	for i, b := range out {
		if b.Type == BlockToolUse && !isObject(b.Input) {
			out[i].Input = json.RawMessage("{}")
		}
	}
	return Message{Role: a.message.Role, Content: out}
}

// Block 返回指定块当前内容的副本 供流式派发在块结束时立即取用
func (a *Accumulator) Block(i int) (ContentBlock, bool) {
	if i < 0 || i >= len(a.message.Content) {
		return ContentBlock{}, false
	}
	b := a.message.Content[i]
	if b.Input != nil {
		b.Input = append(json.RawMessage(nil), b.Input...)
	}
	if b.Type == BlockToolUse && len(strings.TrimSpace(string(b.Input))) == 0 {
		b.Input = json.RawMessage("{}")
	}
	return b, true
}

// 顶层必须是 JSON 对象
func isObject(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return strings.HasPrefix(s, "{") && json.Valid([]byte(s))
}
