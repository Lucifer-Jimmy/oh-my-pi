package goal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"iter"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const (
	noteContinuing = "Goal: continuing toward the objective"
	notePaused     = "Goal paused by the interruption; /goal resume continues it"
	noteHeld       = "Goal: not continuing while in plan mode"
	noteStalled    = "Goal: the last automatic turn made no progress, so continuation stopped; send a message to go on"
)

type Runner struct {
	Runner  agent.Runner
	Tracker *Tracker
	Hold    func() bool
}

func (r Runner) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
	return func(yield func(agent.Event, error) bool) {
		prompt := input
		if g, ok := r.Tracker.Current(); ok && g.Status == StatusActive {
			prompt = ActivePrompt(g) + "\n\n" + input
		}
		var usage llm.Usage
		turns := 0
		automatic := false
		previous := ""
		for {
			before, had := r.Tracker.Current()
			started := r.Tracker.now()
			var last *agent.Result
			var activity strings.Builder
			for ev, err := range r.Runner.Run(ctx, sess, prompt) {
				if err != nil {
					r.spend(before, had, started)
					yield(agent.Event{}, err)
					return
				}
				switch ev.Kind {
				case agent.EventUsage:
					if ev.Usage != nil {
						r.Tracker.Account(*ev.Usage)
					}
				case agent.EventToolUse:
					activity.WriteString("call\x00" + ev.ToolName + "\x00" + string(ev.ToolInput) + "\x00")
				case agent.EventToolResult:
					if ev.ToolResult != nil {
						activity.WriteString("result\x00" + resultText(*ev.ToolResult) + "\x00")
						if ev.ToolResult.IsError {
							activity.WriteString("error\x00")
						}
					}
				case agent.EventResult:
					if ev.Result != nil {
						last = ev.Result
						usage.Add(ev.Result.Usage)
						turns += ev.Result.Turns
					}
					continue
				}
				if !yield(ev, nil) {
					return
				}
			}
			r.spend(before, had, started)
			if last == nil {
				last = &agent.Result{Reason: agent.ReasonError, Err: "the run ended without a result"}
			}
			note := ""
			if last.Reason == agent.ReasonAborted {
				if g, ok := r.Tracker.Current(); ok && g.Status == StatusActive {
					if _, err := r.Tracker.Pause(); err == nil {
						note = notePaused
					}
				}
			}
			if g, ok := r.Tracker.Current(); ok && note == "" && (!had || before.ID != g.ID || before.Status != g.Status) {
				switch g.Status {
				case StatusComplete:
					note = "Goal complete: " + firstLine(g.Objective) + " · " + usageLine(g)
				case StatusBudgetLimited:
					note = "Goal: token budget reached (" + usageLine(g) + "); /goal budget <tokens> raises it"
				}
			}
			proceed := false
			if g, ok := r.Tracker.Current(); ok && g.Status == StatusActive && last.Reason == agent.ReasonCompleted && ctx.Err() == nil {
				proceed = true
				if r.Hold != nil && r.Hold() {
					proceed, note = false, noteHeld
				}
			}
			if proceed && automatic {
				digest := fingerprint(activity.String())
				if digest == "" || digest == previous {
					proceed, note = false, noteStalled
				}
				previous = digest
			}
			if !proceed {
				if note != "" && !yield(agent.Event{Kind: agent.EventNotification, Message: note}, nil) {
					return
				}
				final := *last
				final.Usage, final.Turns, final.Complete = usage, turns, true
				final.Messages = sess.Messages()
				yield(agent.Event{Kind: agent.EventResult, Result: &final}, nil)
				return
			}
			if !yield(agent.Event{Kind: agent.EventNotification, Message: noteContinuing}, nil) {
				return
			}
			g, _ := r.Tracker.Current()
			prompt = ContinuationPrompt(g)
			automatic = true
		}
	}
}

func (r Runner) spend(before Goal, had bool, started time.Time) {
	now := r.Tracker.now()
	if had && before.accounting() {
		r.Tracker.AddTime(before.ID, now.Sub(started))
		return
	}
	g, ok := r.Tracker.Current()
	if !ok || (had && g.ID == before.ID) || g.CreatedAt < started.UnixMilli() {
		return
	}
	r.Tracker.AddTime(g.ID, now.Sub(time.UnixMilli(g.CreatedAt)))
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(line); len(r) > 80 {
		line = string(r[:80]) + "…"
	}
	return line
}

func usageLine(g Goal) string {
	s := formatTokens(g.TokensUsed) + " tokens"
	if g.TokenBudget > 0 {
		s += " of " + formatTokens(g.TokenBudget)
	}
	if g.TimeUsedSeconds > 0 {
		s += ", " + (time.Duration(g.TimeUsedSeconds) * time.Second).String()
	}
	return s
}

func resultText(res agent.ToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
		for _, inner := range c.Content {
			b.WriteString(inner.Text)
		}
	}
	return b.String()
}

func fingerprint(activity string) string {
	if activity == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(activity))
	return hex.EncodeToString(sum[:])
}
