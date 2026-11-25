package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const resultCap = 32 * 1024

type Result struct {
	Text   string
	Note   string
	Turns  int
	Usage  llm.Usage
	Reason agent.StopReason
	Err    string
}

type Task struct {
	Run      func(ctx context.Context, prompt string, progress func(agent.Event)) (Result, error)
	Isolated func(ctx context.Context, prompt string, progress func(agent.Event)) (Result, error)
}

type taskArgs struct {
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	Isolated    bool   `json:"isolated"`
}

func (t *Task) Name() string            { return "task" }
func (t *Task) Description() string     { return taskDescription }
func (t *Task) Schema() json.RawMessage { return json.RawMessage(taskSchema) }

func (t *Task) IsReadOnly() bool                       { return false }
func (t *Task) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *Task) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierWrite}
}

func (t *Task) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: resultCap}
}

func (t *Task) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return taskResult("task: invalid arguments: "+err.Error(), true)
	}
	prompt := strings.TrimSpace(a.Prompt)
	if prompt == "" {
		return taskResult("task: prompt is required", true)
	}
	if a.Description != "" {
		tc.Emit(agent.Event{Kind: agent.EventProgress, Message: "subagent: " + a.Description})
	}
	var res Result
	var err error
	if a.Isolated {
		if t.Isolated == nil {
			return taskResult("task: the working directory is not inside a git repository with a commit, so an isolated task cannot merge its changes back; run it without isolation", true)
		}
		res, err = t.Isolated(ctx, prompt, childProgress(tc))
	} else {
		res, err = t.Run(ctx, prompt, childProgress(tc))
	}
	if err != nil {
		return taskResult("task failed: "+err.Error(), true)
	}
	body := strings.TrimSpace(res.Text)
	if body == "" {
		body = "(the subagent produced no text)"
	}
	if res.Note != "" {
		body += "\n\n" + res.Note
	}
	footer := fmt.Sprintf("\n\n— subagent: %s, %d turns, %d in / %d out tokens",
		res.Reason, res.Turns, res.Usage.InputTokens, res.Usage.OutputTokens)
	if res.Err != "" {
		footer += ", error: " + res.Err
	}
	text := body + footer
	if len(text) > resultCap {
		text = text[:resultCap] + "\n... (subagent result truncated)"
	}
	return taskResult(text, res.Reason != agent.ReasonCompleted)
}

func childProgress(tc agent.ToolContext) func(agent.Event) {
	var used llm.Usage
	var name string
	var input json.RawMessage
	return func(ev agent.Event) {
		switch ev.Kind {
		case agent.EventToolUse:
			name, input = ev.ToolName, ev.ToolInput
		case agent.EventUsage:
			if ev.Usage == nil {
				return
			}
			used.Add(*ev.Usage)
		default:
			return
		}
		u := used
		tc.Emit(agent.Event{Kind: agent.EventProgress, ToolName: name, ToolInput: input, Usage: &u})
	}
}

func taskResult(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

const taskDescription = `Run a subagent: a fresh conversation with the same tool set except this one, given one self-contained prompt. It has no memory of this conversation, so the prompt must carry every fact it needs — the goal, the files, the constraints — and it returns only its final answer. Use it to keep a large, separable investigation out of this conversation's context, or to run an independent piece of work. It cannot spawn further subagents. Start subagent work only after any work this turn depends on has finished. Its output is a summary, not evidence: if you need exact content, read the file yourself. Set isolated when the subagent may edit files: it then works in a throwaway git worktree and its changes are merged into your working tree only if they apply cleanly, so a failing experiment cannot damage the files you are editing.`

const taskSchema = `{
  "type": "object",
  "properties": {
    "description": {
      "type": "string",
      "description": "A five-word label shown to the user while it runs."
    },
    "prompt": {
      "type": "string",
      "description": "The complete, self-contained instruction for the subagent."
    },
    "isolated": {
      "type": "boolean",
      "description": "Edit files in a throwaway git worktree and merge the changes back afterwards. Use it whenever the subagent is allowed to write."
    }
  },
  "required": ["description", "prompt"],
  "additionalProperties": false
}`
