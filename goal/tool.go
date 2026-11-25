package goal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const toolResultCap = 8 * 1024

type toolArgs struct {
	Op          string `json:"op"`
	Objective   string `json:"objective,omitempty"`
	TokenBudget int    `json:"token_budget,omitempty"`
}

type Tool struct {
	Tracker *Tracker
}

func (t *Tool) Name() string            { return "goal" }
func (t *Tool) Description() string     { return toolDescription }
func (t *Tool) Schema() json.RawMessage { return json.RawMessage(toolSchema) }

func (t *Tool) IsReadOnly() bool                       { return false }
func (t *Tool) IsConcurrencySafe(json.RawMessage) bool { return false }

func (t *Tool) Approval(raw json.RawMessage) agent.Approval {
	var a toolArgs
	if json.Unmarshal(raw, &a) == nil && a.Op == "get" {
		return agent.Approval{Tier: agent.TierRead}
	}
	return agent.Approval{Tier: agent.TierWrite}
}

func (t *Tool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: toolResultCap}
}

func (t *Tool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	if t.Tracker == nil {
		return toolResult("goal: goal mode is not available in this session", true)
	}
	var a toolArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return toolResult("goal: invalid arguments: "+err.Error(), true)
	}
	var (
		g   Goal
		ok  = true
		err error
	)
	switch a.Op {
	case "create":
		g, err = t.Tracker.Create(a.Objective, a.TokenBudget)
	case "get":
		g, ok = t.Tracker.Current()
	case "resume":
		g, err = t.Tracker.Resume()
	case "complete":
		g, err = t.Tracker.Complete()
	case "drop":
		g, err = t.Tracker.Drop()
	default:
		return toolResult(fmt.Sprintf("goal: unknown op %q", a.Op), true)
	}
	if errors.Is(err, ErrNoGoal) || !ok {
		return toolResult("No goal is set.", a.Op != "get")
	}
	if err != nil {
		return toolResult("goal: "+err.Error(), true)
	}
	text := Describe(g)
	switch a.Op {
	case "complete":
		text += "\n\n" + completionReport(g)
	case "drop":
		text += "\n\nThe goal is dropped; automatic continuation has stopped."
	case "get":
		if g.Status == StatusPaused {
			text += "\n\nThe goal is paused: resume it before working on it."
		}
	}
	return toolResult(text, false)
}

func toolResult(text string, isError bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(strings.TrimSpace(text))}, IsError: isError}
}

const toolDescription = `Manage this session's goal: one objective the user wants finished, which the session keeps working on across turns until it is verifiably met.

op:
- create: set the goal. Needs objective; token_budget is optional. Only when the user asks for a goal and none is active or paused.
- get: show the goal, its status and the tokens used and left.
- resume: re-activate a paused goal before working on it.
- complete: mark the goal met. Only after checking the current state against every deliverable — read the files, run the checks. Never because the budget is low or the turn is ending.
- drop: abandon the goal without completing it. Never drop a goal to replace its objective; ask the user to set the new one.`

const toolSchema = `{
  "type": "object",
  "properties": {
    "op": {"type": "string", "enum": ["create", "get", "resume", "complete", "drop"], "description": "The operation."},
    "objective": {"type": "string", "description": "The goal, for create: what must be true when it is done, including how to verify it."},
    "token_budget": {"type": "integer", "description": "Optional token budget for create, a positive integer."}
  },
  "required": ["op"],
  "additionalProperties": false
}`
