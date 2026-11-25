package main

import (
	"context"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

type step struct {
	id    string
	tool  string
	input map[string]any
	text  string
}

type stepModel struct {
	steps []step
	calls int
}

func (m *stepModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		s := m.steps[m.calls]
		m.calls++
		evs := []llm.StreamEvent{{Type: llm.SEMessageStart}}
		if s.tool == "" {
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
				llm.StreamEvent{Type: llm.SETextDelta, Text: s.text},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
			)
		} else {
			raw, _ := json.Marshal(s.input)
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: s.id, Name: s.tool, Input: raw}},
				llm.StreamEvent{Type: llm.SEBlockStop},
				llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
			)
		}
		evs = append(evs, llm.StreamEvent{Type: llm.SEMessageStop})
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func toolResults(t *testing.T, loop *agent.Loop) map[string]agent.ToolResult {
	t.Helper()
	out := map[string]agent.ToolResult{}
	for ev, err := range loop.Run(context.Background(), session.NewMemory(nil), "go") {
		if err != nil {
			t.Fatal(err)
		}
		if ev.Kind == agent.EventToolResult {
			out[ev.ToolUseID] = *ev.ToolResult
		}
		if ev.Kind == agent.EventResult && ev.Result.Reason != agent.ReasonCompleted {
			t.Fatalf("run ended with %s: %s", ev.Result.Reason, ev.Result.Err)
		}
	}
	return out
}

func text(r agent.ToolResult) string {
	var sb strings.Builder
	for _, b := range r.Content {
		sb.WriteString(b.Text)
	}
	return sb.String()
}

func TestAssembledToolsEnforceReadBeforeEditAndApproval(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &stepModel{steps: []step{
		{id: "e1", tool: "edit", input: map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "bye"}},
		{id: "r1", tool: "read", input: map[string]any{"path": "a.txt"}},
		{id: "e2", tool: "edit", input: map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "bye"}},
		{id: "b1", tool: "bash", input: map[string]any{"command": "go test ./..."}},
		{id: "b2", tool: "bash", input: map[string]any{"command": "git status"}},
		{text: "done"},
	}}
	cfg := config{}
	shell := tools.ResolveShell("")
	reg := assemble(dir, shell, permission.NewPolicy(cfg.approvalMode(), cfg.rules()), agent.RunDeps{CallModel: m.call}, cfg, nil, nil, nil, nil, nil)
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: m.call}, nil, options(cfg, dir, reg.Names(), shell.Name, nil))

	got := toolResults(t, loop)

	if r := got["e1"]; !r.IsError || !strings.Contains(text(r), "has not been read") {
		t.Errorf("edit before read must be refused with a readable reason, got %+v", text(r))
	}
	if r := got["e2"]; r.IsError {
		t.Errorf("edit after read must succeed, got %q", text(r))
	}
	if b, _ := os.ReadFile(target); string(b) != "bye\n" {
		t.Errorf("file content = %q", b)
	}
	if r := got["b1"]; !r.IsError || !strings.Contains(text(r), "add an allow rule") {
		t.Errorf("running code without a host must be denied with a way forward, got %q", text(r))
	}
	if r := got["b2"]; r.IsError && strings.Contains(text(r), "approval") {
		t.Errorf("an inspection command must not need approval under acceptEdits, got %q", text(r))
	}
}

func TestSystemPromptMatchesAssembledTools(t *testing.T) {
	dir := t.TempDir()
	shell := tools.Shell{Path: "bash", Name: "Git Bash"}
	reg := assemble(dir, shell, permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{}), agent.RunDeps{}, config{}, nil, nil, nil, nil, nil)
	system := options(config{}, dir, reg.Names(), shell.Name, nil).System
	static, context := system[0], system[1]

	for _, name := range []string{"read", "glob", "grep", "edit", "write", "bash", "exit_plan_mode"} {
		if !strings.Contains(static, "- "+name+":") {
			t.Errorf("system prompt does not describe registered tool %q", name)
		}
	}
	if strings.Contains(static, "You cannot") {
		t.Errorf("all capabilities are registered, nothing may be declared missing:\n%s", static)
	}
	if !strings.Contains(context, "Shell: Git Bash") {
		t.Errorf("prompt must name the shell bash will actually use:\n%s", context)
	}
}

