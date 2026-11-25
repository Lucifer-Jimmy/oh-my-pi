package agent

import (
	"context"
	"iter"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type Compactor interface {
	Compact(ctx context.Context, req CompactRequest) ([]llm.Message, *session.Compaction, error)
}

type CompactRequest struct {
	Request      llm.CompletionRequest
	Last         session.Compaction
	Trigger      session.Trigger
	Instructions string
}

const nothingToCompact = "Nothing to compact yet: the conversation already fits in the recent window."

func (l *Loop) Compact(ctx context.Context, sess session.Session, instructions string) iter.Seq2[Event, error] {
	return eventSeq(func(events chan<- Event) { l.newRun(ctx, sess, events).compactNow(instructions) })
}

func (r *run) compactNow(instructions string) {
	if r.loop.deps.Compactor == nil {
		r.finish(ReasonError, "compaction is not available in this session")
		return
	}
	req := r.request()
	recorded, err := r.compact(&req, session.TriggerManual, instructions)
	switch {
	case r.ctx.Err() != nil:
		r.finish(ReasonAborted, r.ctx.Err().Error())
	case err != nil:
		r.finish(ReasonError, "compaction failed: "+err.Error())
	case !recorded:
		r.em.send(Event{Kind: EventCompaction, Message: nothingToCompact})
		r.finish(ReasonCompleted, "")
	default:
		r.finish(ReasonCompleted, "")
	}
}

func (r *run) compact(req *llm.CompletionRequest, trigger session.Trigger, instructions string) (bool, error) {
	last, _ := r.sess.LastCompaction()
	view, cp, err := r.loop.deps.Compactor.Compact(r.ctx, CompactRequest{Request: *req, Last: last, Trigger: trigger, Instructions: instructions})
	if view != nil {
		req.Messages = view
	}
	if cp != nil {
		if cp.Timestamp == 0 {
			cp.Timestamp = r.loop.clock().UnixMilli()
		}
		r.sess.AppendCompaction(*cp)
		r.usage.Add(cp.Usage)
		r.em.send(Event{Kind: EventCompaction, Compaction: cp})
	}
	if err != nil && r.ctx.Err() == nil && trigger != session.TriggerManual {
		r.em.send(Event{Kind: EventCompaction, Message: "Compaction failed, continuing without it: " + err.Error()})
	}
	return cp != nil, err
}
