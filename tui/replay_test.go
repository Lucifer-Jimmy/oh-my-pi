package tui

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func storedSession() *session.Memory {
	usage := llm.Usage{InputTokens: 1200, OutputTokens: 80}
	return session.NewMemory([]llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock("<system-reminder>\nplan mode\n</system-reminder>\n\nwhere is the loop?")}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockToolUse, ID: "g1", Name: "grep", Input: json.RawMessage(`{"pattern":"func drive"}`)},
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("g1", "agent/loop.go:111:func drive", false)}},
		{Role: llm.RoleAssistant, Usage: &usage, Content: []llm.ContentBlock{llm.TextBlock("It is in **agent/loop.go**.")}},
	})
}

func startWith(t *testing.T, opts Options) *harness {
	t.Helper()
	h := &harness{t: t}
	if opts.Runner == nil {
		opts.Runner = &scriptRunner{}
	}
	opts.Model, opts.CWD = "gw/m1", "/work"
	opts.Policy = permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{})
	h.m = newModel(context.Background(), opts)
	h.m.print = func(text string) tea.Cmd {
		h.printed = append(h.printed, ansi.Strip(text))
		return nil
	}
	h.m.Init()
	h.settle()
	return h
}

func TestStartupReplaysTheStoredConversation(t *testing.T) {
	h := startWith(t, Options{Session: storedSession(), InjectedTags: []string{"system-reminder"}})
	out := h.out()
	for _, want := range []string{"where is the loop?", "Grep(func drive)", "It is in agent/loop.go."} {
		if !strings.Contains(out, want) {
			t.Errorf("replayed transcript is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "system-reminder") || strings.Contains(out, "plan mode") {
		t.Errorf("an injected reminder leaked into the transcript:\n%s", out)
	}
	if !strings.Contains(h.view(), "↑1.2k ↓80") {
		t.Errorf("the status bar did not restore the token counters:\n%s", h.view())
	}
}

func TestResumeReplaysTheChosenConversation(t *testing.T) {
	stored := storedSession()
	h := startWith(t, Options{
		Resume: func(arg string) (session.Session, string, error) { return stored, "abc123", nil },
	})
	h.printed = nil
	h.submit("/resume abc")
	out := h.out()
	if !strings.Contains(out, "resumed abc123") || !strings.Contains(out, "It is in agent/loop.go.") {
		t.Fatalf("resume did not replay the conversation:\n%s", out)
	}
	if strings.Index(out, "resumed abc123") > strings.Index(out, "where is the loop?") {
		t.Errorf("the transcript must follow the resume marker:\n%s", out)
	}
}

func TestReplayPrintsAnApprovedPlan(t *testing.T) {
	plan := json.RawMessage(`{"plan":"## Steps\n1. Split the loop"}`)
	sess := session.NewMemory([]llm.Message{
		llm.UserText("plan it"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse, ID: "p1", Name: "exit_plan_mode", Input: plan}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText("p1", "Plan approved, mode is now acceptEdits.", false)}},
	})
	h := startWith(t, Options{Session: sess})
	out := h.out()
	if !strings.Contains(out, "Split the loop") {
		t.Errorf("the plan was not printed on replay:\n%s", out)
	}
	if strings.Count(out, "Split the loop") != 1 {
		t.Errorf("the plan was printed more than once:\n%s", out)
	}
}
