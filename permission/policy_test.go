package permission

import (
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

func commandTool(cmd string) *fakeTool {
	return &fakeTool{tier: agent.TierExec, specs: func(json.RawMessage) []Spec { return SplitCommand(cmd) }}
}

func TestSessionApprovalIsRemembered(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{})
	first := commandTool("go test ./...")
	tc := &fakeCtx{always: true}
	if res := runTool(t, Wrap(first, policy), tc, `{}`); res.IsError || first.called != 1 {
		t.Fatalf("approved call must run: %+v", res)
	}
	if len(tc.last.Options) != 3 || !strings.Contains(tc.last.Options[1], "bash(go test *)") {
		t.Fatalf("session option must name the rule it adds: %q", tc.last.Options)
	}

	again := commandTool("go test ./agent -run TestLoop")
	tc = &fakeCtx{}
	if res := runTool(t, Wrap(again, policy), tc, `{}`); res.IsError || tc.asked {
		t.Errorf("remembered prefix must run without asking: asked=%v %+v", tc.asked, res)
	}

	other := commandTool("rm -rf build")
	tc = &fakeCtx{}
	if res := runTool(t, Wrap(other, policy), tc, `{}`); !res.IsError || !tc.asked {
		t.Error("a different command must still ask")
	}
}

func TestApproveOnceIsNotRemembered(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{})
	runTool(t, Wrap(commandTool("make"), policy), &fakeCtx{approve: true}, `{}`)
	tc := &fakeCtx{}
	runTool(t, Wrap(commandTool("make"), policy), tc, `{}`)
	if !tc.asked {
		t.Error("a one-time approval must not change the policy")
	}
}

func TestStickyAsksOfferNoSessionOption(t *testing.T) {
	ruled := NewPolicy(ModeDefault, Rules{Ask: []Rule{{Tool: "bash", Spec: "git push *"}}})
	tc := &fakeCtx{always: true}
	runTool(t, Wrap(commandTool("git push origin main"), ruled), tc, `{}`)
	if len(tc.last.Options) != 2 {
		t.Errorf("an ask rule means ask every time, got options %q", tc.last.Options)
	}

	tc = &fakeCtx{always: true}
	runTool(t, Wrap(&forcingTool{}, NewPolicy(ModeDefault, Rules{})), tc, `{}`)
	if len(tc.last.Options) != 2 {
		t.Errorf("a tool-forced prompt must not be rememberable, got %q", tc.last.Options)
	}
}

func TestOpaqueCommandOffersNoSessionOption(t *testing.T) {
	tc := &fakeCtx{}
	runTool(t, Wrap(commandTool("echo $(whoami)"), NewPolicy(ModeDefault, Rules{})), tc, `{}`)
	if len(tc.last.Options) != 2 {
		t.Errorf("an unmatchable command cannot be remembered, got %q", tc.last.Options)
	}
}

func TestSetModeTakesEffectOnTheNextCall(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{})
	tool := commandTool("go build")
	w := Wrap(tool, policy)
	if res := runTool(t, w, &fakeCtx{}, `{}`); !res.IsError {
		t.Fatal("default mode must ask and the fake denies")
	}
	policy.SetMode(ModeBypass)
	if policy.Mode() != ModeBypass {
		t.Fatal("mode not updated")
	}
	if res := runTool(t, w, &fakeCtx{}, `{}`); res.IsError || tool.called != 1 {
		t.Error("bypass must apply to an already wrapped tool")
	}
	policy.SetMode(ModePlan)
	if res := runTool(t, w, &fakeCtx{approve: true}, `{}`); !res.IsError {
		t.Error("plan mode must deny exec even when the user would approve")
	}
}

func TestSessionRules(t *testing.T) {
	path := []Spec{{Kind: KindPath, Value: "a.go"}}
	cases := []struct {
		specs []Spec
		want  []Rule
	}{
		{nil, []Rule{{Tool: "bash"}}},
		{SplitCommand("go test ./..."), []Rule{{Tool: "bash", Spec: "go test *"}}},
		{SplitCommand("git status && go test ./..."), []Rule{{Tool: "bash", Spec: "git status *"}, {Tool: "bash", Spec: "go test *"}}},
		{SplitCommand("ls -la"), []Rule{{Tool: "bash", Spec: "ls *"}}},
		{SplitCommand("python script.py"), []Rule{{Tool: "bash", Spec: "python *"}}},
		{SplitCommand("go test ./a; go test ./b"), []Rule{{Tool: "bash", Spec: "go test *"}}},
		{SplitCommand("cat $(ls)"), nil},
		{path, []Rule{{Tool: "bash"}}},
	}
	for _, c := range cases {
		if got := sessionRules("bash", c.specs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %+v, want %+v", c.specs, got, c.want)
		}
	}
}

func TestAllowRulesCoverSubcommandsIndividually(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "bash", Spec: "git status"}, {Tool: "bash", Spec: "go test *"}}}
	ok := Evaluate(Request{Tool: "bash", Tier: TierExec, Mode: ModeDefault, Rules: rules, Specs: SplitCommand("git status && go test ./...")})
	if !ok.Allowed() {
		t.Errorf("each sub-command is allowed by some rule: %+v", ok)
	}
	bad := Evaluate(Request{Tool: "bash", Tier: TierExec, Mode: ModeDefault, Rules: rules, Specs: SplitCommand("git status; rm -rf /")})
	if bad.Allowed() {
		t.Error("an unmatched sub-command must not ride on its neighbour's rule")
	}
}

func TestPolicyIsSafeForConcurrentUse(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			policy.SetMode(ModeAcceptEdits)
			policy.Allow(Rule{Tool: "bash", Spec: "go vet *"})
		}()
		go func() {
			defer wg.Done()
			runTool(t, Wrap(commandTool("go vet"), policy), &fakeCtx{approve: true}, `{}`)
		}()
	}
	wg.Wait()
	if _, rules := policy.current(); len(rules.Allow) != 1 {
		t.Errorf("duplicate session rules must collapse, got %+v", rules.Allow)
	}
}

func TestDeniesPath(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{Deny: []Rule{
		{Tool: "*", Spec: "**/.env"},
		{Tool: "bash", Spec: "**/secrets/**"},
	}})
	cases := []struct {
		tool, rel string
		want      bool
	}{
		{"grep", ".env", true},
		{"glob", "config/.env", true},
		{"glob", "a.go", false},
		{"grep", "secrets/key.pem", false},
		{"bash", "secrets/key.pem", true},
		{"read", "secrets/key.pem", false},
	}
	for _, c := range cases {
		if got := policy.DeniesPath(c.tool, c.rel); got != c.want {
			t.Errorf("%s %q: got %v, want %v", c.tool, c.rel, got, c.want)
		}
	}
}

func TestDeniesPathWithoutSpecDeniesTheTool(t *testing.T) {
	policy := NewPolicy(ModeDefault, Rules{Deny: []Rule{{Tool: "grep"}}})
	if !policy.DeniesPath("grep", "anything.go") {
		t.Error("a rule without a spec denies the whole tool")
	}
	if policy.DeniesPath("glob", "anything.go") {
		t.Error("a grep rule must not touch glob")
	}
}
