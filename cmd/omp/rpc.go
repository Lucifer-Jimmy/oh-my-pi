package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type rpcHost struct {
	mu       sync.Mutex
	pending  map[string]chan agent.AskResponse
	toolWait map[string]chan agent.ExtResult
	next     int
	send     func(wireFrame) error
}

func newRPCHost() *rpcHost {
	return &rpcHost{pending: make(map[string]chan agent.AskResponse), toolWait: make(map[string]chan agent.ExtResult)}
}

func (h *rpcHost) SetSender(send func(wireFrame) error) {
	h.mu.Lock()
	h.send = send
	h.mu.Unlock()
}

func (h *rpcHost) Ask(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	h.mu.Lock()
	h.next++
	id := fmt.Sprintf("ask-%d", h.next)
	ch := make(chan agent.AskResponse, 1)
	h.pending[id] = ch
	send := h.send
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.pending, id)
		h.mu.Unlock()
	}()
	if send == nil {
		return agent.AskResponse{}, agent.ErrNoUI
	}
	message := req.Message
	if req.Detail != "" {
		message += "\n\n" + req.Detail
	}
	f := wireFrame{Type: "ask", ID: id, Ask: &wireAsk{
		Method:      string(req.Method),
		Title:       req.Title,
		Message:     message,
		Placeholder: req.Placeholder,
		Options:     req.Options,
	}}
	if err := send(f); err != nil {
		return agent.AskResponse{}, err
	}
	select {
	case <-ctx.Done():
		return agent.AskResponse{}, ctx.Err()
	case resp := <-ch:
		return resp, nil
	}
}

func (h *rpcHost) Answer(id string, resp agent.AskResponse) bool {
	h.mu.Lock()
	ch, ok := h.pending[id]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- resp:
	default:
	}
	return true
}

func (h *rpcHost) CallTool(ctx context.Context, req agent.ExtRequest) (agent.ExtResult, error) {
	h.mu.Lock()
	h.next++
	id := fmt.Sprintf("tool-%d", h.next)
	ch := make(chan agent.ExtResult, 1)
	h.toolWait[id] = ch
	send := h.send
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.toolWait, id)
		h.mu.Unlock()
	}()
	if send == nil {
		return agent.ExtResult{}, agent.ErrNoToolHost
	}
	if err := send(wireFrame{Type: "tool_call", ID: id, Call: &wireExtCall{Name: req.Name, Input: req.Input}}); err != nil {
		return agent.ExtResult{}, err
	}
	select {
	case <-ctx.Done():
		return agent.ExtResult{}, ctx.Err()
	case res := <-ch:
		return res, nil
	}
}

func (h *rpcHost) ResolveTool(id, text string, isErr bool) bool {
	h.mu.Lock()
	ch, ok := h.toolWait[id]
	h.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- agent.ExtResult{Text: text, IsError: isErr}:
	default:
	}
	return true
}

type rpcCommand struct {
	Type      string   `json:"type"`
	IsError   bool     `json:"is_error,omitempty"`
	Text      string   `json:"text,omitempty"`
	ID        string   `json:"id,omitempty"`
	Value     string   `json:"value,omitempty"`
	Confirmed bool     `json:"confirmed,omitempty"`
	Cancelled bool     `json:"cancelled,omitempty"`
	Options   []string `json:"options,omitempty"`
}

func runRPC(ctx context.Context, host *rpcHost, run agent.Runner, sess session.Session, prompts []string, stdin io.Reader, stdout, stderr io.Writer) int {
	w := newWireWriter(stdout)
	host.SetSender(w.send)
	if err := w.send(wireFrame{Type: "ready"}); err != nil {
		fmt.Fprintln(stderr, "omp: writing rpc:", err)
		return 1
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type runOut struct {
		gen int
		ev  agent.Event
		err error
	}
	events := make(chan runOut, 32)
	gen := 0
	running := false
	start := func(prompt string) {
		gen++
		g := gen
		running = true
		go func() {
			defer func() { events <- runOut{gen: g} }()
			for ev, err := range run.Run(ctx, sess, prompt) {
				select {
				case events <- runOut{gen: g, ev: ev, err: err}:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
	}

	queue := append([]string(nil), prompts...)
	if len(queue) > 0 {
		start(queue[0])
		queue = queue[1:]
	}

	cmds := make(chan rpcCommand)
	go func() {
		sc := bufio.NewScanner(stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var c rpcCommand
			if err := json.Unmarshal(line, &c); err != nil {
				_ = w.send(wireFrame{Type: "error", Error: "bad command: " + err.Error()})
				continue
			}
			if c.Type == "quit" {
				_ = w.send(wireFrame{Type: "bye"})
				cmds <- rpcCommand{Type: "quit"}
				return
			}
			select {
			case cmds <- c:
			case <-ctx.Done():
				return
			}
		}
		cmds <- rpcCommand{Type: "eof"}
	}()

	var res *agent.Result
	pendingQuit := false
	stop := func() int {
		if res != nil && res.Reason == agent.ReasonCompleted {
			return 0
		}
		return 1
	}
	for {
		select {
		case <-ctx.Done():
			return stop()
		case c, ok := <-cmds:
			if !ok {
				return stop()
			}
			switch c.Type {
			case "quit", "eof":
				if running {
					pendingQuit = true
					continue
				}
				_ = w.send(wireFrame{Type: "bye"})
				return stop()
			case "prompt":
				if running {
					_ = w.send(wireFrame{Type: "error", Error: "a run is already in progress"})
					continue
				}
				start(c.Text)
			case "answer":
				host.Answer(c.ID, agent.AskResponse{Value: c.Value, Confirmed: c.Confirmed, Cancelled: c.Cancelled})
			case "tool_result":
				host.ResolveTool(c.ID, c.Text, c.IsError)
			default:
				_ = w.send(wireFrame{Type: "error", Error: "unknown command " + c.Type})
			}
		case out := <-events:
			if out.gen != gen {
				continue
			}
			if out.ev.Kind == "" && out.err == nil {
				running = false
				if len(queue) > 0 && res != nil && res.Reason == agent.ReasonCompleted {
					start(queue[0])
					queue = queue[1:]
					continue
				}
				queue = nil
				_ = w.send(wireFrame{Type: "idle"})
				if pendingQuit {
					_ = w.send(wireFrame{Type: "bye"})
					return stop()
				}
				continue
			}
			if out.err != nil {
				_ = w.send(wireFrame{Type: "error", Error: out.err.Error()})
				res = &agent.Result{Reason: agent.ReasonError, Err: out.err.Error()}
				return stop()
			}
			if out.ev.Kind == agent.EventResult {
				res = out.ev.Result
			}
			_ = w.send(wireOf(out.ev))
			if err := w.failure(); err != nil {
				fmt.Fprintln(stderr, "omp: writing rpc:", err)
				return 1
			}
		}
	}
}
