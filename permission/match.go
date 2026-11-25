package permission

import (
	"regexp"
	"strings"
)

var wrappers = map[string]bool{
	"time": true, "timeout": true, "nice": true, "nohup": true,
	"stdbuf": true, "command": true, "builtin": true, "exec": true,
}

var harmlessRedirect = regexp.MustCompile(`(?:^|\s)(?:\d|&)?>>?\s*/dev/null\b|(?:^|\s)\d?>&\d\b`)

func SplitCommand(cmd string) []Spec {
	cmd = harmlessRedirect.ReplaceAllString(cmd, " ")
	segs, ok := splitSegments(cmd)
	if !ok {
		return []Spec{{Kind: KindCommand, Value: ""}}
	}

	var specs []Spec
	for _, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		specs = append(specs, Spec{Kind: KindCommand, Value: stripWrappers(seg)})
	}
	if len(specs) == 0 {
		return []Spec{{Kind: KindCommand, Value: ""}}
	}
	return specs
}

func splitSegments(cmd string) ([]string, bool) {
	var out []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range cmd {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			if r == '\\' && quote == '"' {
				escaped = true
				continue
			}
			b.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == '\\':
			escaped = true
		case r == ';' || r == '\n' || r == '&' || r == '|':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, false
	}
	out = append(out, b.String())
	if hasOpaque(cmd) {
		return nil, false
	}
	return out, true
}

func hasOpaque(cmd string) bool {
	var quote rune
	escaped := false
	for _, r := range cmd {
		switch {
		case escaped:
			escaped = false
		case quote != 0:
			if r == quote {
				quote = 0
			} else if r == '\\' && quote == '"' {
				escaped = true
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '\\':
			escaped = true
		case r == '>' || r == '<':
			return true
		case r == '$' || r == '`':
			return true
		}
	}
	return false
}

func stripWrappers(seg string) string {
	fields := strings.Fields(seg)
	for len(fields) > 0 && wrappers[fields[0]] {
		fields = fields[1:]
		for len(fields) > 0 && (strings.HasPrefix(fields[0], "-") || isNumericDuration(fields[0])) {
			fields = fields[1:]
		}
	}
	return strings.Join(fields, " ")
}

func isNumericDuration(s string) bool {
	if s == "" {
		return false
	}
	digits := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case r == '.' || r == 's' || r == 'm' || r == 'h' || r == 'd':
		default:
			return false
		}
	}
	return digits
}

func matchCommand(rule, cmd string) bool {
	rule = strings.TrimSpace(rule)
	if rule == "" || rule == "*" {
		return true
	}
	if prefix, ok := strings.CutSuffix(rule, "*"); ok {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" {
			return true
		}
		return cmd == prefix || strings.HasPrefix(cmd, prefix+" ")
	}
	return cmd == rule
}

func matchPath(rule, path string) bool {
	rule = strings.TrimPrefix(strings.ReplaceAll(rule, "\\", "/"), "./")
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "./")
	if rule == "" || path == "" {
		return false
	}
	return globMatch(strings.Split(rule, "/"), strings.Split(path, "/"))
}

func globMatch(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if globMatch(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if !segMatch(pat[0], name[0]) {
		return false
	}
	return globMatch(pat[1:], name[1:])
}

func segMatch(pat, s string) bool {
	p := []rune(pat)
	v := []rune(s)
	pi, vi := 0, 0
	star, mark := -1, 0
	for vi < len(v) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == v[vi]):
			pi++
			vi++
		case pi < len(p) && p[pi] == '*':
			star = pi
			mark = vi
			pi++
		case star >= 0:
			pi = star + 1
			mark++
			vi = mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
