package subagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type fakeCtx struct {
	cwd string
	evs []agent.Event
}

func (f *fakeCtx) CWD() string          { return f.cwd }
func (f *fakeCtx) Workspaces() []string { return nil }
func (f *fakeCtx) Emit(ev agent.Event)  { f.evs = append(f.evs, ev) }
func (f *fakeCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, agent.ErrNoUI
}
func (f *fakeCtx) RecentMessages() []llm.Message { return nil }

func runTask(t *testing.T, t2 *Task, ctx *fakeCtx, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res := t2.Execute(context.Background(), ctx, raw)
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestTaskReturnsTheSubagentTextWithCounters(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	tool := &Task{Run: func(ctx context.Context, prompt string, _ func(agent.Event)) (Result, error) {
		if prompt != "find the bug" {
			t.Errorf("prompt = %q", prompt)
		}
		return Result{Text: "it is in calc.go", Turns: 3, Usage: llm.Usage{InputTokens: 100, OutputTokens: 20}, Reason: agent.ReasonCompleted}, nil
	}}
	got, isErr := runTask(t, tool, c, map[string]any{"description": "look for a bug", "prompt": "find the bug"})
	if isErr {
		t.Fatalf("a completed subagent must not be an error: %q", got)
	}
	for _, want := range []string{"it is in calc.go", "3 turns", "100 in / 20 out"} {
		if !strings.Contains(got, want) {
			t.Errorf("result missing %q:\n%s", want, got)
		}
	}
	if len(c.evs) != 1 || !strings.Contains(c.evs[0].Message, "look for a bug") {
		t.Errorf("the shell must see progress while it runs: %+v", c.evs)
	}
}

func TestTaskFailureIsAnErrorResult(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	tool := &Task{Run: func(context.Context, string, func(agent.Event)) (Result, error) {
		return Result{}, errFake
	}}
	got, isErr := runTask(t, tool, c, map[string]any{"prompt": "x"})
	if !isErr || !strings.Contains(got, "task failed") {
		t.Errorf("got isErr=%v %q", isErr, got)
	}
}

func TestTaskEmptySubagentTextIsStillReadable(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	tool := &Task{Run: func(context.Context, string, func(agent.Event)) (Result, error) {
		return Result{Reason: agent.ReasonCompleted}, nil
	}}
	got, _ := runTask(t, tool, c, map[string]any{"prompt": "x"})
	if !strings.Contains(got, "no text") {
		t.Errorf("an empty child must say so, not return a bare footer: %q", got)
	}
}

func TestTaskNeedsAPrompt(t *testing.T) {
	got, isErr := runTask(t, &Task{}, &fakeCtx{}, map[string]any{"description": "x"})
	if !isErr || !strings.Contains(got, "prompt is required") {
		t.Errorf("got isErr=%v %q", isErr, got)
	}
}

func TestTaskIsNotReadOnly(t *testing.T) {
	tool := &Task{Run: func(context.Context, string, func(agent.Event)) (Result, error) { return Result{}, nil }}
	if tool.IsReadOnly() || tool.Approval(nil).Tier != agent.TierWrite {
		t.Errorf("a subagent can write, so it must be write tier and never run in parallel")
	}
}

func TestChildRegistryHasNoTaskTool(t *testing.T) {
	parent := agent.NewRegistry()
	parent.Register(&Task{Run: func(context.Context, string, func(agent.Event)) (Result, error) { return Result{}, nil }})
	parent.Register(&stubTool{name: "read"})
	for _, name := range []string{"exit_plan_mode", "background", "goal"} {
		parent.Register(&stubTool{name: name})
	}
	child := childRegistry(parent, nil)
	for _, name := range []string{"task", "exit_plan_mode", "background", "goal"} {
		if _, ok := child.Get(name); ok {
			t.Errorf("the child registry must not contain %s", name)
		}
		if Inherits(name) {
			t.Errorf("Inherits(%q) must agree with the child registry", name)
		}
	}
	if _, ok := child.Get("read"); !ok || !Inherits("read") {
		t.Fatalf("the child must inherit the other tools")
	}
	if _, ok := parent.Get("task"); !ok {
		t.Fatalf("building the child must not mutate the parent")
	}
}

type stubTool struct{ name string }

func (s *stubTool) Name() string            { return s.name }
func (s *stubTool) Description() string     { return "" }
func (s *stubTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s *stubTool) Execute(context.Context, agent.ToolContext, json.RawMessage) agent.ToolResult {
	return agent.ToolResult{}
}

var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "boom" }

func TestTaskRelaysTheChildsActivityAsProgress(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	tool := &Task{Run: func(ctx context.Context, prompt string, progress func(agent.Event)) (Result, error) {
		progress(agent.Event{Kind: agent.EventToolUse, ToolUseID: "c1", ToolName: "grep", ToolInput: json.RawMessage(`{"pattern":"x"}`)})
		progress(agent.Event{Kind: agent.EventTextDelta, Text: "thinking out loud"})
		progress(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 10}})
		progress(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 50, OutputTokens: 5}})
		progress(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted}})
		return Result{Reason: agent.ReasonCompleted, Text: "found"}, nil
	}}
	runTask(t, tool, c, map[string]any{"description": "look", "prompt": "find x"})
	if len(c.evs) != 4 {
		t.Fatalf("want the opening line and one report per tool call or response, got %+v", c.evs)
	}
	for _, ev := range c.evs[1:] {
		if ev.Kind != agent.EventProgress || ev.ToolName != "grep" || string(ev.ToolInput) != `{"pattern":"x"}` {
			t.Errorf("each report must name the child's current tool: %+v", ev)
		}
	}
	if u := c.evs[3].Usage; u == nil || u.Total() != 165 {
		t.Errorf("the last report must carry the child's tokens so far: %+v", u)
	}
}

func TestTaskToolWiresTheChildLoopToProgress(t *testing.T) {
	dir := t.TempDir()
	model := scriptedModel(t, func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		if n == 1 {
			return toolTurn("c1", "write", map[string]any{"path": "a.txt", "body": "x"})
		}
		return textTurn("done")
	})
	reg := agent.NewRegistry()
	reg.Register(writeStub{})
	tool := NewTaskTool(Config{
		Registry: reg,
		Deps:     agent.RunDeps{CallModel: model},
		Options:  func(d string) agent.Options { return agent.Options{CWD: d} },
		CWD:      dir,
	})
	c := &fakeCtx{cwd: dir}
	if got, isErr := runTask(t, tool, c, map[string]any{"prompt": "write a"}); isErr {
		t.Fatalf("the child must complete: %s", got)
	}
	seen := false
	for _, ev := range c.evs {
		if ev.Kind == agent.EventProgress && ev.ToolName == "write" {
			seen = true
		}
	}
	if !seen {
		t.Errorf("the child's write must surface as progress: %+v", c.evs)
	}
}
