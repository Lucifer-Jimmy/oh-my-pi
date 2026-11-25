package presentation

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type script struct {
	s   *State
	seq int64
}

func (sc *script) send(ev agent.Event) {
	sc.seq++
	ev.Seq = sc.seq
	ev.Timestamp = sc.seq * 1000
	sc.s.Apply(ev)
}

func newScript(prompt string) *script {
	s := &State{}
	s.Submit(prompt, 0)
	return &script{s: s}
}

func toolResult(text string, isErr bool) *agent.ToolResult {
	return &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}, IsError: isErr}
}

func kinds(items []Item) string {
	var out []string
	for _, it := range items {
		out = append(out, string(it.Kind))
	}
	return strings.Join(out, ",")
}

func TestTurnWithToolCall(t *testing.T) {
	sc := newScript("list files")
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "Let me "})
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "look."})
	sc.send(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 100, OutputTokens: 10}})
	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "glob", ToolInput: json.RawMessage(`{"pattern":"*.go"}`)})
	sc.send(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: toolResult("a.go\nb.go", false)})
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "Two files."})
	sc.send(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 20, CacheReadTokens: 100, OutputTokens: 5}})
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted}})

	s := sc.s
	if got := kinds(s.Items); got != "user,text,tool,text" {
		t.Fatalf("items = %s", got)
	}
	if s.Items[1].Text != "Let me look." || !s.Items[1].Done {
		t.Errorf("first text item wrong: %+v", s.Items[1])
	}
	tool := s.Items[2]
	if !tool.Done || tool.Tool.Status != StatusDone || tool.Tool.Output != "a.go\nb.go" || tool.Tool.Name != "glob" {
		t.Errorf("tool item wrong: %+v %+v", tool, tool.Tool)
	}
	if tool.Started != 4000 || tool.Ended != 5000 {
		t.Errorf("tool timing wrong: %d..%d", tool.Started, tool.Ended)
	}
	if !s.Items[3].Done {
		t.Error("text must be closed by the result")
	}
	if s.Running || s.Reason != agent.ReasonCompleted {
		t.Errorf("run must be finished: running=%v reason=%q", s.Running, s.Reason)
	}
	if s.Usage.InputTokens != 120 || s.Usage.OutputTokens != 15 || s.Usage.CacheReadTokens != 100 {
		t.Errorf("usage must accumulate: %+v", s.Usage)
	}
	if s.Context != 125 {
		t.Errorf("context must be the size of the last request plus its output, got %d", s.Context)
	}
}

func TestProgressAttachesToTheRunningToolOnly(t *testing.T) {
	sc := newScript("delegate")
	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "task", ToolInput: json.RawMessage(`{"prompt":"x"}`)})
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "while "})
	sc.send(agent.Event{Kind: agent.EventProgress, ToolUseID: "t1", ToolName: "grep", ToolInput: json.RawMessage(`{"pattern":"y"}`), Usage: &llm.Usage{InputTokens: 100, OutputTokens: 20}})
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "waiting"})
	sc.send(agent.Event{Kind: agent.EventProgress, ToolUseID: "nope", Message: "stray"})
	sc.send(agent.Event{Kind: agent.EventProgress, Message: "anonymous"})

	s := sc.s
	if got := kinds(s.Items); got != "user,tool,text" {
		t.Fatalf("progress must not add or split items: %s", got)
	}
	if s.Items[2].Text != "while waiting" || s.Items[2].Done {
		t.Errorf("the streaming text must stay one open item: %+v", s.Items[2])
	}
	tool := s.Items[1].Tool
	if p := tool.Progress; p == nil || p.Name != "grep" || string(p.Input) != `{"pattern":"y"}` || p.Tokens != 120 {
		t.Fatalf("the running card must carry the latest report: %+v", p)
	}

	sc.send(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: toolResult("found", false), DurationMs: 2300})
	if tool.Progress != nil || tool.DurationMs != 2300 || tool.Status != StatusDone {
		t.Errorf("a finished card drops its live report and keeps the measured time: %+v", tool)
	}

	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t2", ToolName: "task"})
	sc.send(agent.Event{Kind: agent.EventProgress, ToolUseID: "t2", ToolName: "read"})
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonAborted}})
	cut := s.Items[len(s.Items)-2].Tool
	if cut.UseID != "t2" || cut.Status != StatusCancelled || cut.Progress != nil {
		t.Errorf("a cancelled card must not keep reporting live activity: %+v", cut)
	}
}

