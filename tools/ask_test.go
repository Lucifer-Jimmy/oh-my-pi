package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

type askCtx struct {
	fakeCtx
	req agent.AskRequest
	res agent.AskResponse
	err error
}

func (c *askCtx) Ask(ctx context.Context, req agent.AskRequest) (agent.AskResponse, error) {
	c.req = req
	return c.res, c.err
}

func runAsk(t *testing.T, c *askCtx, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res := (&Ask{}).Execute(context.Background(), c, raw)
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestAskFreeFormQuestion(t *testing.T) {
	c := &askCtx{res: agent.AskResponse{Value: "use postgres"}}
	got, isErr := runAsk(t, c, map[string]any{"question": "which database?"})
	if isErr {
		t.Fatalf("a successful ask must not be an error: %q", got)
	}
	if c.req.Method != agent.AskInput || c.req.Message != "which database?" {
		t.Errorf("request = %+v", c.req)
	}
	if !strings.Contains(got, "use postgres") {
		t.Errorf("the answer must reach the model: %q", got)
	}
}

func TestAskWithOptionsIsSelect(t *testing.T) {
	c := &askCtx{res: agent.AskResponse{Value: "b"}}
	runAsk(t, c, map[string]any{"question": "pick", "options": []string{"a", "b"}})
	if c.req.Method != agent.AskSelect || len(c.req.Options) != 2 {
		t.Errorf("request = %+v", c.req)
	}
}

func TestAskWithoutHostTellsTheModelToAssume(t *testing.T) {
	c := &askCtx{err: agent.ErrNoUI}
	got, isErr := runAsk(t, c, map[string]any{"question": "which database?"})
	if !isErr {
		t.Fatalf("no host means the call fails closed")
	}
	if !strings.Contains(got, "assumption") {
		t.Errorf("the failure must tell the model how to proceed: %q", got)
	}
}

func TestAskCancelledTellsTheModelToAssume(t *testing.T) {
	c := &askCtx{res: agent.AskResponse{Cancelled: true}}
	got, isErr := runAsk(t, c, map[string]any{"question": "which?"})
	if !isErr || !strings.Contains(got, "assumption") {
		t.Errorf("a dismissed question must tell the model to assume: isErr=%v %q", isErr, got)
	}
}

func TestAskNeedsAQuestion(t *testing.T) {
	c := &askCtx{err: errors.New("must not be called")}
	got, isErr := runAsk(t, c, map[string]any{"question": "   "})
	if !isErr || !strings.Contains(got, "question is required") {
		t.Errorf("got isErr=%v %q", isErr, got)
	}
}

func TestAskIsReadOnlyAndExclusive(t *testing.T) {
	a := &Ask{}
	if !a.IsReadOnly() || a.IsConcurrencySafe(nil) {
		t.Errorf("a question must be read tier and never run in parallel")
	}
	if a.Approval(nil).Tier != agent.TierRead {
		t.Errorf("approval tier = %v", a.Approval(nil).Tier)
	}
}
