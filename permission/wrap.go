package permission

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type Reviewable interface {
	ReviewSpecs(input json.RawMessage) []Spec
}

type wrapped struct {
	inner  agent.Tool
	policy *Policy
	specs  func(json.RawMessage) []Spec
}

func Wrap(inner agent.Tool, policy *Policy) agent.Tool {
	w := &wrapped{inner: inner, policy: policy}
	if r, ok := inner.(Reviewable); ok {
		w.specs = r.ReviewSpecs
	}
	return w
}

func (w *wrapped) approval(input json.RawMessage) agent.Approval {
	d, ok := w.inner.(agent.ApprovalDeclarer)
	if !ok {
		return agent.Approval{Tier: agent.TierExec}
	}
	a := approvalOf(d, input)
	if a.Tier == "" {
		a.Tier = agent.TierExec
	}
	return a
}

func approvalOf(d agent.ApprovalDeclarer, input json.RawMessage) (a agent.Approval) {
	defer func() {
		if recover() != nil {
			a = agent.Approval{Tier: agent.TierExec}
		}
	}()
	return d.Approval(input)
}

func (w *wrapped) Name() string            { return w.inner.Name() }
func (w *wrapped) Description() string     { return w.inner.Description() }
func (w *wrapped) Schema() json.RawMessage { return w.inner.Schema() }

func (w *wrapped) IsReadOnly() bool {
	if s, ok := w.inner.(agent.Schedulable); ok {
		return s.IsReadOnly()
	}
	return false
}

func (w *wrapped) IsConcurrencySafe(input json.RawMessage) bool {
	if s, ok := w.inner.(agent.Schedulable); ok {
		return s.IsConcurrencySafe(input)
	}
	return false
}

func (w *wrapped) Approval(input json.RawMessage) agent.Approval {
	if d, ok := w.inner.(agent.ApprovalDeclarer); ok {
		return d.Approval(input)
	}
	return agent.Approval{Tier: agent.TierExec}
}

func (w *wrapped) Metadata(input json.RawMessage) agent.Metadata {
	return agent.MetadataOf(w.inner, input)
}

func (w *wrapped) ReviewSpecs(input json.RawMessage) []Spec {
	if w.specs == nil {
		return nil
	}
	return w.specs(input)
}

func (w *wrapped) Execute(ctx context.Context, tc agent.ToolContext, args json.RawMessage) agent.ToolResult {
	a := w.approval(args)
	mode, rules := w.policy.current()
	req := Request{
		Tool:        w.inner.Name(),
		ForcePrompt: a.ForcePrompt,
		Specs:       w.ReviewSpecs(args),
		Tier:        Tier(a.Tier),
		Mode:        mode,
		Rules:       rules,
		Interactive: true,
	}

	d := Evaluate(req)
	reason := d.Reason
	if a.Reason != "" {
		reason += " (" + a.Reason + ")"
	}
	if d.Behavior == Deny {
		return refuse(w.inner.Name(), reason)
	}
	if d.Behavior == Ask {
		var remember []Rule
		if !d.Sticky {
			remember = sessionRules(req.Tool, req.Specs)
		}
		approved, err := w.ask(ctx, tc, args, reason, remember)
		if err != nil {
			return refuse(w.inner.Name(), unavailable(reason))
		}
		if !approved {
			return refuse(w.inner.Name(), "user denied the call")
		}
	}
	return w.inner.Execute(ctx, tc, args)
}

const (
	optionYes = "Yes"
	optionNo  = "No"
)

func (w *wrapped) ask(ctx context.Context, tc agent.ToolContext, args json.RawMessage, reason string, remember []Rule) (bool, error) {
	options := []string{optionYes}
	var always string
	if len(remember) > 0 {
		always = "Yes, and don't ask again this session for " + describeRules(remember)
		options = append(options, always)
	}
	options = append(options, optionNo)
	resp, err := tc.Ask(ctx, agent.AskRequest{
		Method:  agent.AskSelect,
		Title:   "Approve " + w.inner.Name(),
		Message: riskNotes(args, w.inner) + reason,
		Options: options,
		Tool:    w.inner.Name(),
		Input:   args,
		Detail:  preview(args),
	})
	if err != nil {
		return false, err
	}
	switch {
	case resp.Cancelled:
		return false, nil
	case resp.Value == optionYes:
		return true, nil
	case always != "" && resp.Value == always:
		w.policy.Allow(remember...)
		return true, nil
	}
	return false, nil
}

const (
	noteDestructive = "This call can delete, overwrite or otherwise irreversibly change state."
	noteOpenWorld   = "This call can reach beyond the workspace through the network or other processes."
)

func riskNotes(args json.RawMessage, t agent.Tool) string {
	m := agent.MetadataOf(t, args)
	var b strings.Builder
	if m.Destructive {
		b.WriteString(noteDestructive)
		b.WriteByte('\n')
	}
	if m.OpenWorld {
		b.WriteString(noteOpenWorld)
		b.WriteByte('\n')
	}
	return b.String()
}

func unavailable(reason string) string {
	return reason + "; no interactive approval is available in this mode, " +
		"add an allow rule for this command or rerun with a different --approval mode"
}

func preview(args json.RawMessage) string {
	const limit = 500
	s := describeArgs(args)
	if len(s) > limit {
		s = strings.ToValidUTF8(s[:limit], "") + "..."
	}
	return s
}

func describeArgs(args json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(args))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return string(args)
	}
	var lines []string
	for dec.More() {
		tok, err := dec.Token()
		key, ok := tok.(string)
		var raw json.RawMessage
		if err != nil || !ok || dec.Decode(&raw) != nil {
			return string(args)
		}
		var s string
		if json.Unmarshal(raw, &s) != nil {
			s = string(raw)
		}
		lines = append(lines, key+": "+s)
	}
	return strings.Join(lines, "\n")
}

func refuse(tool, reason string) agent.ToolResult {
	return agent.ToolResult{
		Content: []llm.ContentBlock{llm.TextBlock(fmt.Sprintf("%s: %s", tool, reason))},
		IsError: true,
	}
}
