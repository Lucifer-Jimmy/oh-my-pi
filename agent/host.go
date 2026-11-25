package agent

import (
	"context"
	"encoding/json"
	"errors"
)

var ErrNoUI = errors.New("agent: interactive mode required")

// 超时和中途取消都交给 ctx，发生时 Ask 返回 ctx 的错误
type Host interface {
	Ask(ctx context.Context, req AskRequest) (AskResponse, error)
}

type AskMethod string

const (
	AskSelect  AskMethod = "select"
	AskConfirm AskMethod = "confirm"
	AskInput   AskMethod = "input"
)

// Title 三种方法都用，select 另用 Options，confirm 另用 Message，input 另用 Placeholder
type AskRequest struct {
	Method      AskMethod
	Title       string
	Message     string
	Placeholder string
	Options     []string
	Tool        string
	Input       json.RawMessage
	Detail      string
}

// select 和 input 的答案放在 Value，confirm 的答案放在 Confirmed，用户关掉对话框时 Cancelled 为真
type AskResponse struct {
	Value     string
	Confirmed bool
	Cancelled bool
}
