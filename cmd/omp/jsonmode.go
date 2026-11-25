package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func runJSON(ctx context.Context, run agent.Runner, sess session.Session, prompts []string, cwd string, stdout, stderr io.Writer) int {
	w := newWireWriter(stdout)
	if err := w.send(wireFrame{Type: "ready", State: &wireState{}}); err != nil {
		fmt.Fprintln(stderr, "omp: writing json:", err)
		return 1
	}
	st := &wireState{}
	var res *agent.Result
	emit := func(ev agent.Event) {
		f := wireOf(ev)
		trackState(st, ev)
		if f.Event != nil && f.Event.Kind == agent.EventResult {
			f.State = snapshot(st)
		}
		_ = w.send(f)
	}
	for _, p := range prompts {
		for ev, err := range run.Run(ctx, sess, p) {
			if err != nil {
				_ = w.send(wireFrame{Type: "error", Error: err.Error()})
				return 1
			}
			emit(ev)
			if ev.Kind == agent.EventResult {
				res = ev.Result
			}
		}
		if res == nil || res.Reason != agent.ReasonCompleted {
			break
		}
	}
	if err := w.failure(); err != nil {
		fmt.Fprintln(stderr, "omp: writing json:", err)
		return 1
	}
	if res == nil {
		return 1
	}
	if res.Reason == agent.ReasonCompleted {
		return 0
	}
	if ctx.Err() == nil {
		msg := res.Err
		if msg == "" {
			msg = "request " + string(res.Reason)
		}
		fmt.Fprintln(stderr, sanitize(msg))
	}
	return 1
}

func trackState(st *wireState, ev agent.Event) {
	switch ev.Kind {
	case agent.EventToolUse:
		st.Tools = append(st.Tools, wireTool{ID: ev.ToolUseID, Name: ev.ToolName, Input: string(ev.ToolInput), Status: "running"})
	case agent.EventToolResult:
		for i := range st.Tools {
			if st.Tools[i].ID != ev.ToolUseID || ev.ToolResult == nil {
				continue
			}
			st.Tools[i].Status = "done"
			st.Tools[i].Result = wireResultText(*ev.ToolResult)
			st.Tools[i].Error = ev.ToolResult.IsError
		}
	}
}

func snapshot(st *wireState) *wireState {
	cp := &wireState{Todos: append([]wireTodo(nil), st.Todos...), Tools: append([]wireTool(nil), st.Tools...)}
	return cp
}
