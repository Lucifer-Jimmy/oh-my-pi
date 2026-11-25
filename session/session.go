package session

import "github.com/Lucifer-Jimmy/oh-my-pi/llm"

type Session interface {
	Append(m llm.Message)
	Messages() []llm.Message
	AppendCompaction(c Compaction)
	LastCompaction() (Compaction, bool)
}

type Trigger string

const (
	TriggerAuto     Trigger = "auto"
	TriggerOverflow Trigger = "overflow"
	TriggerManual   Trigger = "manual"
)

type Compaction struct {
	Trigger      Trigger   `json:"trigger"`
	MessageCount int       `json:"message_count"`
	ElideBefore  int       `json:"elide_before"`
	FirstKept    int       `json:"first_kept"`
	Summary      string    `json:"summary,omitempty"`
	Elided       int       `json:"elided"`
	Summarized   int       `json:"summarized"`
	TokensBefore int       `json:"tokens_before"`
	TokensAfter  int       `json:"tokens_after"`
	Usage        llm.Usage `json:"usage"`
	Timestamp    int64     `json:"timestamp,omitempty"`
}

type Memory struct {
	messages    []llm.Message
	compactions []Compaction
}

func NewMemory(seed []llm.Message) *Memory {
	m := &Memory{messages: make([]llm.Message, len(seed))}
	copy(m.messages, seed)
	return m
}

func (m *Memory) Append(msg llm.Message) { m.messages = append(m.messages, msg) }

func (m *Memory) Messages() []llm.Message {
	out := make([]llm.Message, len(m.messages))
	copy(out, m.messages)
	return out
}

func (m *Memory) AppendCompaction(c Compaction) { m.compactions = append(m.compactions, c) }

func (m *Memory) LastCompaction() (Compaction, bool) {
	if len(m.compactions) == 0 {
		return Compaction{}, false
	}
	return m.compactions[len(m.compactions)-1], true
}
