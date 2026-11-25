package plan

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

const exitResultCap = 8 * 1024

const (
	optionAcceptEdits = "Yes, and auto-accept edits"
	optionAskEdits    = "Yes, and manually approve edits"
	optionKeep        = "No, keep planning"
)

type ModeControl interface {
	Mode() permission.Mode
	SetMode(permission.Mode)
}

type exitArgs struct {
	Plan string `json:"plan"`
}

type ExitTool struct {
	Modes ModeControl
}

func (t *ExitTool) Name() string            { return "exit_plan_mode" }
func (t *ExitTool) Description() string     { return exitDescription }
func (t *ExitTool) Schema() json.RawMessage { return json.RawMessage(exitSchema) }

func (t *ExitTool) IsReadOnly() bool                       { return true }
func (t *ExitTool) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *ExitTool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *ExitTool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: exitResultCap}
}

func (t *ExitTool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a exitArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return result("exit_plan_mode: invalid arguments: "+err.Error(), true)
	}
	if strings.TrimSpace(a.Plan) == "" {
		return result("exit_plan_mode: plan is required", true)
	}
	if t.Modes.Mode() != permission.ModePlan {
		return result("Not in plan mode, so no approval is needed.\nContinue with the task.", false)
	}
	resp, err := tc.Ask(ctx, agent.AskRequest{
		Method:  agent.AskSelect,
		Title:   "Approve the plan",
		Message: a.Plan,
		Options: []string{optionAcceptEdits, optionAskEdits, optionKeep},
		Tool:    t.Name(),
		Input:   raw,
	})
	if err != nil {
		return result("No one can approve the plan in this session.\n"+
			"You are still in plan mode and cannot make changes. Give the complete plan as your final answer and stop.", true)
	}
	var mode permission.Mode
	switch {
	case resp.Cancelled:
	case resp.Value == optionAcceptEdits:
		mode = permission.ModeAcceptEdits
	case resp.Value == optionAskEdits:
		mode = permission.ModeDefault
	}
	if mode == "" {
		return result("The user did not approve the plan and wants to keep planning.\n"+
			"You are still in plan mode. Ask the user what to change, then revise the plan and call exit_plan_mode again.", true)
	}
	t.Modes.SetMode(mode)
	return result(fmt.Sprintf("Plan approved, mode is now %s.\n"+
		"Plan mode is over. Make the changes now, following the plan step by step and verifying each step.", mode), false)
}

func result(text string, isError bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}, IsError: isError}
}

const exitSchema = `{
  "type": "object",
  "properties": {
    "plan": {"type": "string", "description": "The complete plan in markdown."}
  },
  "required": ["plan"],
  "additionalProperties": false
}`

const exitDescription = `Present your plan to the user for approval and leave plan mode.
Call it only in plan mode, once you have explored the code with read-only tools and the plan is complete; if the user only asked a question, answer it instead. Put the whole plan in plan as markdown: the goal, the ordered steps with the files and functions each one changes, and how to verify the result. The user either approves it, which ends plan mode so you can make the changes, or asks you to keep planning. Never ask for approval in a message; this tool is the only way out of plan mode.`
