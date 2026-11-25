package prompt

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

const CacheBoundary = 1

type SkillSummary struct {
	Name        string
	Description string
}

type Context struct {
	CWD    string
	Shell  string
	Date   string
	Files  []ContextFile
	Skills []SkillSummary
}

func Build(c Context, tools []string) []string {
	return []string{baseInstruction(tools), projectContext(c)}
}

func ContextFromEnv(cwd, shell string) Context {
	return Context{
		CWD:   cwd,
		Shell: shell,
		Date:  nowDate(),
	}
}

var toolOrder = []string{"glob", "grep", "read", "edit", "write", "bash", "exit_plan_mode"}

var toolLines = map[string]string{
	"glob": "- glob: find files by name pattern. Use it first when you do not know where something lives. " +
		"A pattern without a slash matches file names (*.go); a pattern with a slash matches relative paths " +
		"and ** crosses directories (src/**/*.ts).",
	"grep": "- grep: search file contents by regular expression (RE2 syntax). Use it to locate symbols, " +
		"strings and call sites. It skips .git, gitignored paths and binary files.",
	"read": "- read: read a file, or list a directory. Output is line-numbered and capped; when truncated, " +
		"the notice gives the offset to continue from.",
	"edit":  "- edit: replace an exact string in a file you have read. old_string must match exactly and be unique, or set replace_all. Prefer it over rewriting a whole file.",
	"write": "- write: create a file, or replace one you have read in full. Missing parent directories are created.",
	"bash": "- bash: run a shell command. Use it for real binaries and short fact pipelines " +
		"(counts, checksums, file metadata), never for what glob, grep or read already do.",
	"exit_plan_mode": "- exit_plan_mode: in plan mode, present the finished plan for the user's approval; approval ends plan mode.",
}

func baseInstruction(tools []string) string {
	present := make(map[string]bool, len(tools))
	for _, t := range tools {
		present[t] = true
	}

	var b strings.Builder
	b.WriteString("You are omp, a coding agent running in the user's terminal.\n\n# Tools\n")

	var listed []string
	for _, name := range toolOrder {
		if present[name] && toolLines[name] != "" {
			listed = append(listed, toolLines[name])
		}
	}
	if len(listed) == 0 {
		b.WriteString("You have no tools. Answer from the conversation alone.\n")
	} else {
		b.WriteString("Use these instead of guessing.\n")
		for _, line := range listed {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n# Working rules\n")
	b.WriteString("- Never invent a path, symbol or line number.")
	switch {
	case present["grep"] && present["read"]:
		b.WriteString(" Locate it with grep, then read it.")
	case present["glob"] && present["read"]:
		b.WriteString(" Locate it with glob, then read it.")
	}
	b.WriteString("\n")
	if present["read"] {
		b.WriteString("- Once you know where to look, read ranges instead of whole files.\n")
	}
	b.WriteString("- Cite evidence as path:line so the user can check it.\n")
	if present["grep"] {
		b.WriteString("- An empty search result is proof of absence only when the pattern was specific. " +
			"If a search times out or was truncated, narrow it rather than concluding.\n")
	}

	var missing []string
	if !present["edit"] && !present["write"] {
		missing = append(missing, "edit files")
	}
	if !present["bash"] {
		missing = append(missing, "run commands")
	}
	if len(missing) > 0 {
		b.WriteString("- You cannot " + strings.Join(missing, " or ") +
			". If the task requires that, say so plainly instead of describing work you cannot do.\n")
	}

	b.WriteString("\n# Answering\n")
	b.WriteString("- Answer the question that was asked, in the language it was asked in.\n")
	b.WriteString("- Be concise: no filler, no restating the question.\n")
	b.WriteString("- Separate what you verified by reading from what you inferred. " +
		"Never claim to have read something you did not.")
	return b.String()
}

func projectContext(c Context) string {
	var b strings.Builder
	b.WriteString("<project-context>\n")
	if c.CWD != "" {
		fmt.Fprintf(&b, "- Working directory: %s\n", c.CWD)
	}
	fmt.Fprintf(&b, "- OS: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	if c.Date != "" {
		fmt.Fprintf(&b, "- Date: %s\n", c.Date)
	}
	if c.Shell != "" {
		fmt.Fprintf(&b, "- Shell: %s\n", c.Shell)
	}
	if len(c.Skills) > 0 {
		b.WriteString("\n<skills>\n")
		b.WriteString("Load one with the skill tool before using it.\n")
		for _, sk := range c.Skills {
			fmt.Fprintf(&b, "- %s: %s\n", sk.Name, sk.Description)
		}
		b.WriteString("</skills>\n")
	}
	if len(c.Files) > 0 {
		b.WriteString("\n<repo-rules>\n")
		b.WriteString("Follow these repository context files for all tasks.\n")
		for _, f := range c.Files {
			fmt.Fprintf(&b, "<file path=\"%s\">\n%s\n</file>\n", f.Path, strings.TrimRight(f.Content, "\r\n"))
		}
		b.WriteString("</repo-rules>\n")
	}
	b.WriteString("</project-context>")
	return b.String()
}

func nowDate() string { return time.Now().Format("2006-01-02") }