func TestPlanReminderReachesTheModel(t *testing.T) {
	var inputs []string
	call := func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		last := req.Messages[len(req.Messages)-1]
		inputs = append(inputs, last.Content[0].Text)
		return (&stepModel{steps: []step{{text: "ok"}}}).call(ctx, req)
	}
	policy := permission.NewPolicy(permission.ModePlan, permission.Rules{})
	run := withPlanReminder(agent.NewLoop(agent.NewRegistry(), agent.RunDeps{CallModel: call}, nil, agent.Options{}), policy)
	sess := session.NewMemory(nil)
	for i, input := range []string{"plan it", "do it", "more"} {
		if i == 1 {
			policy.SetMode(permission.ModeAcceptEdits)
		}
		for _, err := range run.Run(context.Background(), sess, input) {
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	if len(inputs) != 3 || !strings.Contains(inputs[0], "Plan mode is active") || !strings.HasSuffix(inputs[0], "plan it") ||
		!strings.Contains(inputs[1], "Plan mode is off") || !strings.HasSuffix(inputs[1], "do it") || inputs[2] != "more" {
		t.Errorf("the model must learn when plan mode starts and ends: %q", inputs)
	}
}

func TestAssembledToolsValidateArguments(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &stepModel{steps: []step{
		{id: "r1", tool: "read", input: map[string]any{"file_path": "a.txt"}},
		{id: "r2", tool: "read", input: map[string]any{"path": "a.txt", "offset": "2", "limit": "1"}},
		{id: "g1", tool: "grep", input: map[string]any{"pattern": "TWO", "-i": true}},
		{id: "w1", tool: "write", input: map[string]any{"path": "b.txt", "content": "x", "mode": "overwrite"}},
		{text: "done"},
	}}
	cfg := config{}
	shell := tools.ResolveShell("")
	reg := assemble(dir, shell, permission.NewPolicy(cfg.approvalMode(), cfg.rules()), agent.RunDeps{CallModel: m.call}, cfg, nil, nil, nil, nil, nil)
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: m.call}, nil, options(cfg, dir, reg.Names(), shell.Name, nil))

	got := toolResults(t, loop)

	if r := got["r1"]; !r.IsError || !strings.Contains(text(r), `unknown property "file_path" (valid properties: limit, offset, path)`) ||
		!strings.Contains(text(r), `missing required property "path"`) {
		t.Errorf("a misnamed argument must be named back to the model, got %q", text(r))
	}
	if r := got["r2"]; r.IsError || !strings.Contains(text(r), "\ttwo") || strings.Contains(text(r), "\tthree") {
		t.Errorf("numbers sent as strings must work, got %q", text(r))
	}
	if r := got["g1"]; !r.IsError || !strings.Contains(text(r), `unknown property "-i"`) {
		t.Errorf("an unknown grep flag must not silently run a case-sensitive search, got %q", text(r))
	}
	if r := got["w1"]; !r.IsError || !strings.Contains(text(r), `unknown property "mode"`) {
		t.Errorf("write with an unknown property must be refused, got %q", text(r))
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Error("a rejected call must not reach the tool")
	}
}

func TestSubagentPolicyInheritsTheParentsWriteDecision(t *testing.T) {
	accept := permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{})
	if got := subagentMode(accept, nil); got != permission.ModeBypass {
		t.Fatalf("when the parent would allow a write, the child gets bypass: %q", got)
	}
	strict := permission.NewPolicy(permission.ModeDefault, permission.Rules{})
	if got := subagentMode(strict, nil); got != permission.ModeDefault {
		t.Fatalf("a parent that asks must keep the child asking: %q", got)
	}
	plan := permission.NewPolicy(permission.ModePlan, permission.Rules{})
	if got := subagentMode(plan, nil); got != permission.ModePlan {
		t.Fatalf("plan mode must not be widened by a subagent: %q", got)
	}
	denied := permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{Deny: []permission.Rule{{Tool: "bash", Spec: "git push*"}}})
	mode, rules := denied.Snapshot()
	d := permission.Evaluate(permission.Request{
		Tool: "bash", Tier: permission.TierWrite, Mode: subagentMode(denied, nil), Rules: rules,
		Specs: []permission.Spec{{Kind: permission.KindCommand, Value: "git push origin main"}},
	})
	if mode != permission.ModeAcceptEdits || d.Behavior != permission.Deny {
		t.Fatalf("deny rules must still win in the child: mode=%q decision=%+v", mode, d)
	}
}
