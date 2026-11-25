package goal

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type step func(yield func(agent.Event, error) bool)

type script struct {
	inputs []string
	steps  []step
}

func (s *script) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
	s.inputs = append(s.inputs, input)
	n := len(s.inputs) - 1
	return func(yield func(agent.Event, error) bool) {
		if n < len(s.steps) {
			s.steps[n](yield)
			return
		}
		yield(done(agent.ReasonCompleted), nil)
	}
}

func done(reason agent.StopReason) agent.Event {
	return agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: reason, Turns: 1, Text: string(reason), Usage: llm.Usage{InputTokens: 10}}}
}

func toolCall(name, input, output string) []agent.Event {
	return []agent.Event{
		{Kind: agent.EventToolUse, ToolName: name, ToolInput: []byte(input)},
		{Kind: agent.EventToolResult, ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(output)}}},
	}
}

func emit(evs ...agent.Event) step {
	return func(yield func(agent.Event, error) bool) {
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func then(evs []agent.Event, more ...agent.Event) []agent.Event {
	return append(append([]agent.Event(nil), evs...), more...)
}

func drive(t *testing.T, r Runner, input string) ([]agent.Event, error) {
	t.Helper()
	var out []agent.Event
	for ev, err := range r.Run(context.Background(), session.NewMemory(nil), input) {
		if err != nil {
			return out, err
		}
		out = append(out, ev)
	}
	return out, nil
}

func notes(evs []agent.Event) []string {
	var out []string
	for _, ev := range evs {
		if ev.Kind == agent.EventNotification {
			out = append(out, ev.Message)
		}
	}
	return out
}

func final(t *testing.T, evs []agent.Event) agent.Result {
	t.Helper()
	var results []agent.Result
	for _, ev := range evs {
		if ev.Kind == agent.EventResult {
			results = append(results, *ev.Result)
		}
	}
	if len(results) != 1 {
		t.Fatalf("exactly one result reaches the shell, got %d", len(results))
	}
	return results[0]
}

func TestWithoutAGoalTheRunPassesThrough(t *testing.T) {
	tr, _, _ := newTracker()
	s := &script{}
	evs, err := drive(t, Runner{Runner: s, Tracker: tr}, "hello")
	if err != nil || len(s.inputs) != 1 || s.inputs[0] != "hello" {
		t.Fatalf("inputs = %q, err %v", s.inputs, err)
	}
	if res := final(t, evs); res.Reason != agent.ReasonCompleted || !res.Complete || len(notes(evs)) != 0 {
		t.Fatalf("result = %+v notes %v", res, notes(evs))
	}
}

func TestAnActiveGoalContinuesUntilTheModelCompletesIt(t *testing.T) {
	tr, _, _ := newTracker()
	tr.Create("all tests green", 0)
	s := &script{}
	s.steps = []step{
		emit(then(toolCall("bash", `{"command":"go test ./..."}`, "FAIL"), agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 20}}, done(agent.ReasonCompleted))...),
		emit(then(toolCall("edit", `{"path":"a.go"}`, "ok"), done(agent.ReasonCompleted))...),
		func(yield func(agent.Event, error) bool) {
			for _, ev := range toolCall("bash", `{"command":"go test ./..."}`, "ok") {
				yield(ev, nil)
			}
			tr.Complete()
			yield(done(agent.ReasonCompleted), nil)
		},
	}
	evs, err := drive(t, Runner{Runner: s, Tracker: tr}, "start")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.inputs) != 3 {
		t.Fatalf("runs = %d, want 3", len(s.inputs))
	}
	if !strings.HasPrefix(s.inputs[0], "<goal_context>") || !strings.HasSuffix(s.inputs[0], "\n\nstart") {
		t.Fatalf("the user's input carries the goal context:\n%s", s.inputs[0])
	}
	for _, in := range s.inputs[1:] {
		if !strings.HasPrefix(in, "<goal_continuation>") || !strings.Contains(in, "all tests green") {
			t.Fatalf("an automatic turn gets the continuation prompt:\n%s", in)
		}
	}
	if !strings.Contains(s.inputs[1], "Tokens used: 120") {
		t.Fatalf("the continuation states the spend so far:\n%s", s.inputs[1])
	}
	res := final(t, evs)
	if res.Reason != agent.ReasonCompleted || res.Turns != 3 || res.Usage.InputTokens != 30 || !res.Complete {
		t.Fatalf("the final result adds up every run: %+v", res)
	}
	if got := notes(evs); len(got) != 3 || got[0] != noteContinuing || got[1] != noteContinuing || !strings.HasPrefix(got[2], "Goal complete: all tests green · 120 tokens") {
		t.Fatalf("notes = %v", got)
	}
	if g, _ := tr.Current(); g.Status != StatusComplete || g.TokensUsed != 120 {
		t.Fatalf("goal = %+v", g)
	}
}

