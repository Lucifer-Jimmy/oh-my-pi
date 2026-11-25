package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

var ErrNoToolHost = errors.New("this tool runs on the host, and no host is attached")

type ExtRequest struct {
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type ExtResult struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

type Ext interface {
	CallTool(ctx context.Context, req ExtRequest) (ExtResult, error)
}

type RemoteTool struct {
	ToolName  string
	ToolDesc  string
	InputJSON json.RawMessage
	ReadOnly  bool
	WriteTier bool
	OpenWorld bool
}

func (r *RemoteTool) Name() string            { return r.ToolName }
func (r *RemoteTool) Description() string     { return r.ToolDesc }
func (r *RemoteTool) Schema() json.RawMessage { return r.InputJSON }

func (r *RemoteTool) IsReadOnly() bool                       { return r.ReadOnly }
func (r *RemoteTool) IsConcurrencySafe(json.RawMessage) bool { return r.ReadOnly }

func (r *RemoteTool) Approval(json.RawMessage) Approval {
	if r.WriteTier {
		return Approval{Tier: TierWrite}
	}
	return Approval{Tier: TierRead}
}

func (r *RemoteTool) Metadata(json.RawMessage) Metadata {
	return Metadata{OpenWorld: r.OpenWorld}
}

type toolContextExt interface {
	CallTool(ctx context.Context, req ExtRequest) (ExtResult, error)
}

func (r *RemoteTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	ext, ok := tc.(toolContextExt)
	if !ok {
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(ErrNoToolHost.Error())}, IsError: true}
	}
	res, err := ext.CallTool(ctx, ExtRequest{Name: r.ToolName, Input: args})
	if err != nil {
		msg := "external tool " + r.ToolName + ": " + err.Error()
		return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(msg)}, IsError: true}
	}
	text := res.Text
	if text == "" {
		text = "(the host returned no output)"
	}
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}, IsError: res.IsError}
}
