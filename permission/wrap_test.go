package permission

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type fakeCtx struct {
	approve   bool
	always    bool
	cancelled bool
	asked     bool
	last      agent.AskRequest
}

func (f *fakeCtx) CWD() string                   { return "." }
func (f *fakeCtx) Workspaces() []string          { return nil }
func (f *fakeCtx) Emit(agent.Event)              {}
func (f *fakeCtx) RecentMessages() []llm.Message { return nil }
func (f *fakeCtx) Ask(_ context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	f.asked, f.last = true, req
	resp := agent.AskResponse{Cancelled: f.cancelled, Value: req.Options[len(req.Options)-1]}
	switch {
	case f.always && len(req.Options) == 3:
		resp.Value = req.Options[1]
	case f.approve:
		resp.Value = req.Options[0]
	}
	return resp, nil
}

type fakeTool struct {
	tier   agent.Tier
	called int
	specs  func(json.RawMessage) []Spec
}

func (f *fakeTool) Name() string                           { return "bash" }
func (f *fakeTool) Description() string                    { return "fake" }
func (f *fakeTool) Schema() json.RawMessage                { return json.RawMessage(`{}`) }
func (f *fakeTool) IsReadOnly() bool                       { return false }
func (f *fakeTool) IsConcurrencySafe(json.RawMessage) bool { return false }
func (f *fakeTool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: f.tier}
}
func (f *fakeTool) Execute(context.Context, agent.ToolContext, json.RawMessage) agent.ToolResult {
	f.called++
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ran")}}
}

func (f *fakeTool) ReviewSpecs(input json.RawMessage) []Spec {
	if f.specs == nil {
		return nil
	}
	return f.specs(input)
}

type inputTierTool struct {
	fakeTool
}

func (i *inputTierTool) Approval(input json.RawMessage) agent.Approval {
	if string(input) == `"safe"` {
		return agent.Approval{Tier: agent.TierRead}
	}
	return agent.Approval{Tier: agent.TierExec}
}

func runTool(t *testing.T, tool agent.Tool, tc agent.ToolContext, args string) agent.ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), tc, json.RawMessage(args))
}

func TestWrapDeniesWithoutInteractiveHost(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	res := runTool(t, w, &fakeCtx{}, `{}`)
	if !res.IsError {
		t.Fatal("must deny when no approval is possible")
	}
	if inner.called != 0 {
		t.Error("denied tool must not execute")
	}
}

func TestWrapAsksAndExecutesOnApproval(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	tc := &fakeCtx{approve: true}
	res := runTool(t, w, tc, `{}`)
	if !tc.asked {
		t.Error("must consult the host before an exec call")
	}
	if res.IsError || inner.called != 1 {
		t.Errorf("approved call must run once, called %d err %v", inner.called, res.IsError)
	}
}

func TestWrapApprovalCarriesTheToolAndItsArguments(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	tc := &fakeCtx{approve: true}
	args := `{"command":"go test ./..."}`
	runTool(t, w, tc, args)
	if !tc.asked {
		t.Fatal("must consult the host")
	}
	if tc.last.Tool != "bash" {
		t.Errorf("ask tool = %q, want bash", tc.last.Tool)
	}
	if string(tc.last.Input) != args {
		t.Errorf("ask input = %s, want %s", tc.last.Input, args)
	}
}

func TestWrapDeniesOnRejection(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	res := runTool(t, w, &fakeCtx{approve: false}, `{}`)
	if !res.IsError || inner.called != 0 {
		t.Errorf("rejected call must not run, called %d", inner.called)
	}
}

func TestWrapCancelledAskIsDenial(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	res := runTool(t, w, &fakeCtx{cancelled: true}, `{}`)
	if !res.IsError || inner.called != 0 {
		t.Error("a cancelled prompt must deny")
	}
}

func TestWrapAllowsReadTierWithoutAsking(t *testing.T) {
	inner := &fakeTool{tier: agent.TierRead}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	tc := &fakeCtx{approve: false}
	res := runTool(t, w, tc, `{}`)
	if res.IsError || inner.called != 1 {
		t.Errorf("read tier must run without asking, called %d", inner.called)
	}
	if tc.asked {
		t.Error("read tier must not prompt")
	}
}

func TestWrapTierFollowsInput(t *testing.T) {
	inner := &inputTierTool{}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	if res := runTool(t, w, &fakeCtx{}, `"safe"`); res.IsError {
		t.Errorf("read-tier input must pass: %v", res)
	}
	if res := runTool(t, w, &fakeCtx{}, `"danger"`); !res.IsError {
		t.Error("exec-tier input must be denied without a host")
	}
}

func TestWrapAppliesRules(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	inner.specs = func(json.RawMessage) []Spec {
		return []Spec{{Kind: KindCommand, Value: "go test ./..."}}
	}
	cfg := NewPolicy(ModeDefault, Rules{Allow: []Rule{{Tool: "bash", Spec: "go test *"}}})
	w := Wrap(inner, cfg)

	res := runTool(t, w, &fakeCtx{}, `{}`)
	if res.IsError || inner.called != 1 {
		t.Error("allow rule must bypass the prompt")
	}
}

