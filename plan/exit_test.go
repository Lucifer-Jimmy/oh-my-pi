package plan

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

var _ agent.Tool = (*ExitTool)(nil)

type askCtx struct {
	asked []agent.AskRequest
	reply agent.AskResponse
	err   error
}

func (c *askCtx) CWD() string                   { return "" }
func (c *askCtx) Workspaces() []string          { return nil }
func (c *askCtx) Emit(agent.Event)              {}
func (c *askCtx) RecentMessages() []llm.Message { return nil }
func (c *askCtx) Ask(_ context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	c.asked = append(c.asked, req)
	return c.reply, c.err
}

const planArgs = `{"plan": "# Add retries\n1. Wrap the client"}`

func runExit(t *testing.T, tool agent.Tool, tc *askCtx, args string) (string, bool) {
	t.Helper()
	res := tool.Execute(context.Background(), tc, json.RawMessage(args))
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestExitToolApproval(t *testing.T) {
	cases := []struct {
		answer agent.AskResponse
		mode   permission.Mode
		isErr  bool
		says   string
	}{
		{agent.AskResponse{Value: optionAcceptEdits}, permission.ModeAcceptEdits, false, "mode is now acceptEdits"},
		{agent.AskResponse{Value: optionAskEdits}, permission.ModeDefault, false, "mode is now default"},
		{agent.AskResponse{Value: optionKeep}, permission.ModePlan, true, "keep planning"},
		{agent.AskResponse{Cancelled: true}, permission.ModePlan, true, "keep planning"},
	}
	for _, c := range cases {
		policy := permission.NewPolicy(permission.ModePlan, permission.Rules{})
		tc := &askCtx{reply: c.answer}
		out, isErr := runExit(t, &ExitTool{Modes: policy}, tc, planArgs)
		if policy.Mode() != c.mode || isErr != c.isErr || !strings.Contains(out, c.says) {
			t.Errorf("answer %+v: mode %s, error %v, %q", c.answer, policy.Mode(), isErr, out)
		}
		if len(tc.asked) != 1 || tc.asked[0].Message != "# Add retries\n1. Wrap the client" ||
			tc.asked[0].Method != agent.AskSelect || len(tc.asked[0].Options) != 3 {
			t.Errorf("the plan itself must be put to the user: %+v", tc.asked)
		}
	}
}

func TestExitToolOutsidePlanMode(t *testing.T) {
	policy := permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{})
	tc := &askCtx{}
	out, isErr := runExit(t, &ExitTool{Modes: policy}, tc, planArgs)
	if isErr || len(tc.asked) != 0 || policy.Mode() != permission.ModeAcceptEdits || !strings.Contains(out, "no approval is needed") {
		t.Errorf("outside plan mode the call must pass without asking: %q, error %v, asked %d", out, isErr, len(tc.asked))
	}
}

func TestExitToolWithoutHost(t *testing.T) {
	policy := permission.NewPolicy(permission.ModePlan, permission.Rules{})
	out, isErr := runExit(t, &ExitTool{Modes: policy}, &askCtx{err: agent.ErrNoUI}, planArgs)
	if !isErr || policy.Mode() != permission.ModePlan || !strings.Contains(out, "final answer") {
		t.Errorf("without a host the plan cannot be approved and must become the answer: %q, error %v, mode %s", out, isErr, policy.Mode())
	}
}

func TestExitToolRequiresPlan(t *testing.T) {
	for _, args := range []string{`{}`, `{"plan": "  "}`, `{"plan": 3}`, ``} {
		policy := permission.NewPolicy(permission.ModePlan, permission.Rules{})
		tc := &askCtx{reply: agent.AskResponse{Value: optionAcceptEdits}}
		if _, isErr := runExit(t, &ExitTool{Modes: policy}, tc, args); !isErr || len(tc.asked) != 0 || policy.Mode() != permission.ModePlan {
			t.Errorf("%q: an empty plan must fail without asking", args)
		}
	}
}

func TestExitToolIsCallableInPlanMode(t *testing.T) {
	policy := permission.NewPolicy(permission.ModePlan, permission.Rules{})
	tool := &ExitTool{Modes: policy}
	if a := tool.Approval(nil); a.Tier != agent.TierRead || a.ForcePrompt {
		t.Errorf("presenting a plan changes nothing on disk, it must be read tier: %+v", a)
	}
	if !tool.IsReadOnly() || tool.IsConcurrencySafe(json.RawMessage(planArgs)) {
		t.Error("it must run alone, after every call before it")
	}
	tc := &askCtx{reply: agent.AskResponse{Value: optionAcceptEdits}}
	if out, isErr := runExit(t, permission.Wrap(tool, policy), tc, planArgs); isErr || len(tc.asked) != 1 {
		t.Errorf("the plan policy must let it through: %q", out)
	}
	if !json.Valid(tool.Schema()) {
		t.Error("schema is not valid JSON")
	}
}

func TestExitSchemaMatchesArguments(t *testing.T) {
	var s struct {
		Type       string `json:"type"`
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties *bool    `json:"additionalProperties"`
	}
	if err := json.Unmarshal((&ExitTool{}).Schema(), &s); err != nil {
		t.Fatal(err)
	}
	if s.Type != "object" || s.AdditionalProperties == nil || *s.AdditionalProperties {
		t.Error("schema must be a closed object")
	}
	if len(s.Properties) != 1 || s.Properties["plan"].Type != "string" || len(s.Required) != 1 || s.Required[0] != "plan" {
		t.Errorf("schema must take exactly one required string plan: %+v", s)
	}
	var a exitArgs
	if err := json.Unmarshal([]byte(`{"plan":"steps"}`), &a); err != nil || a.Plan != "steps" {
		t.Errorf("exitArgs must decode the schema's plan: %+v %v", a, err)
	}
	if n := reflect.TypeFor[exitArgs]().NumField(); n != 1 {
		t.Errorf("exitArgs has %d fields, the schema has one property", n)
	}
}
