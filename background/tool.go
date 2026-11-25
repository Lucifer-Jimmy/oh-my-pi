package background

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	toolResultCap = 8 * 1024
	waitDefault   = 30 * time.Second
	waitMax       = 10 * time.Minute
)

type toolArgs struct {
	Action         string `json:"action"`
	Command        string `json:"command,omitempty"`
	ID             string `json:"id,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type Tool struct {
	Manager *Manager
}

func (t *Tool) Name() string            { return "background" }
func (t *Tool) Description() string     { return toolDescription }
func (t *Tool) Schema() json.RawMessage { return json.RawMessage(toolSchema) }

func (t *Tool) IsReadOnly() bool                       { return false }
func (t *Tool) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *Tool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierExec}
}

func (t *Tool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: toolResultCap}
}

func (t *Tool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a toolArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return result("background: invalid arguments: "+err.Error(), true)
	}
	if t.Manager == nil {
		return result("background: no process manager in this session", true)
	}
	switch strings.ToLower(strings.TrimSpace(a.Action)) {
	case "start":
		task, err := t.Manager.Start(tc.CWD(), a.Command)
		if err != nil {
			return result("background: "+err.Error(), true)
		}
		return result(fmt.Sprintf("started %s\nlog: %s\nYou will be woken with a short notification when it finishes; read the log for the rest.",
			task.ID, task.LogPath), false)
	case "wait":
		id := strings.TrimSpace(a.ID)
		if id == "" {
			return result("background: id is required for wait", true)
		}
		timeout := waitDefault
		if a.TimeoutSeconds > 0 {
			timeout = time.Duration(a.TimeoutSeconds) * time.Second
			timeout = min(timeout, waitMax)
		}
		task, err := t.Manager.Wait(ctx, id, timeout)
		if err != nil && task == nil {
			return result("background: "+err.Error(), true)
		}
		status := task.Status()
		if status == "running" {
			return result(fmt.Sprintf("%s is still running after %s; you will be notified when it finishes.\nlog: %s",
				id, timeout, task.LogPath), false)
		}
		return result(fmt.Sprintf("%s %s\nlog: %s\nlast lines:\n%s", id, status, task.LogPath, task.Tail()), status != "succeeded")
	case "list":
		return result(t.Manager.List(), false)
	}
	return result(`background: action must be "start", "wait" or "list"`, true)
}

func (m *Manager) List() string {
	m.mu.Lock()
	ids := append([]string{}, m.order...)
	tasks := make(map[string]*Task, len(m.tasks))
	for k, v := range m.tasks {
		tasks[k] = v
	}
	m.mu.Unlock()
	if len(ids) == 0 {
		return "no background tasks"
	}
	var b strings.Builder
	for _, id := range ids {
		t := tasks[id]
		fmt.Fprintf(&b, "%s  %s  %s\n", id, t.Status(), t.Command)
	}
	return strings.TrimRight(b.String(), "\n")
}

func result(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

const toolDescription = `Run and check background commands. action=start launches the command through the same shell as bash, writes its full output to a log file, and returns an id at once — use it for anything long (a build, a test suite, a server) instead of blocking the conversation. You are notified with the id, the exit status, the log path and the last few lines when it finishes. action=wait blocks until that task ends or the timeout expires; action=list shows what has been started. Read the log with the read tool for anything beyond the tail.`

const toolSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["start", "wait", "list"]},
    "command": {"type": "string", "description": "The command to run (start only)."},
    "id": {"type": "string", "description": "The task id (wait only)."},
    "timeout_seconds": {"type": "integer", "description": "How long wait may block, default 30, capped at 600."}
  },
  "required": ["action"],
  "additionalProperties": false
}`
