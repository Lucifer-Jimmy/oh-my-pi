package permission

import (
	"slices"
	"strings"
	"sync"
)

type Policy struct {
	mu    sync.RWMutex
	mode  Mode
	rules Rules
}

func NewPolicy(mode Mode, rules Rules) *Policy {
	return &Policy{mode: mode, rules: rules}
}

func (p *Policy) Mode() Mode {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode
}

func (p *Policy) Snapshot() (Mode, Rules) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode, p.rules
}

func (p *Policy) SetMode(m Mode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mode = m
}

func (p *Policy) Allow(rules ...Rule) {
	p.mu.Lock()
	defer p.mu.Unlock()
	allow := slices.Clip(p.rules.Allow)
	for _, r := range rules {
		if !slices.Contains(allow, r) {
			allow = append(allow, r)
		}
	}
	p.rules.Allow = allow
}

func (p *Policy) DeniesPath(tool, rel string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, rule := range p.rules.Deny {
		if !rule.matchesTool(tool) {
			continue
		}
		if rule.Spec == "" || matchPath(rule.Spec, rel) {
			return true
		}
	}
	return false
}

func (p *Policy) current() (Mode, Rules) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode, p.rules
}

func sessionRules(tool string, specs []Spec) []Rule {
	if len(specs) == 0 {
		return []Rule{{Tool: tool}}
	}
	var out []Rule
	for _, s := range specs {
		if s.Kind != KindCommand {
			return []Rule{{Tool: tool}}
		}
		prefix := commandPrefix(s.Value)
		if prefix == "" {
			return nil
		}
		if r := (Rule{Tool: tool, Spec: prefix + " *"}); !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	return out
}

func commandPrefix(cmd string) string {
	fields := strings.Fields(cmd)
	switch {
	case len(fields) == 0:
		return ""
	case len(fields) > 1 && isSubcommand(fields[1]):
		return fields[0] + " " + fields[1]
	}
	return fields[0]
}

func isSubcommand(s string) bool {
	if s[0] == '-' {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == ':') {
			return false
		}
	}
	return true
}

func describeRules(rules []Rule) string {
	parts := make([]string, len(rules))
	for i, r := range rules {
		parts[i] = r.Tool
		if r.Spec != "" {
			parts[i] += "(" + r.Spec + ")"
		}
	}
	return strings.Join(parts, ", ")
}
