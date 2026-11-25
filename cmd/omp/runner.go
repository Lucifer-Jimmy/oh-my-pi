package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/plan"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
	"github.com/Lucifer-Jimmy/oh-my-pi/trace"
	"github.com/Lucifer-Jimmy/oh-my-pi/verifier"
)

type steerer struct {
	mu   sync.Mutex
	msgs []string
}

func (s *steerer) Push(text string) {
	s.mu.Lock()
	s.msgs = append(s.msgs, text)
	s.mu.Unlock()
}

func (s *steerer) Take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.msgs
	s.msgs = nil
	return out
}

func openTrace(cfg config, cwd string, conv *conversation, window *atomic.Int64, stderr io.Writer) (*trace.Recorder, error) {
	path := cfg.tracePath
	if path == "" {
		return nil, nil
	}
	rec, err := trace.New(path, trace.Meta{CWD: cwd, Model: cfg.modelLabel, Session: conv.sessionPath(), Window: func() int { return int(window.Load()) }})
	if err != nil {
		return nil, fmt.Errorf("writing trace to %s: %w", path, err)
	}
	return rec, nil
}

func traceRunner(rec *trace.Recorder, run agent.Runner) agent.Runner {
	if rec == nil {
		return run
	}
	return agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			for ev, err := range run.Run(ctx, sess, input) {
				if err != nil {
					yield(ev, err)
					return
				}
				rec.Write(ev)
				if !yield(ev, err) {
					return
				}
			}
		}
	})
}

type compactFunc = func(ctx context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error]

func traceCompact(rec *trace.Recorder, compact compactFunc) compactFunc {
	if rec == nil || compact == nil {
		return compact
	}
	return func(ctx context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			for ev, err := range compact(ctx, sess, instructions) {
				if err != nil {
					yield(ev, err)
					return
				}
				rec.Write(ev)
				if !yield(ev, err) {
					return
				}
			}
		}
	}
}

func verifyRunner(cfg config, cwd string, shell tools.Shell, run agent.Runner) agent.Runner {
	if cfg.verify == "" {
		return run
	}
	return agent.VerifierRunner{
		Runner:   run,
		Attempts: cfg.verifyTries,
		Checks:   []agent.Verifier{verifier.Command{Command: cfg.verify, Dir: cwd, Shell: shell}},
	}
}

func withPlanReminder(loop *agent.Loop, policy *permission.Policy) agent.RunnerFunc {
	return func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		inPlan := policy.Mode() == permission.ModePlan
		return loop.Run(ctx, sess, plan.WithReminder(input, inPlan, sess.Messages()))
	}
}

func goalRunner(goals *goal.Tracker, policy *permission.Policy, run agent.Runner) agent.Runner {
	if goals == nil {
		return run
	}
	return goal.Runner{Runner: run, Tracker: goals, Hold: func() bool { return policy.Mode() == permission.ModePlan }}
}

type promptHooks []agent.PromptInjectHook

func (p promptHooks) OnPrompt(ctx context.Context) ([]llm.Message, error) {
	var out []llm.Message
	for _, h := range p {
		msgs, err := h.OnPrompt(ctx)
		out = append(out, msgs...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func withGoalPrompt(h agent.Hooks, goals *goal.Tracker) agent.Hooks {
	switch {
	case goals == nil:
	case h.Prompt == nil:
		h.Prompt = goals
	default:
		h.Prompt = promptHooks{goals, h.Prompt}
	}
	return h
}