func TestWrapDenyRuleBlocksEvenInBypass(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	inner.specs = func(json.RawMessage) []Spec {
		return []Spec{{Kind: KindCommand, Value: "rm -rf /"}}
	}
	cfg := NewPolicy(ModeBypass, Rules{Deny: []Rule{{Tool: "bash", Spec: "rm *"}}})
	w := Wrap(inner, cfg)

	res := runTool(t, w, &fakeCtx{approve: true}, `{}`)
	if !res.IsError || inner.called != 0 {
		t.Error("deny rule must block even in bypass mode")
	}
}

func TestWrapForwardsIdentity(t *testing.T) {
	inner := &fakeTool{tier: agent.TierRead}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	if w.Name() != inner.Name() || w.Description() != inner.Description() {
		t.Error("identity must be forwarded unchanged")
	}
	if string(w.Schema()) != string(inner.Schema()) {
		t.Error("schema must be forwarded unchanged")
	}
	if !w.(agent.Schedulable).IsReadOnly() {
		_ = w
	}
}

func TestWrapPreservesApprovalDeclaration(t *testing.T) {
	inner := &fakeTool{tier: agent.TierWrite}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	d, ok := w.(agent.ApprovalDeclarer)
	if !ok {
		t.Fatal("wrapped tool must still declare its approval")
	}
	if d.Approval(nil).Tier != agent.TierWrite {
		t.Errorf("approval must pass through, got %q", d.Approval(nil).Tier)
	}
}

func TestRefusalIsReadableByModel(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	res := runTool(t, w, &fakeCtx{}, `{}`)
	if len(res.Content) == 0 || res.Content[0].Text == "" {
		t.Fatal("refusal must carry text the model can read")
	}
	if !res.IsError {
		t.Error("refusal must be marked as an error")
	}
}

var _ agent.Tool = (*wrapped)(nil)

var errAskFailed = errors.New("ask failed")

type failingCtx struct{ fakeCtx }

func (f *failingCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, errAskFailed
}

func TestWrapAskErrorBecomesDenial(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	res := runTool(t, w, &failingCtx{}, `{}`)
	if !res.IsError || inner.called != 0 {
		t.Error("a failing approval channel must deny the call")
	}
}

type forcingTool struct{ fakeTool }

func (f *forcingTool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierWrite, ForcePrompt: true, Reason: "touches .git"}
}

func TestWrapPassesForcePromptAndReason(t *testing.T) {
	inner := &forcingTool{}
	w := Wrap(inner, NewPolicy(ModeAcceptEdits, Rules{}))

	res := runTool(t, w, &failingCtx{}, `{}`)
	if !res.IsError || inner.called != 0 {
		t.Fatal("a forced prompt must not be auto-allowed by acceptEdits")
	}
	if !strings.Contains(res.Content[0].Text, "touches .git") {
		t.Errorf("the tool's own reason must reach the model: %q", res.Content[0].Text)
	}
}

type tierlessTool struct{ fakeTool }

func (f *tierlessTool) Approval(json.RawMessage) agent.Approval { return agent.Approval{} }

func TestWrapTreatsMissingTierAsExec(t *testing.T) {
	inner := &tierlessTool{}
	w := Wrap(inner, NewPolicy(ModeAcceptEdits, Rules{}))
	if res := runTool(t, w, &fakeCtx{}, `{}`); !res.IsError || inner.called != 0 {
		t.Error("an empty tier must fail closed as exec")
	}
}

type declaredTool struct {
	fakeTool
	meta agent.Metadata
}

func (d *declaredTool) Metadata(json.RawMessage) agent.Metadata { return d.meta }

func TestWrapApprovalCarriesDeclaredRisks(t *testing.T) {
	inner := &declaredTool{
		fakeTool: fakeTool{tier: agent.TierExec},
		meta:     agent.Metadata{Destructive: true, OpenWorld: true, MaxResultSize: 1024},
	}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	tc := &fakeCtx{approve: true}
	runTool(t, w, tc, `{"command":"rm -rf build"}`)
	if !tc.asked {
		t.Fatal("must ask before an exec call")
	}
	msg := tc.last.Message
	if !strings.Contains(msg, noteDestructive) || !strings.Contains(msg, noteOpenWorld) {
		t.Errorf("approval must state both declared risks, got %q", msg)
	}
	if strings.Contains(msg, "command:") {
		t.Errorf("the argument preview belongs in Detail, not in the message: %q", msg)
	}
	if tc.last.Detail != "command: rm -rf build" {
		t.Errorf("detail = %q, want the argument preview", tc.last.Detail)
	}
}

func TestWrapApprovalTreatsUndeclaredToolAsRisky(t *testing.T) {
	inner := &fakeTool{tier: agent.TierExec}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	tc := &fakeCtx{approve: true}
	runTool(t, w, tc, `{}`)
	if !strings.Contains(tc.last.Message, noteDestructive) || !strings.Contains(tc.last.Message, noteOpenWorld) {
		t.Errorf("an undeclared tool must look maximally risky, got %q", tc.last.Message)
	}
}

func TestWrapForwardsMetadata(t *testing.T) {
	inner := &declaredTool{
		fakeTool: fakeTool{tier: agent.TierRead},
		meta:     agent.Metadata{Destructive: true, OpenWorld: true, MaxResultSize: 1024},
	}
	w := Wrap(inner, NewPolicy(ModeDefault, Rules{}))

	d, ok := w.(agent.MetadataDeclarer)
	if !ok {
		t.Fatal("wrapped tool must still publish metadata")
	}
	if got := d.Metadata(nil); got != inner.meta {
		t.Errorf("metadata must pass through, got %+v", got)
	}
}
