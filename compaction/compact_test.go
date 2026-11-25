package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type fakeModel struct {
	reply  string
	err    error
	calls  []llm.CompletionRequest
	filter func(call int) error
}

func (f *fakeModel) complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.Usage, error) {
	f.calls = append(f.calls, req)
	fail := f.err
	if f.filter != nil {
		fail = f.filter(len(f.calls))
	}
	if fail != nil {
		return llm.Message{}, llm.Usage{}, fail
	}
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(f.reply)}}, llm.Usage{InputTokens: 100, OutputTokens: 50}, nil
}

func bigText(n int) string { return strings.Repeat("x", n) }

func conversation(turns int, resultBytes int) []llm.Message {
	msgs := []llm.Message{llm.UserText("start")}
	for i := 0; i < turns; i++ {
		id := fmt.Sprintf("t%d", i)
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
				llm.TextBlock(fmt.Sprintf("step %d", i)),
				{Type: llm.BlockToolUse, ID: id, Name: "read", Input: []byte(`{"path":"a.go"}`)},
			}},
			llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{
				llm.ToolResultText(id, fmt.Sprintf("line %d\n", i)+bigText(resultBytes), false),
			}},
		)
	}
	return msgs
}

func newTest(window int, m *fakeModel) *Compactor {
	return New(Config{Window: func() int { return window }}, m.complete)
}

func TestThresholdMatchesOmp(t *testing.T) {
	cases := map[int]int{200_000: 170_000, 128_000: 108_800, 100_000: 83_616}
	for window, want := range cases {
		if got := newTest(window, &fakeModel{}).Threshold(); got != want {
			t.Errorf("window %d: threshold %d, want %d", window, got, want)
		}
	}
	if got := newTest(0, &fakeModel{}).Threshold(); got != 0 {
		t.Errorf("an unknown window has no threshold, got %d", got)
	}
	explicit := New(Config{Window: func() int { return 200_000 }, ReserveTokens: 50_000}, (&fakeModel{}).complete)
	if got := explicit.Threshold(); got != 150_000 {
		t.Errorf("an explicit reserve wins, got %d", got)
	}
}

func TestAutoCompactionLeavesASmallContextAlone(t *testing.T) {
	m := &fakeModel{reply: "unused"}
	c := newTest(200_000, m)
	msgs := conversation(3, 2000)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerAuto}
	view, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp != nil {
		t.Fatalf("nothing to do: cp=%v err=%v", cp, err)
	}
	if len(view) != len(msgs) || len(m.calls) != 0 {
		t.Errorf("the view is untouched and no model call happens: %d msgs, %d calls", len(view), len(m.calls))
	}
	if !strings.Contains(resultText(view[len(view)-1].Content[0].Content), "line 2") {
		t.Error("tool results below the elide floor stay intact")
	}
}

func TestElisionReclaimsReproducibleResults(t *testing.T) {
	m := &fakeModel{reply: "unused"}
	c := newTest(200_000, m)
	msgs := conversation(40, 20_000)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerAuto}
	view, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp == nil {
		t.Fatalf("a large context compacts: cp=%v err=%v", cp, err)
	}
	if len(m.calls) != 0 {
		t.Errorf("elision alone is enough here, got %d summarizer calls", len(m.calls))
	}
	if cp.Summary != "" {
		t.Error("no summary is written when elision suffices")
	}
	if cp.TokensAfter >= cp.TokensBefore {
		t.Errorf("compaction must shrink the projection: %d → %d", cp.TokensBefore, cp.TokensAfter)
	}
	if cp.ElideBefore <= 0 || cp.Elided == 0 {
		t.Fatalf("expected an elide boundary and elided results, got %+v", cp)
	}
	first := view[2]
	text := resultText(first.Content[0].Content)
	if !strings.Contains(text, "read succeeded") || !strings.Contains(text, "re-run read") {
		t.Errorf("a reproducible result is elided with a re-read hint, got %q", text)
	}
	if strings.Contains(text, bigText(100)) {
		t.Error("the body must be gone")
	}
	errMsgs := conversation(1, 20_000)
	errMsgs[len(errMsgs)-1].Content[0].IsError = true
	c.minElide = 1
	view, _ = c.project(errMsgs, session.Compaction{ElideBefore: len(errMsgs)})
	if !view[len(view)-1].Content[0].IsError || !strings.Contains(resultText(view[len(view)-1].Content[0].Content), "failed") {
		t.Error("the error flag and outcome survive elision")
	}
}