func TestAnAutomaticTurnWithoutProgressStopsTheLoop(t *testing.T) {
	for name, steps := range map[string][]step{
		"no tool calls": {
			emit(done(agent.ReasonCompleted)),
			emit(done(agent.ReasonCompleted)),
		},
		"the same calls twice": {
			emit(done(agent.ReasonCompleted)),
			emit(then(toolCall("read", `{"path":"a"}`, "x"), done(agent.ReasonCompleted))...),
			emit(then(toolCall("read", `{"path":"a"}`, "x"), done(agent.ReasonCompleted))...),
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _, _ := newTracker()
			tr.Create("x", 0)
			s := &script{steps: steps}
			evs, _ := drive(t, Runner{Runner: s, Tracker: tr}, "go")
			if len(s.inputs) != len(steps) {
				t.Fatalf("runs = %d, want %d", len(s.inputs), len(steps))
			}
			got := notes(evs)
			if len(got) == 0 || got[len(got)-1] != noteStalled {
				t.Fatalf("notes = %v", got)
			}
			if g, _ := tr.Current(); g.Status != StatusActive {
				t.Fatalf("a stall leaves the goal active for the user: %+v", g)
			}
		})
	}
}

func TestAnInterruptionPausesTheGoal(t *testing.T) {
	tr, _, _ := newTracker()
	tr.Create("x", 0)
	s := &script{steps: []step{emit(done(agent.ReasonAborted))}}
	evs, _ := drive(t, Runner{Runner: s, Tracker: tr}, "go")
	if len(s.inputs) != 1 || final(t, evs).Reason != agent.ReasonAborted {
		t.Fatalf("runs = %d", len(s.inputs))
	}
	if g, _ := tr.Current(); g.Status != StatusPaused {
		t.Fatalf("status = %s", g.Status)
	}
	if got := notes(evs); len(got) != 1 || got[0] != notePaused {
		t.Fatalf("notes = %v", got)
	}
}

func TestPlanModeHoldsTheContinuation(t *testing.T) {
	tr, _, _ := newTracker()
	tr.Create("x", 0)
	s := &script{}
	evs, _ := drive(t, Runner{Runner: s, Tracker: tr, Hold: func() bool { return true }}, "go")
	if len(s.inputs) != 1 {
		t.Fatalf("runs = %d", len(s.inputs))
	}
	if got := notes(evs); len(got) != 1 || got[0] != noteHeld {
		t.Fatalf("notes = %v", got)
	}
}

func TestReachingTheBudgetEndsTheLoop(t *testing.T) {
	tr, _, _ := newTracker()
	tr.Create("x", 100)
	s := &script{steps: []step{
		emit(then(toolCall("read", `{}`, "x"), agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 150}}, done(agent.ReasonCompleted))...),
	}}
	evs, _ := drive(t, Runner{Runner: s, Tracker: tr}, "go")
	if len(s.inputs) != 1 {
		t.Fatalf("a budget-limited goal does not continue, runs = %d", len(s.inputs))
	}
	if got := notes(evs); len(got) != 1 || !strings.HasPrefix(got[0], "Goal: token budget reached (150 tokens of 100)") {
		t.Fatalf("notes = %v", got)
	}
	if g, _ := tr.Current(); g.Status != StatusBudgetLimited {
		t.Fatalf("status = %s", g.Status)
	}
	if msgs, _ := tr.OnPrompt(context.Background()); len(msgs) != 1 {
		t.Fatal("the wrap-up message waits for the next round")
	}
	s2 := &script{}
	drive(t, Runner{Runner: s2, Tracker: tr}, "anything else?")
	if strings.HasPrefix(s2.inputs[0], "<goal_context>") {
		t.Fatal("only an active goal is prefixed")
	}
}

func TestAGoalCreatedDuringARunIsContinuedAndTimed(t *testing.T) {
	c := &clock{t: time.UnixMilli(1_800_000_000_000)}
	tr := &Tracker{Now: func() time.Time {
		c.t = c.t.Add(2 * time.Second)
		return c.t
	}}
	s := &script{steps: []step{
		func(yield func(agent.Event, error) bool) {
			tr.Create("created by the tool", 0)
			yield(done(agent.ReasonCompleted), nil)
		},
		func(yield func(agent.Event, error) bool) {
			tr.Complete()
			yield(done(agent.ReasonCompleted), nil)
		},
	}}
	drive(t, Runner{Runner: s, Tracker: tr}, "set a goal")
	if len(s.inputs) != 2 || !strings.HasPrefix(s.inputs[1], "<goal_continuation>") {
		t.Fatalf("inputs = %q", s.inputs)
	}
	if g, _ := tr.Current(); g.TimeUsedSeconds == 0 {
		t.Fatalf("the runs that worked on the goal are timed: %+v", g)
	}
}

func TestAnErrorFromTheInnerRunEndsEverything(t *testing.T) {
	tr, _, _ := newTracker()
	tr.Create("x", 0)
	boom := errors.New("boom")
	s := &script{steps: []step{func(yield func(agent.Event, error) bool) { yield(agent.Event{}, boom) }}}
	_, err := drive(t, Runner{Runner: s, Tracker: tr}, "go")
	if !errors.Is(err, boom) || len(s.inputs) != 1 {
		t.Fatalf("err = %v runs = %d", err, len(s.inputs))
	}
}
