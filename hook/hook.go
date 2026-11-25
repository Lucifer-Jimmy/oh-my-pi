package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	defaultTimeout = 10 * time.Second
	maxOutput      = 1 << 20
)

type Spec struct {
	Command string
	Args    []string
	Env     map[string]string
	Dir     string
	Timeout time.Duration
}

type In struct {
	Event  string          `json:"event"`
	Tool   string          `json:"tool,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Result *ResultIn       `json:"result,omitempty"`
}

type ResultIn struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

type Out struct {
	Block    bool            `json:"block,omitempty"`
	Prevent  bool            `json:"prevent,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Result   *ResultOut      `json:"result,omitempty"`
	Messages []MessageOut    `json:"messages,omitempty"`
}

type ResultOut struct {
	Text    string `json:"text"`
	IsError bool   `json:"is_error"`
}

type MessageOut struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func call(ctx context.Context, spec Spec, in In) (Out, error) {
	if strings.TrimSpace(spec.Command) == "" {
		return Out{}, errors.New("hook: command is required")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(in)
	if err != nil {
		return Out{}, err
	}
	cmd := exec.CommandContext(cctx, spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	env := os.Environ()
	for k, v := range spec.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	cmd.Stdin = bytes.NewReader(append(body, '\n'))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if cctx.Err() != nil {
			return Out{}, fmt.Errorf("hook %s timed out after %s", spec.Command, timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return Out{}, fmt.Errorf("hook %s: %w", spec.Command, err)
		}
		return Out{}, fmt.Errorf("hook %s: %w: %s", spec.Command, err, oneLine(msg))
	}
	if stdout.Len() > maxOutput {
		return Out{}, fmt.Errorf("hook %s wrote more than %d bytes", spec.Command, maxOutput)
	}
	var out Out
	if trimmed := bytes.TrimSpace(stdout.Bytes()); len(trimmed) > 0 {
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return Out{}, fmt.Errorf("hook %s: stdout is not a JSON object: %w", spec.Command, err)
		}
	}
	return out, nil
}

type Set struct {
	PreTool  []Spec
	PostTool []Spec
	Stop     []Spec
	Prompt   []Spec
}

var _ agent.PreToolCallHook = (*Set)(nil)
var _ agent.PostToolCallHook = (*Set)(nil)
var _ agent.StopHook = (*Set)(nil)
var _ agent.PromptInjectHook = (*Set)(nil)

func (s *Set) BeforeToolCall(ctx context.Context, name string, input json.RawMessage) (agent.Decision, error) {
	for _, spec := range s.PreTool {
		out, err := call(ctx, spec, In{Event: "pre_tool", Tool: name, Input: input})
		if err != nil {
			return agent.Decision{}, err
		}
		if out.Block || len(out.Input) > 0 {
			return agent.Decision{Block: out.Block, Reason: out.Reason, NewInput: out.Input}, nil
		}
	}
	return agent.Decision{}, nil
}

func (s *Set) AfterToolCall(ctx context.Context, name string, res agent.ToolResult) (*agent.ToolResult, error) {
	for _, spec := range s.PostTool {
		out, err := call(ctx, spec, In{Event: "post_tool", Tool: name, Result: &ResultIn{Text: textOf(res), IsError: res.IsError}})
		if err != nil {
			return nil, err
		}
		if out.Result != nil {
			res = agent.ToolResult{
				Content: []llm.ContentBlock{llm.TextBlock(out.Result.Text)},
				IsError: out.Result.IsError,
			}
		}
	}
	return &res, nil
}

func (s *Set) OnStop(ctx context.Context) (agent.StopDecision, error) {
	for _, spec := range s.Stop {
		out, err := call(ctx, spec, In{Event: "stop"})
		if err != nil {
			return agent.StopDecision{}, err
		}
		if out.Prevent {
			return agent.StopDecision{Prevent: true, Reason: out.Reason}, nil
		}
	}
	return agent.StopDecision{}, nil
}

func (s *Set) OnPrompt(ctx context.Context) ([]llm.Message, error) {
	var msgs []llm.Message
	for _, spec := range s.Prompt {
		out, err := call(ctx, spec, In{Event: "prompt"})
		if err != nil {
			return msgs, err
		}
		for _, m := range out.Messages {
			if strings.TrimSpace(m.Text) == "" {
				continue
			}
			text := m.Text
			if strings.EqualFold(m.Role, "system") {
				text = "<hook role=\"system\">\n" + text + "\n</hook>"
			}
			msgs = append(msgs, llm.UserText(text))
		}
	}
	return msgs, nil
}

func (s *Set) Hooks() agent.Hooks {
	if s == nil {
		return agent.Hooks{}
	}
	var h agent.Hooks
	if len(s.PreTool) > 0 {
		h.PreTool = s
	}
	if len(s.PostTool) > 0 {
		h.PostTool = s
	}
	if len(s.Stop) > 0 {
		h.Stop = s
	}
	if len(s.Prompt) > 0 {
		h.Prompt = s
	}
	return h
}

func textOf(res agent.ToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if c.Type == llm.BlockText {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

func oneLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
