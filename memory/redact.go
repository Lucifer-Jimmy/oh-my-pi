package memory

import (
	"regexp"
	"strings"
)

const redacted = "[REDACTED]"

var (
	fixedSecrets = []*regexp.Regexp{
		regexp.MustCompile(`(?:AKIA|ASIA)[A-Z0-9]{16}`),
		regexp.MustCompile(`(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
		regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`),
		regexp.MustCompile(`npm_[A-Za-z0-9]{30,}`),
		regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),
		regexp.MustCompile(`AIza[A-Za-z0-9_-]{30,}`),
	}
	jwtSecret     = regexp.MustCompile(`[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}`)
	keywordSecret = regexp.MustCompile(`(?i)(?:password|secret|token|key|tok|sk|pk|rk)[-_][A-Za-z0-9_-]+`)
	keywordPrefix = regexp.MustCompile(`(?i)^(?:password|secret|token|key|tok|sk|pk|rk)[-_]`)
)

func Redact(s string) string {
	s = keywordSecret.ReplaceAllStringFunc(s, func(m string) string {
		loc := keywordPrefix.FindStringIndex(m)
		if loc == nil || !credentialRun(m[loc[1]:]) {
			return m
		}
		return redacted
	})
	s = jwtSecret.ReplaceAllString(s, redacted)
	for _, re := range fixedSecrets {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}

func credentialRun(run string) bool {
	for _, seg := range strings.FieldsFunc(run, func(r rune) bool { return r == '-' || r == '_' }) {
		letters, digits := false, false
		for _, r := range seg {
			switch {
			case r >= '0' && r <= '9':
				digits = true
			default:
				letters = true
			}
		}
		switch {
		case letters && digits && len(seg) >= 12:
			return true
		case letters && !digits && len(seg) >= 16:
			return true
		}
	}
	return false
}