func TestRecentResultsAreNeverElided(t *testing.T) {
	m := &fakeModel{reply: "unused"}
	c := newTest(200_000, m)
	msgs := conversation(40, 20_000)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerAuto}
	view, cp, _ := c.Compact(context.Background(), req)
	for i := cp.ElideBefore; i < len(msgs); i++ {
		block := view[i].Content[0]
		if block.Type != llm.BlockToolResult {
			continue
		}
		if got := resultText(block.Content); !strings.Contains(got, bigText(100)) {
			t.Fatalf("message %d is inside the recent window and must stay intact, got %q", i, got)
		}
	}
}

func TestNonReproducibleResultsKeepTheirTail(t *testing.T) {
	m := &fakeModel{reply: "unused"}
	c := New(Config{Window: func() int { return 200_000 }, KeepRecentTokens: 1000, MinElideTokens: 1}, m.complete)
	msgs := []llm.Message{
		llm.UserText("run the tests"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			llm.TextBlock("running"),
			{Type: llm.BlockToolUse, ID: "b1", Name: "bash", Input: []byte(`{"command":"go build ./..."}`)},
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{
			llm.ToolResultText("b1", "go test output\n"+strings.Repeat("verbose line\n", 2000)+"FAIL: exit 1", true),
		}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(strings.Repeat("y", 4000))}},
	}
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerManual}
	view, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp == nil || cp.Elided != 1 || cp.Summary != "" {
		t.Fatalf("elision alone must handle this: cp=%+v err=%v", cp, err)
	}
	block := view[2].Content[0]
	text := resultText(block.Content)
	if !strings.Contains(text, "bash failed") {
		t.Errorf("the outcome and the tool name stay, got %q", text)
	}
	if !strings.Contains(text, "FAIL: exit 1") || !strings.Contains(text, "verbose line") {
		t.Errorf("a non-reproducible result keeps its last lines, got %q", text)
	}
	if strings.Contains(text, strings.Repeat("verbose line\n", 20)) {
		t.Error("the body must be gone")
	}
	if !block.IsError {
		t.Error("the error flag survives elision")
	}
}

func TestSummaryIsSplicedInFrontOfTheKeptTail(t *testing.T) {
	m := &fakeModel{reply: "## Goal\nship it"}
	c := newTest(10_000, m)
	msgs := conversation(60, 3000)
	messages := append([]llm.Message(nil), msgs...)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: messages}, Trigger: session.TriggerManual}
	view, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp == nil {
		t.Fatalf("manual compaction: %v", err)
	}
	if len(m.calls) != 1 {
		t.Fatalf("one summarizer call, got %d", len(m.calls))
	}
	sent := m.calls[0]
	if len(sent.Tools) != 0 {
		t.Error("the summarizer gets no tools")
	}
	if len(sent.System) != 1 || sent.System[0] != summarySystem {
		t.Error("the summarizer gets its own system prompt")
	}
	if !strings.Contains(sent.Messages[0].Text(), "<conversation>") {
		t.Error("the conversation is serialized inside tags")
	}
	if cp.Summary != m.reply || cp.FirstKept == 0 || cp.Summarized == 0 {
		t.Errorf("the checkpoint names the summary and the kept tail: %+v", cp)
	}
	head := view[0]
	if head.Role != llm.RoleUser || !strings.Contains(head.Content[0].Text, m.reply) {
		t.Errorf("the summary leads the view as a user message, got %+v", head)
	}
	if !strings.Contains(head.Content[0].Text, summaryContext[:20]) {
		t.Error("the summary is wrapped so the model knows what it is reading")
	}
	if cp.TokensAfter >= cp.TokensBefore {
		t.Errorf("summarizing must shrink the projection: %d → %d", cp.TokensBefore, cp.TokensAfter)
	}
	if cp.Usage.InputTokens != 100 {
		t.Errorf("the summarizer's usage is recorded, got %+v", cp.Usage)
	}
}

