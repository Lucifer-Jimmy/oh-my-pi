package agent

import (
	"context"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func (r *run) request() llm.CompletionRequest {
	o := r.loop.opts
	return llm.CompletionRequest{
		System:        o.System,
		CacheBoundary: o.CacheBoundary,
		Messages:      r.sess.Messages(),
		Tools:         r.loop.reg.Schemas(),
		MaxTokens:     o.MaxTokens,
		Thinking:      o.Thinking,
	}
}

func (r *run) respond(ctx context.Context, req llm.CompletionRequest, onTool func(llm.ContentBlock)) (*llm.Accumulator, time.Duration, error) {
	started := time.Now()
	acc, received, err := r.stream(ctx, req, onTool)
	if err != nil && !received && r.ctx.Err() == nil && r.loop.deps.Compactor != nil && llm.IsContextOverflow(err) {
		if recorded, _ := r.compact(&req, session.TriggerOverflow, ""); recorded {
			started = time.Now()
			acc, _, err = r.stream(ctx, req, onTool)
		}
	}
	return acc, time.Since(started), err
}

func (r *run) stream(ctx context.Context, req llm.CompletionRequest, onTool func(llm.ContentBlock)) (*llm.Accumulator, bool, error) {
	acc := llm.NewAccumulator()
	received := false
	for ev, err := range r.loop.deps.CallModel(ctx, req) {
		if err != nil {
			return acc, received, err
		}
		received = true
		if e := acc.Add(ev); e != nil {
			return acc, received, e
		}
		switch ev.Type {
		case llm.SETextDelta:
			r.em.send(Event{Kind: EventTextDelta, Text: ev.Text})
		case llm.SEThinkingDelta:
			r.em.send(Event{Kind: EventThinkingDelta, Text: ev.Text})
		case llm.SEBlockStop:
			if onTool != nil {
				if b, ok := acc.Block(ev.BlockIndex); ok && b.Type == llm.BlockToolUse {
					onTool(b)
				}
			}
		}
	}
	return acc, received, nil
}
