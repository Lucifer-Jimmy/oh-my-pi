package hook

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func TestMain(m *testing.M) {
	if b := os.Getenv("HOOK_BEHAVIOUR"); b != "" {
		runBehaviour(b)
		return
	}
	os.Exit(m.Run())
}

func runBehaviour(b string) {
	var in In
	_ = json.NewDecoder(os.Stdin).Decode(&in)
	switch b {
	case "echo-input-as-blocked":
		_ = json.NewEncoder(os.Stdout).Encode(Out{Block: true, Reason: "blocked:" + in.Tool})
	case "rewrite":
		_ = json.NewEncoder(os.Stdout).Encode(Out{Input: json.RawMessage(`{"b":2}`)})
	case "rewrite-result":
		_ = json.NewEncoder(os.Stdout).Encode(Out{Result: &ResultOut{Text: "post:" + in.Result.Text, IsError: false}})
	case "prevent-once":
		path := os.Getenv("HOOK_COUNTER")
		data, _ := os.ReadFile(path)
		prevent := len(data) == 0
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, _ = f.WriteString("x")
			_ = f.Close()
		}
		_ = json.NewEncoder(os.Stdout).Encode(Out{Prevent: prevent, Reason: "keep going"})
	case "inject":
		_ = json.NewEncoder(os.Stdout).Encode(Out{Messages: []MessageOut{{Role: "user", Text: "injected fact"}, {Role: "system", Text: "system rule"}}})
	case "garbage":
		_, _ = os.Stdout.WriteString("this is not json")
	case "fail":
		_, _ = os.Stderr.WriteString("the hook says no\nsecond line")
		os.Exit(1)
	case "sleep":
		time.Sleep(2 * time.Second)
	case "noop":
		_ = json.NewEncoder(os.Stdout).Encode(Out{})
	case "cwd":
		wd, err := os.Getwd()
		if err != nil {
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(Out{Reason: wd})
	}
}

func spec(t *testing.T, behaviour string, timeout time.Duration) Spec {
	t.Helper()
	return Spec{
		Command: os.Args[0],
		Env:     map[string]string{"HOOK_BEHAVIOUR": behaviour},
		Timeout: timeout,
	}
}

func TestBeforeToolCallBlockAndRewrite(t *testing.T) {
	set := &Set{PreTool: []Spec{spec(t, "echo-input-as-blocked", 0)}}
	d, err := set.BeforeToolCall(context.Background(), "bash", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !d.Block || !strings.Contains(d.Reason, "blocked:bash") {
		t.Fatalf("decision = %+v", d)
	}

	set = &Set{PreTool: []Spec{spec(t, "rewrite", 0)}}
	d, err = set.BeforeToolCall(context.Background(), "read", json.RawMessage(`{"a":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Block || string(d.NewInput) != `{"b":2}` {
		t.Fatalf("decision = %+v input=%s", d, d.NewInput)
	}
}

func TestAfterToolCallRewritesTheResult(t *testing.T) {
	set := &Set{PostTool: []Spec{spec(t, "rewrite-result", 0)}}
	res, err := set.AfterToolCall(context.Background(), "read", agent.ToolResult{
		Content: []llm.ContentBlock{llm.TextBlock("original")},
	})
	if err != nil || res == nil {
		t.Fatalf("res=%v err=%v", res, err)
	}
	if got := textOf(*res); got != "post:original" {
		t.Errorf("result = %q", got)
	}
}

func TestOnStopPrevents(t *testing.T) {
	counter := t.TempDir() + "/count"
	set := &Set{Stop: []Spec{{Command: os.Args[0], Env: map[string]string{"HOOK_BEHAVIOUR": "prevent-once", "HOOK_COUNTER": counter}}}}
	first, err := set.OnStop(context.Background())
	if err != nil || !first.Prevent {
		t.Fatalf("first stop must be prevented: %+v %v", first, err)
	}
	second, err := set.OnStop(context.Background())
	if err != nil || second.Prevent {
		t.Fatalf("second stop must be honoured: %+v %v", second, err)
	}
}

func TestOnPromptCollectsMessagesAndMarksSystemText(t *testing.T) {
	set := &Set{Prompt: []Spec{spec(t, "inject", 0)}}
	msgs, err := set.OnPrompt(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 {
		t.Fatalf("messages = %d", len(msgs))
	}
	if got := textOf(agent.ToolResult{Content: msgs[0].Content}); got != "injected fact" {
		t.Errorf("first message = %q", got)
	}
	if got := textOf(agent.ToolResult{Content: msgs[1].Content}); !strings.Contains(got, "<hook role=\"system\">") {
		t.Errorf("a system message must keep its authority visible: %q", got)
	}
}

func TestProtocolProblemsAreErrors(t *testing.T) {
	cases := map[string]string{
		"non-JSON stdout": "garbage",
		"exit code":       "fail",
	}
	for name, behaviour := range cases {
		set := &Set{PreTool: []Spec{spec(t, behaviour, 0)}}
		if _, err := set.BeforeToolCall(context.Background(), "x", nil); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if _, err := (&Set{PreTool: []Spec{spec(t, "fail", 0)}}).BeforeToolCall(context.Background(), "x", nil); err == nil ||
		!strings.Contains(err.Error(), "the hook says no") {
		t.Errorf("stderr must be part of the error: %v", err)
	}
}

func TestTimeoutIsReported(t *testing.T) {
	set := &Set{PreTool: []Spec{spec(t, "sleep", 150*time.Millisecond)}}
	start := time.Now()
	_, err := set.BeforeToolCall(context.Background(), "x", nil)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("the timeout was not enforced: %s", elapsed)
	}
}

func TestDirSetsTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	sp := spec(t, "cwd", 0)
	sp.Dir = dir
	out, err := call(context.Background(), sp, In{Event: "pre_tool"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(out.Reason) != filepath.Clean(dir) {
		t.Errorf("hook ran in %q, want %q", out.Reason, dir)
	}
}

func TestEmptySetYieldsNoHooks(t *testing.T) {
	if h := (&Set{}).Hooks(); h.PreTool != nil || h.PostTool != nil || h.Stop != nil || h.Prompt != nil {
		t.Errorf("no configured spec means no hook is installed")
	}
	if h := (*Set)(nil).Hooks(); h.PreTool != nil {
		t.Errorf("a nil set yields zero-value hooks")
	}
	set := &Set{PreTool: []Spec{spec(t, "noop", 0)}, Stop: []Spec{spec(t, "noop", 0)}}
	h := set.Hooks()
	if h.PreTool == nil || h.Stop == nil || h.PostTool != nil || h.Prompt != nil {
		t.Errorf("only the configured families are installed: %+v", h)
	}
}

func TestNoopHookChangesNothing(t *testing.T) {
	set := &Set{PreTool: []Spec{spec(t, "noop", 0)}, PostTool: []Spec{spec(t, "noop", 0)}}
	d, err := set.BeforeToolCall(context.Background(), "x", json.RawMessage(`{"a":1}`))
	if err != nil || d.Block || len(d.NewInput) != 0 {
		t.Fatalf("a noop hook must not change the call: %+v %v", d, err)
	}
	res, err := set.AfterToolCall(context.Background(), "x", agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("same")}})
	if err != nil || textOf(*res) != "same" {
		t.Fatalf("a noop post hook must keep the result: %v %v", res, err)
	}
}
