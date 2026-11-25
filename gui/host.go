package gui

import (
	"context"
	"fmt"
	"sync"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/markdown"
)

type Host struct {
	mu      sync.Mutex
	pending map[string]*waiting
	order   []string
	next    int
	send    func(frame) error
}

type waiting struct {
	ask   askFrame
	reply chan agent.AskResponse
}

func NewHost() *Host {
	return &Host{pending: make(map[string]*waiting)}
}

func (h *Host) SetSender(send func(frame) error) {
	h.mu.Lock()
	h.send = send
	h.mu.Unlock()
}

func (h *Host) Ask(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	h.mu.Lock()
	h.next++
	id := fmt.Sprintf("ask-%d", h.next)
	message := req.Message
	if req.Detail != "" {
		message += "\n\n" + req.Detail
	}
	w := &waiting{
		ask: askFrame{
			ID:          id,
			Method:      string(req.Method),
			Title:       req.Title,
			Message:     message,
			Placeholder: req.Placeholder,
			Options:     append([]string(nil), req.Options...),
			Tool:        req.Tool,
			Input:       req.Input,
		},
		reply: make(chan agent.AskResponse, 1),
	}
	if req.Tool == "exit_plan_mode" {
		w.ask.HTML = markdown.HTML(req.Message)
	}
	h.pending[id] = w
	h.order = append(h.order, id)
	send := h.send
	h.mu.Unlock()
	defer h.forget(id)
	if send == nil {
		return agent.AskResponse{}, agent.ErrNoUI
	}
	if err := send(frame{Type: "ask", Ask: &w.ask}); err != nil {
		return agent.AskResponse{}, err
	}
	select {
	case <-ctx.Done():
		return agent.AskResponse{Cancelled: true}, nil
	case resp := <-w.reply:
		return resp, nil
	}
}

func (h *Host) Answer(id string, resp agent.AskResponse) bool {
	h.mu.Lock()
	w, ok := h.pending[id]
	if ok {
		delete(h.pending, id)
	}
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case w.reply <- resp:
	default:
	}
	return true
}

func (h *Host) CancelAll() {
	h.mu.Lock()
	open := make([]*waiting, 0, len(h.pending))
	for _, w := range h.pending {
		open = append(open, w)
	}
	h.pending = make(map[string]*waiting)
	h.order = nil
	h.mu.Unlock()
	for _, w := range open {
		select {
		case w.reply <- agent.AskResponse{Cancelled: true}:
		default:
		}
	}
}

func (h *Host) Pending() []askFrame {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]askFrame, 0, len(h.pending))
	for _, id := range h.order {
		if w, ok := h.pending[id]; ok {
			out = append(out, w.ask)
		}
	}
	return out
}

func (h *Host) forget(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	for i, other := range h.order {
		if other == id {
			h.order = append(h.order[:i], h.order[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
}
