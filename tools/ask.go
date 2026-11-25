package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const askResultCap = 4 * 1024

type askArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
}

type Ask struct{}

func (t *Ask) Name() string            { return "ask" }
func (t *Ask) Description() string     { return askDescription }
func (t *Ask) Schema() json.RawMessage { return json.RawMessage(askSchema) }

func (t *Ask) IsReadOnly() bool                       { return true }
func (t *Ask) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *Ask) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Ask) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: askResultCap}
}

func (t *Ask) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a askArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return askResult("ask: invalid arguments: "+err.Error(), true)
	}
	question := strings.TrimSpace(a.Question)
	if question == "" {
		return askResult("ask: question is required", true)
	}
	req := agent.AskRequest{Method: agent.AskInput, Title: "The model is asking", Message: question}
	var opts []string
	for _, o := range a.Options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	if len(opts) > 0 {
		req.Method, req.Options = agent.AskSelect, opts
	}
	resp, err := tc.Ask(ctx, req)
	if err != nil {
		return askResult("No one can answer a question in this session. "+
			"Make a reasonable assumption, state it in your answer, and continue without asking.", true)
	}
	if resp.Cancelled {
		return askResult("The user dismissed the question without answering. "+
			"Make a reasonable assumption, state it, and continue.", true)
	}
	answer := resp.Value
	if req.Method == agent.AskConfirm {
		if resp.Confirmed {
			answer = "yes"
		} else {
			answer = "no"
		}
	}
	return askResult("The user answered: "+answer, false)
}

func askResult(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

const askDescription = `Ask the user a question and wait for the answer. Use it only when the answer changes what you do next and you cannot get it from the repository: a genuine ambiguity in the request, a choice between approaches the user must make, or information that exists only in their head. Do not use it to confirm something you can verify, to ask permission (approval prompts do that), or to report progress. When options are given the user picks one; without options they type a free-form answer. In a session with no interactive user the call fails, so make a reasonable assumption instead and say what you assumed.`

const askSchema = `{
  "type": "object",
  "properties": {
    "question": {
      "type": "string",
      "description": "The question, phrased so a single answer resolves it."
    },
    "options": {
      "type": "array",
      "items": {"type": "string"},
      "description": "Optional answers to choose from, most likely first. Omit for a free-form answer."
    }
  },
  "required": ["question"],
  "additionalProperties": false
}`
