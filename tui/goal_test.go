package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

type fakeGoal struct {
	args    []string
	prompt  string
	notice  string
	err     error
	summary string
}

func (f *fakeGoal) Command(args string) (string, string, error) {
	f.args = append(f.args, args)
	return f.prompt, f.notice, f.err
}

func (f *fakeGoal) Summary() string { return f.summary }

func TestGoalCommandStartsTheObjectiveAsARun(t *testing.T) {
	r := &scriptRunner{script: func(ctx context.Context, input string, emit func(agent.Event)) {
		emit(text("on it"))
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, r, nil)
	g := &fakeGoal{prompt: "make the tests pass", notice: "Goal set.", summary: "goal active 1.2k"}
	h.m.opts.Goal = g
	h.submit("/goal make the tests pass")
	h.pump()
	if len(g.args) != 1 || strings.TrimSpace(g.args[0]) != "make the tests pass" {
		t.Fatalf("the arguments reach the goal control: %q", g.args)
	}
	if seen := r.seen(); len(seen) != 1 || seen[0] != "make the tests pass" {
		t.Fatalf("the objective starts a run: %q", seen)
	}
	if out := h.out(); !strings.Contains(out, "Goal set.") {
		t.Errorf("the notice is shown:\n%s", out)
	}
	if v := h.view(); !strings.Contains(v, "acceptEdits · goal active 1.2k · ctx") {
		t.Errorf("the status bar shows the goal next to the mode:\n%s", v)
	}
}

func TestGoalCommandWithoutAPromptDoesNotStartARun(t *testing.T) {
	r := &scriptRunner{script: func(ctx context.Context, input string, emit func(agent.Event)) {
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, r, nil)
	h.m.opts.Goal = &fakeGoal{notice: "Goal paused."}
	h.submit("/goal pause")
	if h.m.run != nil || len(r.seen()) != 0 {
		t.Fatalf("a goal command with no prompt only reports")
	}
	if out := h.out(); !strings.Contains(out, "Goal paused.") {
		t.Errorf("notice missing:\n%s", out)
	}
	h.m.opts.Goal = &fakeGoal{err: errors.New("no goal is set")}
	h.submit("/goal resume")
	if out := h.out(); !strings.Contains(out, "Goal: no goal is set") {
		t.Errorf("an error is reported as a warning:\n%s", out)
	}
	if v := h.view(); strings.Contains(v, "· goal") {
		t.Errorf("no goal, no status segment:\n%s", v)
	}
}

func TestGoalCommandSteersARunningTurn(t *testing.T) {
	release := make(chan struct{})
	r := &scriptRunner{script: func(ctx context.Context, input string, emit func(agent.Event)) {
		<-release
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, r, nil)
	var steered []string
	h.m.opts.Steer = func(text string) { steered = append(steered, text) }
	h.m.opts.Goal = &fakeGoal{prompt: "ship it"}
	h.submit("first")
	h.submit("/goal ship it")
	close(release)
	h.pump()
	if len(steered) != 1 || steered[0] != "ship it" || len(r.seen()) != 1 {
		t.Fatalf("a goal set during a run steers it: steered %q, runs %q", steered, r.seen())
	}
}

func TestGoalCommandWithoutControlSaysSo(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/goal anything")
	if out := h.out(); !strings.Contains(out, "Goal mode is not available") {
		t.Errorf("missing control must be reported:\n%s", out)
	}
}

type fakeMemory struct {
	args   []string
	notice string
	err    error
}

func (f *fakeMemory) Command(args string) (string, error) {
	f.args = append(f.args, args)
	return f.notice, f.err
}

func TestMemoryCommandShowsWhatTheControlSays(t *testing.T) {
	r := &scriptRunner{script: func(ctx context.Context, input string, emit func(agent.Event)) {
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, r, nil)
	h.submit("/memory")
	if out := h.out(); !strings.Contains(out, "Memory is off") || !strings.Contains(out, "backend: local") {
		t.Fatalf("without memory the command says how to turn it on:\n%s", out)
	}
	mem := &fakeMemory{notice: "Memory for this project: /m"}
	h.m.opts.Memory = mem
	h.submit("/memory sync")
	if len(mem.args) != 1 || strings.TrimSpace(mem.args[0]) != "sync" || !strings.Contains(h.out(), "Memory for this project: /m") {
		t.Fatalf("args %q, out:\n%s", mem.args, h.out())
	}
	h.m.opts.Memory = &fakeMemory{err: errors.New("a memory update is already running")}
	h.submit("/memory sync")
	if !strings.Contains(h.out(), "Memory: a memory update is already running") {
		t.Fatalf("errors are warnings:\n%s", h.out())
	}
	if h.m.run != nil || len(r.seen()) != 0 {
		t.Fatal("/memory never starts a turn")
	}
}
