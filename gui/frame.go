package gui

import (
	"encoding/json"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
)

const version = 1

type frame struct {
	V     int         `json:"v"`
	Type  string      `json:"type"`
	Ask   *askFrame   `json:"ask,omitempty"`
	State *stateFrame `json:"state,omitempty"`
	Error string      `json:"error,omitempty"`
}

type askFrame struct {
	ID          string          `json:"id"`
	Method      string          `json:"method"`
	Title       string          `json:"title,omitempty"`
	Message     string          `json:"message,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Options     []string        `json:"options,omitempty"`
	Tool        string          `json:"tool,omitempty"`
	Input       json.RawMessage `json:"input,omitempty"`
	HTML        string          `json:"html,omitempty"`
}

type stateFrame struct {
	From    int                     `json:"from"`
	Items   []item                  `json:"items"`
	Running bool                    `json:"running"`
	Usage   llm.Usage               `json:"usage"`
	Context int                     `json:"context"`
	Reason  agent.StopReason        `json:"reason,omitempty"`
	Todos   []presentation.TodoItem `json:"todos,omitempty"`
	Asks    []askFrame              `json:"asks,omitempty"`
	Model   string                  `json:"model,omitempty"`
	Mode    string                  `json:"mode,omitempty"`
	CWD     string                  `json:"cwd,omitempty"`
	Window  int                     `json:"window,omitempty"`
}

type item struct {
	presentation.Item
	HTML string `json:"html,omitempty"`
}

type command struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	ID        string `json:"id,omitempty"`
	Value     string `json:"value,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
	Cancelled bool   `json:"cancelled,omitempty"`
	Mode      string `json:"mode,omitempty"`
}

type reply struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}
