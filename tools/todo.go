package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const todoResultCap = 8 * 1024

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	Content string     `json:"content"`
	Status  TodoStatus `json:"status"`
}

type todoArgs struct {
	Items []TodoItem `json:"items"`
}

type Todo struct{}

func (t *Todo) Name() string            { return "todo" }
func (t *Todo) Description() string     { return todoDescription }
func (t *Todo) Schema() json.RawMessage { return json.RawMessage(todoSchema) }

func (t *Todo) IsReadOnly() bool                       { return true }
func (t *Todo) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *Todo) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Todo) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: todoResultCap}
}

func (t *Todo) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a todoArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return todoResult("todo: invalid arguments: "+err.Error(), true)
	}
	if len(a.Items) == 0 {
		return todoResult("todo: items must not be empty; pass the whole list each time", true)
	}
	inProgress := 0
	for i, it := range a.Items {
		if strings.TrimSpace(it.Content) == "" {
			return todoResult(fmt.Sprintf("todo: item %d has no content", i+1), true)
		}
		switch it.Status {
		case TodoPending, TodoCompleted:
		case TodoInProgress:
			inProgress++
		default:
			return todoResult(fmt.Sprintf("todo: item %d has status %q; use pending, in_progress or completed", i+1, it.Status), true)
		}
	}
	if inProgress > 1 {
		return todoResult("todo: at most one item may be in_progress", true)
	}
	return todoResult(renderTodos(a.Items), false)
}

func renderTodos(items []TodoItem) string {
	var sb strings.Builder
	done := 0
	for _, it := range items {
		if it.Status == TodoCompleted {
			done++
		}
	}
	fmt.Fprintf(&sb, "Todos (%d/%d done)\n", done, len(items))
	for _, it := range items {
		switch it.Status {
		case TodoCompleted:
			sb.WriteString("[x] " + it.Content + "\n")
		case TodoInProgress:
			sb.WriteString("[>] " + it.Content + "\n")
		default:
			sb.WriteString("[ ] " + it.Content + "\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func todoResult(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

const todoDescription = `Maintain the task list for this session. Pass the whole list on every call — items are replaced, not merged. Each item has a short content line and a status: pending, in_progress or completed. Keep at most one item in_progress; mark an item completed as soon as it is actually done, not when it is started. Use it for work with three or more steps, or when the user gives several tasks: it is how the user sees progress. Do not use it for a single trivial edit.`

const todoSchema = `{
  "type": "object",
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "content": {"type": "string", "description": "One short line describing the step."},
          "status": {"type": "string", "enum": ["pending", "in_progress", "completed"]}
        },
        "required": ["content", "status"],
        "additionalProperties": false
      }
    }
  },
  "required": ["items"],
  "additionalProperties": false
}`
