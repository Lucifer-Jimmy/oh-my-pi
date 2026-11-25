package tui

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

const maxPreviewFile = 256 << 10

type Host struct {
	requests chan *askRequest
}

type askRequest struct {
	ctx   context.Context
	req   agent.AskRequest
	reply chan agent.AskResponse

	target string
	note   string
	lines  []diffLine
}

func NewHost() *Host {
	return &Host{requests: make(chan *askRequest)}
}

func (h *Host) Ask(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	switch req.Method {
	case agent.AskConfirm, agent.AskInput:
	case agent.AskSelect:
		if len(req.Options) == 0 {
			return agent.AskResponse{}, agent.ErrNoUI
		}
	default:
		return agent.AskResponse{}, agent.ErrNoUI
	}
	r := &askRequest{ctx: ctx, req: req, reply: make(chan agent.AskResponse, 1)}
	select {
	case h.requests <- r:
	case <-ctx.Done():
		return agent.AskResponse{Cancelled: true}, nil
	}
	select {
	case resp := <-r.reply:
		return resp, nil
	case <-ctx.Done():
		return agent.AskResponse{Cancelled: true}, nil
	}
}

func (r *askRequest) answer(resp agent.AskResponse) {
	select {
	case r.reply <- resp:
	default:
	}
}

func (r *askRequest) prepare(cwd string) {
	switch r.req.Tool {
	case "edit":
		var a editArgs
		if json.Unmarshal(r.req.Input, &a) != nil {
			return
		}
		r.target = a.Path
		r.lines = collapse(lineDiff(a.OldString, a.NewString), diffContext)
		if a.ReplaceAll {
			r.note = "replaces every occurrence"
		}
	case "write":
		var a writeArgs
		if json.Unmarshal(r.req.Input, &a) != nil {
			return
		}
		r.target = a.Path
		if old, ok := readForPreview(cwd, a.Path); ok {
			r.note = "overwrites the file"
			r.lines = collapse(lineDiff(old, a.Content), diffContext)
			return
		}
		r.note = "new file"
		for _, l := range diffInput(a.Content) {
			r.lines = append(r.lines, diffLine{'+', l})
		}
	}
}

func readForPreview(cwd, path string) (string, bool) {
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxPreviewFile {
		return "", false
	}
	b, err := os.ReadFile(path)
	if err != nil || strings.IndexByte(string(b), 0) >= 0 {
		return "", false
	}
	return strings.TrimPrefix(string(b), "\ufeff"), true
}
