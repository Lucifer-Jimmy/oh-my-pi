package compaction

import (
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const messageOverhead = 4

func estimateText(s string) int { return (len(s) + 3) / 4 }

func estimateBlocks(blocks []llm.ContentBlock) int {
	n := 0
	for _, b := range blocks {
		switch b.Type {
		case llm.BlockText:
			n += estimateText(b.Text)
		case llm.BlockThinking:
			n += estimateText(b.Thinking)
		case llm.BlockToolUse:
			n += estimateText(b.Name) + estimateText(string(b.Input))
		case llm.BlockToolResult:
			n += messageOverhead + estimateBlocks(b.Content)
		case llm.BlockRedactedThinking:
			n += estimateText(b.Data)
		default:
			n += estimateText(string(b.Raw))
		}
	}
	return n
}

func estimateMessage(m llm.Message) int { return messageOverhead + estimateBlocks(m.Content) }

func estimateMessages(msgs []llm.Message) int {
	n := 0
	for _, m := range msgs {
		n += estimateMessage(m)
	}
	return n
}

func estimateFixed(req llm.CompletionRequest) int {
	n := 0
	for _, s := range req.System {
		n += estimateText(s)
	}
	for _, t := range req.Tools {
		n += estimateText(t.Name) + estimateText(t.Description) + estimateText(string(t.InputSchema))
	}
	return n
}

func measure(req llm.CompletionRequest, last session.Compaction, view []llm.Message) int {
	estimate := estimateFixed(req) + estimateMessages(view)
	msgs := req.Messages
	for i := len(msgs) - 1; i >= last.MessageCount; i-- {
		m := msgs[i]
		if m.Role != llm.RoleAssistant || m.Usage == nil {
			continue
		}
		if reported := m.Usage.Total(); reported > 0 {
			return max(reported+estimateMessages(msgs[i+1:]), estimate)
		}
		break
	}
	return estimate
}
