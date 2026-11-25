package memory

import (
	"context"
	"encoding/json"
	"path/filepath"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const learnResultCap = 4 * 1024

type learnArgs struct {
	Memory  string `json:"memory"`
	Context string `json:"context,omitempty"`
}

type LearnTool struct {
	Store Store
}

func (t *LearnTool) Name() string            { return "learn" }
func (t *LearnTool) Description() string     { return learnDescription }
func (t *LearnTool) Schema() json.RawMessage { return json.RawMessage(learnSchema) }

func (t *LearnTool) IsReadOnly() bool                       { return false }
func (t *LearnTool) IsConcurrencySafe(json.RawMessage) bool { return false }

func (t *LearnTool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierWrite}
}

func (t *LearnTool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: learnResultCap}
}

func (t *LearnTool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a learnArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return learnResult("learn: invalid arguments: "+err.Error(), true)
	}
	stored, err := t.Store.Learn(a.Memory, a.Context)
	if err != nil {
		return learnResult("learn: the lesson was not saved: "+err.Error(), true)
	}
	if !stored {
		return learnResult("learn: the lesson is empty once markup and secrets are removed; nothing was saved", true)
	}
	return learnResult("Lesson saved to "+filepath.Join(t.Store.Dir, LessonsFile)+". It is part of the memory of later sessions in this project, not of this one.", false)
}

func learnResult(text string, isError bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}, IsError: isError}
}

const learnDescription = `Save a durable lesson to this project's long-term memory. Later sessions in this project start with it.

Use it after learning something likely to pay off again: a non-obvious fix, a project convention, a command or workflow that worked, a pitfall and how to avoid it, a preference the user stated. Write the lesson self-contained — what, when it applies, why — because it will be read without this conversation.

Capture sparingly: one strong lesson beats several vague ones. Never save secrets, credentials or anything only true for this moment. Saving the same lesson again moves it to the top instead of duplicating it.`

const learnSchema = `{
  "type": "object",
  "properties": {
    "memory": {"type": "string", "description": "The durable, self-contained lesson: what, when it applies, why."},
    "context": {"type": "string", "description": "Optional short note on where the lesson came from."}
  },
  "required": ["memory"],
  "additionalProperties": false
}`