func TestThinkingAndTextAreSeparateItems(t *testing.T) {
	sc := newScript("q")
	sc.send(agent.Event{Kind: agent.EventThinkingDelta, Text: "hmm"})
	sc.send(agent.Event{Kind: agent.EventThinkingDelta, Text: " ok"})
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "answer"})
	if got := kinds(sc.s.Items); got != "user,thinking,text" {
		t.Fatalf("items = %s", got)
	}
	th := sc.s.Items[1]
	if th.Text != "hmm ok" || !th.Done || th.Ended != 3000 {
		t.Errorf("thinking must close when text starts: %+v", th)
	}
	if sc.s.Items[2].Done {
		t.Error("text is still streaming")
	}
}

func TestDuplicateAndStaleEventsAreIgnored(t *testing.T) {
	s := &State{}
	s.Submit("q", 0)
	s.Apply(agent.Event{Seq: 1, Kind: agent.EventTextDelta, Text: "a"})
	s.Apply(agent.Event{Seq: 1, Kind: agent.EventTextDelta, Text: "a"})
	s.Apply(agent.Event{Seq: 2, Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "read"})
	s.Apply(agent.Event{Seq: 3, Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: toolResult("first", false)})
	s.Apply(agent.Event{Seq: 4, Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: toolResult("second", true)})
	s.Apply(agent.Event{Seq: 2, Kind: agent.EventTextDelta, Text: "late"})

	if s.Items[1].Text != "a" {
		t.Errorf("duplicate delta applied: %q", s.Items[1].Text)
	}
	tool := s.Items[2].Tool
	if tool.Output != "first" || tool.Status != StatusDone {
		t.Errorf("second result for the same call must be ignored: %+v", tool)
	}
	if len(s.Items) != 3 {
		t.Errorf("stale event created an item: %s", kinds(s.Items))
	}
}

func TestAbortCancelsRunningToolsAndAddsNotice(t *testing.T) {
	sc := newScript("q")
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "partial"})
	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "bash"})
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonAborted, Err: "context canceled"}})

	s := sc.s
	if got := kinds(s.Items); got != "user,text,tool,notice" {
		t.Fatalf("items = %s", got)
	}
	if s.Items[1].Text != "partial" || !s.Items[1].Done {
		t.Errorf("partial text must be kept and closed: %+v", s.Items[1])
	}
	if tool := s.Items[2]; !tool.Done || tool.Tool.Status != StatusCancelled {
		t.Errorf("running tool must be marked cancelled: %+v", tool.Tool)
	}
	if n := s.Items[3]; n.Level != LevelWarn || n.Text != "Interrupted" {
		t.Errorf("abort notice wrong: %+v", n)
	}
	if s.Running || s.Reason != agent.ReasonAborted {
		t.Errorf("run state wrong: %v %q", s.Running, s.Reason)
	}
}

func TestErrorResultBecomesErrorNotice(t *testing.T) {
	sc := newScript("q")
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonError, Err: "401 unauthorized"}})
	n := sc.s.Items[len(sc.s.Items)-1]
	if n.Kind != KindNotice || n.Level != LevelError || n.Text != "401 unauthorized" {
		t.Errorf("error notice wrong: %+v", n)
	}

	sc = newScript("q")
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonError}})
	if n := sc.s.Items[len(sc.s.Items)-1]; n.Text == "" {
		t.Error("an error without text must still say something")
	}
}

func TestFailedToolResult(t *testing.T) {
	sc := newScript("q")
	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "bash"})
	sc.send(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", ToolResult: toolResult("bash: user denied the call", true)})
	if tool := sc.s.Items[1].Tool; tool.Status != StatusFailed || tool.Output != "bash: user denied the call" {
		t.Errorf("failed tool wrong: %+v", tool)
	}
}

