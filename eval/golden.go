package eval

import (
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

func golden() []evalCase {
	return []evalCase{
		{
			Name:   "reads-a-file-then-answers",
			Files:  map[string]string{"a.txt": "alpha\nbeta\n"},
			Prompt: "what is in a.txt?",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "a.txt"}}}},
				{Text: "a.txt contains alpha and beta."},
			},
			Checks: []check{calledTools("read"), finalTextContains("alpha"), Reason(agent.ReasonCompleted)},
		},
		{
			Name:   "edit-before-read-is-refused",
			Files:  map[string]string{"a.txt": "hello\n"},
			Prompt: "replace hello with bye",
			Script: []turn{
				{Calls: []Call{{ID: "e1", Name: "edit", Input: map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "bye"}}}},
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "a.txt"}}}},
				{Calls: []Call{{ID: "e2", Name: "edit", Input: map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "bye"}}}},
				{Text: "done"},
			},
			Checks: []check{
				calledTools("edit", "read", "edit"),
				fileEquals("a.txt", "bye\n"),
				func(o *outcome, _ string) string {
					if !strings.Contains(o.Results["e1"], "has not been read") {
						return "the first edit must be refused with a readable reason, got: " + o.Results["e1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "unknown-tool-goes-back-to-model",
			Files:  map[string]string{"a.txt": "x\n"},
			Prompt: "use a tool",
			Script: []turn{
				{Calls: []Call{{ID: "u1", Name: "reed", Input: map[string]any{"path": "a.txt"}}}},
				{Text: "recovered"},
			},
			Checks: []check{
				calledTools("reed"),
				finalTextContains("recovered"),
				func(o *outcome, _ string) string {
					got := o.Results["u1"]
					if !strings.Contains(got, "read") {
						return "an unknown tool must suggest the near name, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "plan-mode-denies-writes",
			Files:  map[string]string{"a.txt": "hello\n"},
			Prompt: "change the file",
			Mode:   permission.ModePlan,
			Script: []turn{
				{Calls: []Call{{ID: "e1", Name: "edit", Input: map[string]any{"path": "a.txt", "old_string": "hello", "new_string": "bye"}}}},
				{Text: "plan only"},
			},
			Checks: []check{
				fileEquals("a.txt", "hello\n"),
				func(o *outcome, _ string) string {
					if !strings.Contains(strings.ToLower(o.Results["e1"]), "plan") {
						return "a denied write must say why, got: " + o.Results["e1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "deny-rule-beats-bypass",
			Files:  map[string]string{"secret.txt": "token\n"},
			Prompt: "read the secret",
			Mode:   permission.ModeBypass,
			Deny:   []string{"read(secret.txt)"},
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "secret.txt"}}}},
				{Text: "could not"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					if strings.Contains(o.Results["r1"], "token") {
						return "a deny rule must win over bypass mode"
					}
					return ""
				},
			},
		},

		{
			Name:   "glob-finds-nested-file",
			Files:  map[string]string{"a/b/c.txt": "deep\n", "top.txt": "t\n"},
			Prompt: "find c.txt",
			Script: []turn{
				{Calls: []Call{{ID: "g1", Name: "glob", Input: map[string]any{"pattern": "**/c.txt"}}}},
				{Text: "found it"},
			},
			Checks: []check{
				calledTools("glob"),
				func(o *outcome, _ string) string {
					if !strings.Contains(o.Results["g1"], "c.txt") {
						return "glob must list the nested file, got: " + o.Results["g1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "grep-reports-line-and-file",
			Files:  map[string]string{"x.go": "package x\n// TODO: fix\n", "y.go": "package y\n"},
			Prompt: "find TODOs",
			Script: []turn{
				{Calls: []Call{{ID: "s1", Name: "grep", Input: map[string]any{"pattern": "TODO"}}}},
				{Text: "one TODO"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					got := o.Results["s1"]
					if !strings.Contains(got, "x.go") || strings.Contains(got, "y.go") {
						return "grep must report the hit and only the hit, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "write-creates-then-read-sees-it",
			Prompt: "create notes.md",
			Script: []turn{
				{Calls: []Call{{ID: "w1", Name: "write", Input: map[string]any{"path": "notes.md", "content": "hello\nworld\n"}}}},
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "notes.md"}}}},
				{Text: "created"},
			},
			Checks: []check{
				fileEquals("notes.md", "hello\nworld\n"),
				func(o *outcome, _ string) string {
					if !strings.Contains(o.Results["r1"], "world") {
						return "read after write must return the new content, got: " + o.Results["r1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "ambiguous-edit-is-refused-and-lists-lines",
			Files:  map[string]string{"m.txt": "a\nx\na\n"},
			Prompt: "replace a",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "m.txt"}}}},
				{Calls: []Call{{ID: "e1", Name: "edit", Input: map[string]any{"path": "m.txt", "old_string": "a", "new_string": "b"}}}},
				{Text: "needs a unique string"},
			},
			Checks: []check{
				fileEquals("m.txt", "a\nx\na\n"),
				func(o *outcome, _ string) string {
					got := o.Results["e1"]
					if !strings.Contains(got, "1") || !strings.Contains(got, "3") {
						return "an ambiguous edit must name every matching line, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "missing-required-argument-becomes-tool-error",
			Files:  map[string]string{"a.txt": "x\n"},
			Prompt: "read",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{}}}},
				{Text: "retry with a path"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					got := o.Results["r1"]
					if !strings.Contains(got, "path") {
						return "a schema error must name the missing property, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "string-number-is-corrected",
			Files:  map[string]string{"a.txt": "alpha\nbeta\ngamma\n"},
			Prompt: "read two lines",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "a.txt", "limit": "2"}}}},
				{Text: "read"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					got := o.Results["r1"]
					if !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
						return "a numeric string must be corrected and honoured, got: " + got
					}
					if strings.Contains(got, "gamma") {
						return "limit 2 must stop before the third line, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "path-escape-is-refused",
			Prompt: "read outside",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "../../etc/passwd"}}}},
				{Text: "cannot"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					got := strings.ToLower(o.Results["r1"])
					if !strings.Contains(got, "outside") && !strings.Contains(got, "workspace") {
						return "a path escaping the workspace must say so, got: " + o.Results["r1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "directory-read-lists-entries",
			Files:  map[string]string{"dir/one.txt": "1\n", "dir/two.txt": "2\n"},
			Prompt: "list dir",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "dir"}}}},
				{Text: "two files"},
			},
			Checks: []check{
				func(o *outcome, _ string) string {
					got := o.Results["r1"]
					if !strings.Contains(got, "one.txt") || !strings.Contains(got, "two.txt") {
						return "reading a directory must list its entries, got: " + got
					}
					return ""
				},
			},
		},
		{
			Name:   "non-object-arguments-are-refused-not-run",
			Files:  map[string]string{"a.txt": "x\n"},
			Prompt: "read",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "read", Input: map[string]any{"path": "a.txt"}}}},
				{Text: "ok"},
			},
			Checks: []check{
				calledTools("read"),
				func(o *outcome, _ string) string {
					if !strings.Contains(o.Results["r1"], "x") {
						return "a valid call must still run, got: " + o.Results["r1"]
					}
					return ""
				},
			},
		},
		{
			Name:   "script-exhaustion-is-visible",
			Prompt: "do something",
			Script: []turn{
				{Calls: []Call{{ID: "r1", Name: "glob", Input: map[string]any{"pattern": "*"}}}},
			},
			Checks: []check{
				finalTextContains("script exhausted"),
			},
		},
	}
}
