package plan

import (
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const activeReminder = `<system-reminder>
Plan mode is active: the user wants a plan before anything changes.
- Only read-only tools work. Editing or writing files and running shell commands are blocked until the plan is approved.
- Explore until you know what the change involves. Ask the user only about choices you cannot settle by reading the code.
- When the plan is complete, call exit_plan_mode with it. If the user only asked a question, answer it instead.
</system-reminder>`

const endedReminder = `<system-reminder>
Plan mode is off. You may edit files and run commands again, subject to the usual approvals.
</system-reminder>`

func WithReminder(input string, inPlan bool, history []llm.Message) string {
	switch {
	case inPlan:
		return activeReminder + "\n\n" + input
	case strings.HasPrefix(lastInput(history), activeReminder):
		return endedReminder + "\n\n" + input
	}
	return input
}

func lastInput(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role != llm.RoleUser {
			continue
		}
		for _, b := range history[i].Content {
			if b.Type == llm.BlockText {
				return b.Text
			}
		}
	}
	return ""
}
