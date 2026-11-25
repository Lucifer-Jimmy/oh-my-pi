package plan

import (
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func TestWithReminder(t *testing.T) {
	planned := WithReminder("fix the retry bug", true, nil)
	if !strings.HasPrefix(planned, "<system-reminder>") || !strings.HasSuffix(planned, "\n\nfix the retry bug") ||
		!strings.Contains(planned, "exit_plan_mode") {
		t.Errorf("plan mode must prefix the input with its rules:\n%s", planned)
	}

	history := []llm.Message{
		llm.UserText(planned),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{{Type: llm.BlockToolUse, ID: "p1", Name: "exit_plan_mode"}}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "p1"}}},
	}
	left := WithReminder("go ahead", false, history)
	if !strings.Contains(left, "Plan mode is off") || !strings.HasSuffix(left, "\n\ngo ahead") {
		t.Errorf("the first input after plan mode must say it ended:\n%s", left)
	}
	if got := WithReminder("next", false, append(history, llm.UserText(left))); got != "next" {
		t.Errorf("the end of plan mode is announced once, got:\n%s", got)
	}
	if got := WithReminder("hello", false, nil); got != "hello" {
		t.Errorf("outside plan mode the input is untouched, got %q", got)
	}
	if again := WithReminder("and also", true, history); !strings.HasPrefix(again, "<system-reminder>") {
		t.Errorf("every input in plan mode carries the rules:\n%s", again)
	}
}