func TestSecondCompactionUpdatesTheFirstSummary(t *testing.T) {
	m := &fakeModel{reply: "## Goal\nsecond"}
	c := newTest(10_000, m)
	msgs := conversation(60, 3000)
	first := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerManual}
	_, cp, err := c.Compact(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	msgs = append(msgs, conversation(10, 3000)...)
	second := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Last: *cp, Trigger: session.TriggerAuto}
	_, next, err := c.Compact(context.Background(), second)
	if err != nil || next == nil {
		t.Fatalf("the second pass compacts: %v", err)
	}
	prompt := m.calls[len(m.calls)-1].Messages[0].Text()
	if !strings.Contains(prompt, "<previous-summary>") || !strings.Contains(prompt, cp.Summary) {
		t.Errorf("the second pass builds on the previous summary, got %q", prompt)
	}
	if !strings.Contains(prompt, "Update the summary") {
		t.Error("the update prompt is used when a summary exists")
	}
	if next.Summary != "## Goal\nsecond" {
		t.Errorf("the new summary replaces the old one, got %q", next.Summary)
	}
	if next.FirstKept <= cp.FirstKept {
		t.Errorf("the kept tail advances: %d → %d", cp.FirstKept, next.FirstKept)
	}
}

func TestSummaryKeepsTheEndOfTheToolsAndErrors(t *testing.T) {
	region := []llm.Message{
		llm.UserText("fix the build"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockToolUse, ID: "1", Name: "bash", Input: []byte(`{"command":"go build ./..."}`)},
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("1", bigText(40_000)+"\nFAIL: undefined: x", true)}},
	}
	prompt := summaryPrompt(region, "", "")
	if !strings.Contains(prompt, "[User]: fix the build") || !strings.Contains(prompt, "[Tool Call]: bash") {
		t.Error("the summarizer sees roles and tool calls")
	}
	if strings.Contains(prompt, bigText(40_000)) {
		t.Error("huge tool results are clipped in the summary input")
	}
	if !strings.Contains(prompt, "FAIL: undefined: x") {
		t.Error("the end of a clipped result survives")
	}
	if !strings.Contains(prompt, "... (") {
		t.Error("the clip says how much was dropped")
	}
	if !strings.Contains(prompt, "<conversation>") || !strings.Contains(prompt, "</conversation>") {
		t.Error("the conversation is fenced so embedded tags cannot escape it")
	}
	if !strings.Contains(prompt, "Additional focus:") {
		return
	}
}

func TestSummarizerOverflowDropsOldestFirst(t *testing.T) {
	m := &fakeModel{reply: "ok", err: errors.New("prompt is too long: 300000 tokens > 200000 maximum")}
	m.filter = func(call int) error {
		if call < 3 {
			return m.err
		}
		return nil
	}
	c := newTest(1000, m)
	msgs := conversation(60, 3000)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerManual}
	_, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp == nil {
		t.Fatalf("a shrinking retry succeeds: %v", err)
	}
	if len(m.calls) < 3 {
		t.Fatalf("expected retries, got %d calls", len(m.calls))
	}
	first := m.calls[0].Messages[0].Text()
	last := m.calls[2].Messages[0].Text()
	if len(last) >= len(first) {
		t.Error("each overflow retry drops the oldest messages")
	}
}

func TestSummarizerFailureIsReported(t *testing.T) {
	m := &fakeModel{err: errors.New("529 overloaded")}
	c := newTest(1000, m)
	msgs := conversation(60, 3000)
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerManual}
	_, cp, err := c.Compact(context.Background(), req)
	if err == nil || cp != nil {
		t.Fatalf("a failed summary is an error and records nothing: cp=%v err=%v", cp, err)
	}
	if len(m.calls) != 1 {
		t.Errorf("a non-overflow error is not retried, got %d calls", len(m.calls))
	}
}

func TestNothingToCompactReportsNil(t *testing.T) {
	m := &fakeModel{reply: "x"}
	c := newTest(200_000, m)
	msgs := []llm.Message{llm.UserText("hi")}
	req := agent.CompactRequest{Request: llm.CompletionRequest{Messages: msgs}, Trigger: session.TriggerManual}
	_, cp, err := c.Compact(context.Background(), req)
	if err != nil || cp != nil {
		t.Errorf("a conversation of one message has nothing to compact: %v", err)
	}
	if len(m.calls) != 0 {
		t.Error("no model call for a one-message conversation")
	}
}
