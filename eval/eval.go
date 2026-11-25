package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

type Call struct {
	ID    string
	Name  string
	Input map[string]any
}

type turn struct {
	Text  string
	Calls []Call
}

type evalCase struct {
	Name    string
	Files   map[string]string
	Prompt  string
	Script  []turn
	Mode    permission.Mode
	Allow   []string
	Deny    []string
	MaxTurn int
	Checks  []check
}

type baseline struct {
	Cases     int       `json:"cases"`
	Passed    int       `json:"passed"`
	Rate      float64   `json:"rate"`
	Turns     int       `json:"turns"`
	Usage     llm.Usage `json:"usage"`
	FailNames []string  `json:"failed,omitempty"`
}

type outcome struct {
	Name     string            `json:"name"`
	Passed   bool              `json:"passed"`
	Failures []string          `json:"failures,omitempty"`
	Reason   string            `json:"reason"`
	Err      string            `json:"err,omitempty"`
	Turns    int               `json:"turns"`
	Usage    llm.Usage         `json:"usage"`
	Text     string            `json:"text"`
	Calls    []string          `json:"calls"`
	Results  map[string]string `json:"results,omitempty"`
}

type check func(o *outcome, dir string) string

func calledTools(names ...string) check {
	return func(o *outcome, _ string) string {
		i := 0
		for _, got := range o.Calls {
			if i < len(names) && got == names[i] {
				i++
			}
			if i == len(names) {
				return ""
			}
		}
		return fmt.Sprintf("tool trajectory %v does not contain %v in order", o.Calls, names)
	}
}

func finalTextContains(sub string) check {
	return func(o *outcome, _ string) string {
		if !strings.Contains(o.Text, sub) {
			return fmt.Sprintf("final text %q does not contain %q", o.Text, sub)
		}
		return ""
	}
}

func fileEquals(path, want string) check {
	return func(_ *outcome, dir string) string {
		b, err := os.ReadFile(filepath.Join(dir, path))
		if err != nil {
			return fmt.Sprintf("read %s: %v", path, err)
		}
		if got := strings.ReplaceAll(string(b), "\r\n", "\n"); got != want {
			return fmt.Sprintf("%s = %q, want %q", path, got, want)
		}
		return ""
	}
}

func Reason(want agent.StopReason) check {
	return func(o *outcome, _ string) string {
		if o.Reason != string(want) {
			return fmt.Sprintf("stop reason = %s, want %s", o.Reason, want)
		}
		return ""
	}
}

type scripted struct {
	turns []turn
	next  int
}

func (s *scripted) stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		if s.next >= len(s.turns) {
			yield(llm.StreamEvent{Type: llm.SEMessageStart}, nil)
			yield(llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}}, nil)
			yield(llm.StreamEvent{Type: llm.SETextDelta, Text: "(script exhausted)"}, nil)
			yield(llm.StreamEvent{Type: llm.SEBlockStop}, nil)
			yield(llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop}, nil)
			yield(llm.StreamEvent{Type: llm.SEMessageStop}, nil)
			return
		}
		t := s.turns[s.next]
		s.next++
		evs := []llm.StreamEvent{{Type: llm.SEMessageStart}}
		if t.Text != "" {
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
				llm.StreamEvent{Type: llm.SETextDelta, Text: t.Text},
				llm.StreamEvent{Type: llm.SEBlockStop},
			)
		}
		for _, c := range t.Calls {
			raw, _ := json.Marshal(c.Input)
			evs = append(evs,
				llm.StreamEvent{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: c.ID, Name: c.Name, Input: raw}},
				llm.StreamEvent{Type: llm.SEBlockStop},
			)
		}
		stop := llm.StopReasonStop
		if len(t.Calls) > 0 {
			stop = llm.StopReasonToolUse
		}
		evs = append(evs, llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: stop}, llm.StreamEvent{Type: llm.SEMessageStop})
		for _, ev := range evs {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func parseRules(specs []string) []permission.Rule {
	var out []permission.Rule
	for _, spec := range specs {
		name, rest, _ := strings.Cut(spec, "(")
		out = append(out, permission.Rule{
			Tool: strings.TrimSpace(name),
			Spec: strings.TrimSuffix(strings.TrimSpace(rest), ")"),
		})
	}
	return out
}

type streamFunc func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]

func run(ctx context.Context, c evalCase) (outcome, error) {
	return runWith(ctx, c, (&scripted{turns: c.Script}).stream)
}

func runAll(ctx context.Context, cases []evalCase) (baseline, []outcome, error) {
	b := baseline{Cases: len(cases)}
	var outs []outcome
	for _, c := range cases {
		out, err := run(ctx, c)
		if err != nil {
			return b, outs, fmt.Errorf("%s: %w", c.Name, err)
		}
		outs = append(outs, out)
		b.Turns += out.Turns
		b.Usage.Add(out.Usage)
		if out.Passed {
			b.Passed++
		} else {
			b.FailNames = append(b.FailNames, out.Name)
		}
	}
	if b.Cases > 0 {
		b.Rate = float64(b.Passed) / float64(b.Cases)
	}
	return b, outs, nil
}

func runWith(ctx context.Context, c evalCase, call streamFunc) (outcome, error) {
	dir, err := os.MkdirTemp("", "omp-eval-")
	if err != nil {
		return outcome{}, err
	}
	defer os.RemoveAll(dir)
	for name, body := range c.Files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return outcome{}, err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return outcome{}, err
		}
	}

	mode := c.Mode
	if mode == "" {
		mode = permission.ModeBypass
	}
	policy := permission.NewPolicy(mode, permission.Rules{Allow: parseRules(c.Allow), Deny: parseRules(c.Deny)})
	reg := agent.NewRegistry()
	for _, tool := range tools.Builtin(tools.Config{CWD: dir, Shell: tools.ResolveShell("")}) {
		reg.Register(permission.Wrap(tool, policy))
	}
	sess := session.NewMemory(nil)
	deps := agent.RunDeps{CallModel: call}
	loop := agent.NewLoop(reg, deps, nil, agent.Options{
		System:      []string{"You are being evaluated. Use the tools to complete the task."},
		MaxTurns:    c.MaxTurn,
		CWD:         dir,
		MaxTokens:   8192,
		Concurrency: 1,
	})

	out := outcome{Name: c.Name, Results: map[string]string{}}
	for ev, err := range loop.Run(ctx, sess, c.Prompt) {
		if err != nil {
			return out, err
		}
		switch ev.Kind {
		case agent.EventToolUse:
			out.Calls = append(out.Calls, ev.ToolName)
		case agent.EventToolResult:
			if ev.ToolResult != nil {
				var sb strings.Builder
				for _, b := range ev.ToolResult.Content {
					sb.WriteString(b.Text)
				}
				out.Results[ev.ToolUseID] = sb.String()
			}
		case agent.EventResult:
			out.Reason = string(ev.Result.Reason)
			out.Err = ev.Result.Err
			out.Turns = ev.Result.Turns
			out.Usage = ev.Result.Usage
			out.Text = ev.Result.Text
		}
	}
	for _, check := range c.Checks {
		if msg := check(&out, dir); msg != "" {
			out.Failures = append(out.Failures, msg)
		}
	}
	out.Passed = len(out.Failures) == 0
	return out, nil
}
