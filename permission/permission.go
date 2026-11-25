package permission

import (
	"fmt"
	"strings"
)

type Kind int

const (
	KindCommand Kind = iota
	KindPath
)

type Spec struct {
	Kind  Kind
	Value string
}

func (s Spec) Match(rule string) bool {
	if s.Value == "" || rule == "" {
		return false
	}
	switch s.Kind {
	case KindCommand:
		return matchCommand(rule, s.Value)
	case KindPath:
		return matchPath(rule, s.Value)
	}
	return false
}

type Rule struct {
	Tool string
	Spec string
}

func (r Rule) matchesTool(tool string) bool {
	return r.Tool == "*" || strings.EqualFold(r.Tool, tool)
}

type Rules struct {
	Allow []Rule
	Ask   []Rule
	Deny  []Rule
}

func (r Rules) first(list []Rule, tool string, specs []Spec) (Rule, bool) {
	for _, rule := range list {
		if !rule.matchesTool(tool) {
			continue
		}
		if rule.Spec == "" {
			return rule, true
		}
		for _, s := range specs {
			if s.Match(rule.Spec) {
				return rule, true
			}
		}
	}
	return Rule{}, false
}

func (r Rules) firstCovering(list []Rule, tool string, specs []Spec) (Rule, bool) {
	var first Rule
	found := false
	for _, rule := range list {
		if rule.matchesTool(tool) && rule.Spec == "" {
			return rule, true
		}
	}
	if len(specs) == 0 {
		return Rule{}, false
	}
	for _, s := range specs {
		rule, ok := matchingRule(list, tool, s)
		if !ok {
			return Rule{}, false
		}
		if !found {
			first, found = rule, true
		}
	}
	return first, true
}

func matchingRule(list []Rule, tool string, s Spec) (Rule, bool) {
	for _, rule := range list {
		if rule.matchesTool(tool) && s.Match(rule.Spec) {
			return rule, true
		}
	}
	return Rule{}, false
}

type Behavior string

const (
	Allow Behavior = "allow"
	Ask   Behavior = "ask"
	Deny  Behavior = "deny"
)

type Mode string

const (
	ModeDefault     Mode = "default"
	ModeAcceptEdits Mode = "acceptEdits"
	ModePlan        Mode = "plan"
	ModeBypass      Mode = "bypass"
)

func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case "":
		return ModeDefault, nil
	case ModeDefault, ModeAcceptEdits, ModePlan, ModeBypass:
		return Mode(s), nil
	}
	return "", fmt.Errorf("unknown approval mode %q", s)
}

type Tier string

const (
	TierRead  Tier = "read"
	TierWrite Tier = "write"
	TierExec  Tier = "exec"
)

type Request struct {
	Tool        string
	ForcePrompt bool
	Specs       []Spec
	Tier        Tier
	Mode        Mode
	Rules       Rules
	Interactive bool
}

type Decision struct {
	Behavior Behavior
	Reason   string
	Sticky   bool
}

func (d Decision) Allowed() bool { return d.Behavior == Allow }

func allow(reason string) Decision { return Decision{Behavior: Allow, Reason: reason} }
func deny(reason string) Decision  { return Decision{Behavior: Deny, Reason: reason} }

func (r Request) pendingAsk(reason string) Decision {
	if r.Interactive {
		return Decision{Behavior: Ask, Reason: reason}
	}
	return deny(reason + "; no interactive approval available, add an allow rule or change the mode")
}

func (r Request) stickyAsk(reason string) Decision {
	d := r.pendingAsk(reason)
	d.Sticky = d.Behavior == Ask
	return d
}

func Evaluate(r Request) Decision {
	if rule, ok := r.Rules.first(r.Rules.Deny, r.Tool, r.Specs); ok {
		return deny(describe("denied by rule", rule))
	}
	if r.Mode == ModePlan && r.Tier != TierRead {
		return deny("denied: plan mode permits read-only tools only")
	}
	if r.Mode == ModeBypass {
		return allow("allowed: bypass mode")
	}
	if rule, ok := r.Rules.first(r.Rules.Ask, r.Tool, r.Specs); ok {
		return r.stickyAsk(describe("approval required by rule", rule))
	}
	if rule, ok := r.Rules.firstCovering(r.Rules.Allow, r.Tool, r.Specs); ok {
		return allow(describe("allowed by rule", rule))
	}
	if r.ForcePrompt {
		return r.stickyAsk("approval required: the tool flagged this call")
	}

	switch r.Mode {
	case ModeAcceptEdits:
		switch r.Tier {
		case TierRead:
			return allow("allowed: read-only tool")
		case TierWrite:
			return allow("allowed: acceptEdits mode")
		}
		return r.pendingAsk("approval required for a command")
	default:
		if r.Tier == TierRead {
			return allow("allowed: read-only tool")
		}
		return r.pendingAsk("approval required for " + string(r.Tier) + " tool")
	}
}

func describe(prefix string, rule Rule) string {
	if rule.Spec == "" {
		return prefix + " for tool " + rule.Tool
	}
	return prefix + ": " + rule.Tool + "(" + rule.Spec + ")"
}