func TestNextRunRestartsSequence(t *testing.T) {
	sc := newScript("one")
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "first"})
	sc.send(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted}})

	sc.s.Submit("two", 9000)
	sc.seq = 0
	sc.send(agent.Event{Kind: agent.EventTextDelta, Text: "second"})
	if got := kinds(sc.s.Items); got != "user,text,user,text" {
		t.Fatalf("items = %s", got)
	}
	if sc.s.Items[3].Text != "second" || !sc.s.Running {
		t.Errorf("second run must apply from seq 1: %+v running=%v", sc.s.Items[3], sc.s.Running)
	}
	ids := map[int]bool{}
	for _, it := range sc.s.Items {
		if ids[it.ID] {
			t.Errorf("item IDs must be unique, %d repeats", it.ID)
		}
		ids[it.ID] = true
	}
}

func TestResetKeepsIDsUnique(t *testing.T) {
	s := &State{}
	s.Submit("a", 0)
	first := s.Items[0].ID
	s.Reset()
	if len(s.Items) != 0 || s.Running {
		t.Fatalf("reset must clear the transcript: %+v", s)
	}
	s.Submit("b", 0)
	if s.Items[0].ID == first {
		t.Error("IDs must not be reused after reset")
	}
}

func TestStateSnapshotIsJSON(t *testing.T) {
	sc := newScript("q")
	sc.send(agent.Event{Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "read", ToolInput: json.RawMessage(`{"path":"a.go"}`)})
	b, err := json.Marshal(sc.s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"kind":"tool"`, `"use_id":"t1"`, `"status":"running"`, `"running":true`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("snapshot missing %s: %s", want, b)
		}
	}
}

func TestReducerIsBlindWithoutAProjector(t *testing.T) {
	var s State
	s.Submit("go", 0)
	s.Apply(agent.Event{Seq: 1, Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "todo",
		ToolInput: json.RawMessage(`{"items":[{"content":"one","status":"completed"}]}`)})
	if len(s.Todos) != 0 {
		t.Fatalf("the reducer must not know what a todo is, got %+v", s.Todos)
	}
	if len(s.Items) != 2 {
		t.Fatalf("the tool item is still recorded, got %d items", len(s.Items))
	}
}

func TestProjectorFeedsThePlanStrip(t *testing.T) {
	s := State{Projectors: map[string]ToolProjector{"todo": func(s *State, raw json.RawMessage) {
		var a struct {
			Items []TodoItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || len(a.Items) == 0 {
			return
		}
		s.Todos = a.Items
	}}}
	s.Submit("go", 0)
	raw := json.RawMessage(`{"items":[{"content":"one","status":"completed"},{"content":"two","status":"in_progress"}]}`)
	s.Apply(agent.Event{Seq: 1, Kind: agent.EventToolUse, ToolUseID: "t1", ToolName: "todo", ToolInput: raw})
	if len(s.Todos) != 2 || s.Todos[1].Status != "in_progress" {
		t.Fatalf("todos = %+v", s.Todos)
	}
	s.Apply(agent.Event{Seq: 2, Kind: agent.EventToolUse, ToolUseID: "t2", ToolName: "todo", ToolInput: json.RawMessage(`{"items":[]}`)})
	if len(s.Todos) != 2 {
		t.Fatalf("a bad update must keep the previous list, got %+v", s.Todos)
	}
	s.Apply(agent.Event{Seq: 3, Kind: agent.EventToolUse, ToolUseID: "t3", ToolName: "read"})
	if len(s.Todos) != 2 {
		t.Fatalf("an unrelated tool must not touch todos")
	}
	s.Reset()
	if len(s.Todos) != 0 {
		t.Fatalf("Reset clears the strip")
	}
	if s.Projectors["todo"] == nil {
		t.Fatalf("Reset must keep the projectors, or a cleared screen stops tracking todos")
	}
}

func TestReplayRebuildsTheTranscript(t *testing.T) {
	s := &State{
		InjectedTags: []string{"system-reminder", "goal_continuation", "background-task"},
		Projectors: map[string]ToolProjector{"todo": func(s *State, raw json.RawMessage) {
			var a struct {
				Items []TodoItem `json:"items"`
			}
			if json.Unmarshal(raw, &a) == nil {
				s.Todos = a.Items
			}
		}},
	}
	first := llm.Usage{InputTokens: 100, OutputTokens: 10}
	second := llm.Usage{InputTokens: 300, OutputTokens: 20, CacheReadTokens: 50}
	msgs := []llm.Message{
		{Role: llm.RoleUser, Timestamp: 1, Content: []llm.ContentBlock{llm.TextBlock("<system-reminder>\nplan mode\n</system-reminder>\n\nfix the bug")}},
		{Role: llm.RoleAssistant, Timestamp: 2, Usage: &first, Content: []llm.ContentBlock{
			llm.ThinkingBlock("let me look", "sig"),
			llm.TextBlock("Reading it."),
			{Type: llm.BlockToolUse, ID: "r1", Name: "read", Input: json.RawMessage(`{"path":"a.go"}`)},
			{Type: llm.BlockToolUse, ID: "t1", Name: "todo", Input: json.RawMessage(`{"items":[{"content":"fix","status":"in_progress"}]}`)},
			{Type: llm.BlockToolUse, ID: "x1", Name: "bash", Input: json.RawMessage(`{"command":"go test"}`)},
		}},
		{Role: llm.RoleUser, Timestamp: 3, Content: []llm.ContentBlock{
			llm.ToolResultText("r1", "1\tpackage a", false),
			llm.ToolResultText("t1", "ok", false),
		}},
		{Role: llm.RoleUser, Timestamp: 4, Content: []llm.ContentBlock{llm.TextBlock("<background-task id=\"bg-1\">\ndone\n</background-task>")}},
		{Role: llm.RoleUser, Timestamp: 5, Content: []llm.ContentBlock{llm.TextBlock("<goal_continuation>\ncontinue\n</goal_continuation>")}},
		{Role: llm.RoleAssistant, Timestamp: 6, Usage: &second, Content: []llm.ContentBlock{llm.TextBlock("**Fixed.**")}},
	}
	s.Replay(msgs)

	var kinds []string
	for _, it := range s.Items {
		kinds = append(kinds, string(it.Kind))
		if !it.Done {
			t.Errorf("item %d (%s) is not done after a replay", it.ID, it.Kind)
		}
	}
	want := "user thinking text tool tool tool text"
	if got := strings.Join(kinds, " "); got != want {
		t.Fatalf("kinds = %q, want %q", got, want)
	}
	if s.Items[0].Text != "fix the bug" {
		t.Errorf("injected prefix was not stripped: %q", s.Items[0].Text)
	}
	if s.Items[1].Text != "let me look" || s.Items[2].Text != "Reading it." {
		t.Errorf("assistant blocks = %q, %q", s.Items[1].Text, s.Items[2].Text)
	}
	read, todo, bash := s.Items[3].Tool, s.Items[4].Tool, s.Items[5].Tool
	if read.Status != StatusDone || read.Output != "1\tpackage a" {
		t.Errorf("read = %+v", read)
	}
	if todo.Status != StatusDone {
		t.Errorf("todo = %+v", todo)
	}
	if bash.Status != StatusCancelled {
		t.Errorf("a call with no stored result must replay as cancelled, got %s", bash.Status)
	}
	if len(s.Todos) != 1 || s.Todos[0].Content != "fix" {
		t.Errorf("todos were not projected: %+v", s.Todos)
	}
	if s.Usage.InputTokens != 400 || s.Usage.OutputTokens != 30 || s.Usage.CacheReadTokens != 50 {
		t.Errorf("usage = %+v", s.Usage)
	}
	if s.Context != second.Total() {
		t.Errorf("context = %d, want the last response's %d", s.Context, second.Total())
	}
	if s.Running || s.Reason != "" {
		t.Errorf("a replay must not leave a run open: running=%v reason=%q", s.Running, s.Reason)
	}
}

func TestReplayKeepsWhatTheUserTyped(t *testing.T) {
	s := &State{InjectedTags: []string{"system-reminder"}}
	s.Replay([]llm.Message{
		llm.UserText("<config>\nkey: 1\n</config>\n\nwhat is this?"),
		llm.UserText("<system-reminderx>not ours</system-reminderx>"),
		llm.UserText("<system-reminder>unclosed"),
	})
	if len(s.Items) != 3 {
		t.Fatalf("items = %+v", s.Items)
	}
	if !strings.HasPrefix(s.Items[0].Text, "<config>") {
		t.Errorf("an unknown tag is the user's text: %q", s.Items[0].Text)
	}
	if !strings.HasPrefix(s.Items[1].Text, "<system-reminderx>") {
		t.Errorf("a longer tag name must not match: %q", s.Items[1].Text)
	}
	if s.Items[2].Text != "<system-reminder>unclosed" {
		t.Errorf("an unclosed block must be kept: %q", s.Items[2].Text)
	}
}
