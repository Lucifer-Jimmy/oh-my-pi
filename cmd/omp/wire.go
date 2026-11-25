package main

import (
	"encoding/json"
	"io"
	"sync"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const wireVersion = 1

type wireEventBody struct {
	Kind       agent.EventKind     `json:"kind"`
	Seq        int64               `json:"seq"`
	Timestamp  int64               `json:"timestamp,omitempty"`
	Text       string              `json:"text,omitempty"`
	ToolUseID  string              `json:"tool_use_id,omitempty"`
	ToolName   string              `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage     `json:"tool_input,omitempty"`
	Approval   *wireApproval       `json:"approval,omitempty"`
	ToolResult *wireToolResult     `json:"tool_result,omitempty"`
	Usage      *llm.Usage          `json:"usage,omitempty"`
	StopReason llm.StopReason      `json:"stop_reason,omitempty"`
	DurationMs int64               `json:"duration_ms,omitempty"`
	Message    string              `json:"message,omitempty"`
	Reason     agent.StopReason    `json:"reason,omitempty"`
	Err        string              `json:"err,omitempty"`
	Turns      int                 `json:"turns,omitempty"`
	Compaction *session.Compaction `json:"compaction,omitempty"`
}

type wireApproval struct {
	Tier        agent.Tier `json:"tier"`
	ForcePrompt bool       `json:"force_prompt,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

type wireToolResult struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

type wireFrame struct {
	V     int             `json:"v"`
	Type  string          `json:"type"`
	Dead  bool            `json:"dead,omitempty"`
	Event *wireEventBody  `json:"event,omitempty"`
	ID    string          `json:"id,omitempty"`
	Text  string          `json:"text,omitempty"`
	Ask   *wireAsk        `json:"ask,omitempty"`
	Call  *wireExtCall    `json:"call,omitempty"`
	Ext   *wireToolResult `json:"tool_result_out,omitempty"`
	State *wireState      `json:"state,omitempty"`
	Usage *llm.Usage      `json:"usage,omitempty"`
	Error string          `json:"error,omitempty"`
}

type wireAsk struct {
	Method      string   `json:"method"`
	Title       string   `json:"title,omitempty"`
	Message     string   `json:"message,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"`
}

type wireExtCall struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input,omitempty"`
}

type wireState struct {
	Todos []wireTodo `json:"todos,omitempty"`
	Tools []wireTool `json:"tools,omitempty"`
}

type wireTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type wireTool struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Input  string `json:"input,omitempty"`
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  bool   `json:"is_error,omitempty"`
}

type wireWriter struct {
	mu     sync.Mutex
	enc    *json.Encoder
	failed error
}

func newWireWriter(w io.Writer) *wireWriter {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return &wireWriter{enc: enc}
}

func (w *wireWriter) send(f wireFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed != nil {
		return w.failed
	}
	f.V = wireVersion
	if err := w.enc.Encode(f); err != nil {
		w.failed = err
	}
	return w.failed
}

func (w *wireWriter) failure() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failed
}

func wireOf(ev agent.Event) wireFrame {
	body := &wireEventBody{
		Kind:       ev.Kind,
		Seq:        ev.Seq,
		Timestamp:  ev.Timestamp,
		Text:       ev.Text,
		ToolUseID:  ev.ToolUseID,
		ToolName:   ev.ToolName,
		ToolInput:  ev.ToolInput,
		Usage:      ev.Usage,
		StopReason: ev.StopReason,
		DurationMs: ev.DurationMs,
		Message:    ev.Message,
		Compaction: ev.Compaction,
	}
	if ev.Approval != nil {
		body.Approval = &wireApproval{Tier: ev.Approval.Tier, ForcePrompt: ev.Approval.ForcePrompt, Reason: ev.Approval.Reason}
	}
	if ev.ToolResult != nil {
		body.ToolResult = &wireToolResult{Text: wireResultText(*ev.ToolResult), IsError: ev.ToolResult.IsError}
	}
	if ev.Result != nil {
		body.Reason = ev.Result.Reason
		body.Err = ev.Result.Err
		body.Turns = ev.Result.Turns
		body.Usage = &ev.Result.Usage
		if ev.Kind == agent.EventResult {
			body.Text = ev.Result.Text
		}
	}
	return wireFrame{Type: "event", Event: body}
}

func wireResultText(r agent.ToolResult) string {
	out := ""
	for _, b := range r.Content {
		if b.Type != "" && b.Type != llm.BlockText {
			continue
		}
		out += b.Text
	}
	return out
}
