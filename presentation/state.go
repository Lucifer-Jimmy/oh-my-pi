package presentation

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type Kind string

const (
	KindUser     Kind = "user"
	KindText     Kind = "text"
	KindThinking Kind = "thinking"
	KindTool     Kind = "tool"
	KindNotice   Kind = "notice"
)

type Status string

const (
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Level string

const (
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

type Item struct {
	ID      int    `json:"id"`
	Kind    Kind   `json:"kind"`
	Text    string `json:"text,omitempty"`
	Done    bool   `json:"done"`
	Level   Level  `json:"level,omitempty"`
	Tool    *Tool  `json:"tool,omitempty"`
	Started int64  `json:"started,omitempty"`
	Ended   int64  `json:"ended,omitempty"`
}

type Tool struct {
	UseID      string          `json:"use_id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input,omitempty"`
	Status     Status          `json:"status"`
	Output     string          `json:"output,omitempty"`
	DurationMs int64           `json:"duration_ms,omitempty"`
	Progress   *Progress       `json:"progress,omitempty"`
}

type Progress struct {
	Message string          `json:"message,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
	Tokens  int             `json:"tokens,omitempty"`
}

type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type ToolProjector func(s *State, input json.RawMessage)

type State struct {
	Items        []Item                   `json:"items"`
	Todos        []TodoItem               `json:"todos,omitempty"`
	Running      bool                     `json:"running"`
	Usage        llm.Usage                `json:"usage"`
	Context      int                      `json:"context"`
	Reason       agent.StopReason         `json:"reason,omitempty"`
	Projectors   map[string]ToolProjector `json:"-"`
	InjectedTags []string                 `json:"-"`
	lastSeq      int64
	nextID       int
}

func (s *State) Submit(text string, at int64) {
	s.add(Item{Kind: KindUser, Text: text, Done: true, Started: at, Ended: at})
	s.Running = true
	s.Reason = ""
	s.lastSeq = 0
}

func (s *State) Notice(level Level, text string, at int64) {
	s.add(Item{Kind: KindNotice, Level: level, Text: text, Done: true, Started: at, Ended: at})
}

func compactionNote(ev agent.Event) string {
	c := ev.Compaction
	if c == nil {
		return ev.Message
	}
	note := fmt.Sprintf("Context compacted: %s → %s tokens", tokens(c.TokensBefore), tokens(c.TokensAfter))
	var done []string
	if c.Elided > 0 {
		done = append(done, fmt.Sprintf("%d tool results elided", c.Elided))
	}
	if c.Summarized > 0 {
		done = append(done, fmt.Sprintf("%d messages summarized", c.Summarized))
	}
	if len(done) > 0 {
		note += " (" + strings.Join(done, ", ") + ")"
	}
	return note
}

func tokens(n int) string { return FormatTokens(n) }

func FormatTokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
}

func (s *State) Reset() {
	*s = State{nextID: s.nextID, Projectors: s.Projectors, InjectedTags: s.InjectedTags}
}

func (s *State) Replay(msgs []llm.Message) {
	for _, m := range msgs {
		at := m.Timestamp
		switch m.Role {
		case llm.RoleUser:
			for _, b := range m.Content {
				switch b.Type {
				case llm.BlockText:
					if text := s.typed(b.Text); text != "" {
						s.add(Item{Kind: KindUser, Text: text, Done: true, Started: at, Ended: at})
					}
				case llm.BlockToolResult:
					s.finishTool(agent.Event{ToolUseID: b.ToolUseID, Timestamp: at, ToolResult: &agent.ToolResult{Content: b.Content, IsError: b.IsError}})
				}
			}
		case llm.RoleAssistant:
			for _, b := range m.Content {
				switch {
				case b.Type == llm.BlockText && b.Text != "":
					s.add(Item{Kind: KindText, Text: b.Text, Done: true, Started: at, Ended: at})
				case b.Type == llm.BlockThinking && b.Thinking != "":
					s.add(Item{Kind: KindThinking, Text: b.Thinking, Done: true, Started: at, Ended: at})
				case b.Type == llm.BlockToolUse:
					s.add(Item{Kind: KindTool, Started: at, Tool: &Tool{UseID: b.ID, Name: b.Name, Input: b.Input, Status: StatusRunning}})
					if p, ok := s.Projectors[b.Name]; ok {
						p(s, b.Input)
					}
				}
			}
			if m.Usage != nil {
				s.Usage.Add(*m.Usage)
				s.Context = m.Usage.Total()
			}
		}
	}
	for i := range s.Items {
		it := &s.Items[i]
		if it.Kind == KindTool && !it.Done {
			it.Done, it.Ended = true, it.Started
			it.Tool.Status = StatusCancelled
		}
	}
}

func (s *State) typed(text string) string {
	for {
		rest, ok := s.stripInjected(strings.TrimLeft(text, " \t\r\n"))
		if !ok {
			return strings.TrimSpace(text)
		}
		text = rest
	}
}

func (s *State) stripInjected(text string) (string, bool) {
	for _, tag := range s.InjectedTags {
		open := "<" + tag
		if !strings.HasPrefix(text, open) || len(text) == len(open) {
			continue
		}
		if c := text[len(open)]; c != '>' && c != ' ' && c != '\n' {
			continue
		}
		closing := "</" + tag + ">"
		end := strings.Index(text, closing)
		if end < 0 {
			continue
		}
		return text[end+len(closing):], true
	}
	return "", false
}

func (s *State) Apply(ev agent.Event) {
	if ev.Seq != 0 {
		if ev.Seq <= s.lastSeq {
			return
		}
		s.lastSeq = ev.Seq
	}
	switch ev.Kind {
	case agent.EventTextDelta:
		s.stream(KindText, ev.Text, ev.Timestamp)
	case agent.EventThinkingDelta:
		s.stream(KindThinking, ev.Text, ev.Timestamp)
	case agent.EventToolUse:
		s.closeStream(ev.Timestamp)
		s.add(Item{Kind: KindTool, Started: ev.Timestamp, Tool: &Tool{
			UseID: ev.ToolUseID, Name: ev.ToolName, Input: ev.ToolInput, Status: StatusRunning,
		}})
		if p, ok := s.Projectors[ev.ToolName]; ok {
			p(s, ev.ToolInput)
		}
	case agent.EventToolResult:
		s.finishTool(ev)
	case agent.EventProgress:
		s.progress(ev)
	case agent.EventUsage:
		s.closeStream(ev.Timestamp)
		if ev.Usage != nil {
			s.Usage.Add(*ev.Usage)
			u := ev.Usage
			s.Context = u.Total()
		}
	case agent.EventNotification:
		s.closeStream(ev.Timestamp)
		if ev.Message != "" {
			s.Notice(LevelInfo, ev.Message, ev.Timestamp)
		}
	case agent.EventCompaction:
		s.closeStream(ev.Timestamp)
		s.Notice(LevelInfo, compactionNote(ev), ev.Timestamp)
	case agent.EventResult:
		s.finish(ev)
	}
}

func (s *State) add(it Item) {
	s.nextID++
	it.ID = s.nextID
	s.Items = append(s.Items, it)
}

func (s *State) stream(kind Kind, text string, at int64) {
	if n := len(s.Items); n > 0 {
		last := &s.Items[n-1]
		if last.Kind == kind && !last.Done {
			last.Text += text
			return
		}
	}
	s.closeStream(at)
	s.add(Item{Kind: kind, Text: text, Started: at})
}

func (s *State) closeStream(at int64) {
	for i := len(s.Items) - 1; i >= 0; i-- {
		it := &s.Items[i]
		if it.Kind != KindText && it.Kind != KindThinking {
			continue
		}
		if !it.Done {
			it.Done, it.Ended = true, at
		}
		return
	}
}

func (s *State) progress(ev agent.Event) {
	if ev.ToolUseID == "" {
		return
	}
	for i := len(s.Items) - 1; i >= 0; i-- {
		it := &s.Items[i]
		if it.Kind != KindTool || it.Done || it.Tool.UseID != ev.ToolUseID {
			continue
		}
		p := &Progress{Message: ev.Message, Name: ev.ToolName, Input: ev.ToolInput}
		if ev.Usage != nil {
			p.Tokens = ev.Usage.Total()
		}
		it.Tool.Progress = p
		return
	}
}

func (s *State) finishTool(ev agent.Event) {
	for i := len(s.Items) - 1; i >= 0; i-- {
		it := &s.Items[i]
		if it.Kind != KindTool || it.Done || it.Tool.UseID != ev.ToolUseID {
			continue
		}
		it.Done, it.Ended = true, ev.Timestamp
		it.Tool.Status = StatusDone
		it.Tool.DurationMs = ev.DurationMs
		it.Tool.Progress = nil
		if ev.ToolResult != nil {
			it.Tool.Output = resultText(ev.ToolResult.Content)
			if ev.ToolResult.IsError {
				it.Tool.Status = StatusFailed
			}
		}
		return
	}
}

func (s *State) finish(ev agent.Event) {
	s.closeStream(ev.Timestamp)
	for i := range s.Items {
		it := &s.Items[i]
		if it.Kind == KindTool && !it.Done {
			it.Done, it.Ended = true, ev.Timestamp
			it.Tool.Status = StatusCancelled
			it.Tool.Progress = nil
		}
	}
	s.Running = false
	if ev.Result == nil {
		return
	}
	s.Reason = ev.Result.Reason
	switch ev.Result.Reason {
	case agent.ReasonCompleted:
	case agent.ReasonAborted:
		s.Notice(LevelWarn, "Interrupted", ev.Timestamp)
	case agent.ReasonMaxTurns:
		s.Notice(LevelWarn, "Stopped: turn limit reached", ev.Timestamp)
	default:
		msg := ev.Result.Err
		if msg == "" {
			msg = "Stopped: " + string(ev.Result.Reason)
		}
		s.Notice(LevelError, msg, ev.Timestamp)
	}
}

func resultText(blocks []llm.ContentBlock) string {
	var parts []string
	for _, b := range blocks {
		if b.Type == llm.BlockText && b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}
