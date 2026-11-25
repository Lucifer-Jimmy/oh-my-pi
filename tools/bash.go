package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/sysproc"
)

const (
	bashDefaultTimeout = 120 * time.Second
	bashMaxTimeout     = 600 * time.Second

	bashHeadKeep = 12 * 1024
	bashTailKeep = 18 * 1024
)

const (
	shellGitBash    = "Git Bash"
	shellBash       = "bash"
	shellPwsh       = "PowerShell 7"
	shellPowerShell = "Windows PowerShell"
	shellCmd        = "cmd"
)

var bashWaitDelay = 2 * time.Second

var bashEnv = []string{
	"CI=1", "TERM=dumb", "NO_COLOR=1",
	"PAGER=cat", "GIT_PAGER=cat", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never",
}

type bashArgs struct {
	Command   string `json:"command"`
	Dir       string `json:"dir,omitempty"`
	TimeoutMs int    `json:"timeout_ms,omitempty"`
}

type Shell struct {
	Path string
	Args []string
	Name string
}

type Bash struct {
	CWD        string
	Workspaces []string
	Shell      Shell

	once     sync.Once
	resolved Shell
}

func (t *Bash) Name() string            { return "bash" }
func (t *Bash) Description() string     { return bashDescription + shellNote(t.shell().Name) }
func (t *Bash) Schema() json.RawMessage { return json.RawMessage(bashSchema) }

func (t *Bash) IsReadOnly() bool                       { return false }
func (t *Bash) IsConcurrencySafe(json.RawMessage) bool { return false }

func (t *Bash) Approval(input json.RawMessage) agent.Approval {
	var a bashArgs
	if err := json.Unmarshal(input, &a); err != nil || strings.TrimSpace(a.Command) == "" {
		return agent.Approval{Tier: agent.TierExec, Reason: "unreadable command"}
	}
	if dangerousCommand(a.Command) {
		return agent.Approval{Tier: agent.TierExec, ForcePrompt: true, Reason: "destructive command"}
	}
	if t.shell().Name != shellCmd && inspectOnly(a.Command) {
		return agent.Approval{Tier: agent.TierWrite}
	}
	return agent.Approval{Tier: agent.TierExec}
}

func (t *Bash) ReviewSpecs(input json.RawMessage) []permission.Spec {
	var a bashArgs
	if err := json.Unmarshal(input, &a); err != nil {
		return nil
	}
	return permission.SplitCommand(a.Command)
}

func (t *Bash) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{Destructive: true, OpenWorld: true, MaxResultSize: bashResultCap}
}

func (t *Bash) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[bashArgs](raw, "bash")
	if bad != nil {
		return *bad
	}
	if strings.TrimSpace(a.Command) == "" {
		return fail("bash: command is required")
	}
	dir, err := resolve(runRoots(t.CWD, t.Workspaces, tc), a.Dir)
	if err != nil {
		return fail("bash: " + err.Error())
	}
	timeout := bashDefaultTimeout
	if a.TimeoutMs > 0 {
		timeout = min(time.Duration(a.TimeoutMs)*time.Millisecond, bashMaxTimeout)
	}

	sh := t.shell()
	command := a.Command
	if sh.Name == shellPowerShell || sh.Name == shellPwsh {
		command = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; " + command
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, sh.Path, append(append([]string{}, sh.Args...), command)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), bashEnv...)
	cmd.WaitDelay = bashWaitDelay
	sysproc.Prepare(cmd)

	out := &capWriter{}
	cmd.Stdout = out
	cmd.Stderr = out
	runErr := cmd.Run()
	output := out.String()

	switch {
	case ctx.Err() != nil:
		return fail("bash: aborted by user" + outputBlock(output))
	case runCtx.Err() == context.DeadlineExceeded:
		return fail(fmt.Sprintf("bash: timed out after %s%s", timeout, outputBlock(output)))
	case runErr != nil:
		return fail("bash: " + describeRunError(runErr, sh.Path) + outputBlock(output))
	}
	if strings.TrimSpace(output) == "" {
		return ok("(no output)")
	}
	return ok(strings.TrimRight(output, "\r\n"))
}

func (t *Bash) shell() Shell {
	if t.Shell.Path != "" {
		return t.Shell
	}
	t.once.Do(func() { t.resolved = ResolveShell("") })
	return t.resolved
}

func describeRunError(err error, shell string) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Sprintf("exited with code %d", exitErr.ExitCode())
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return fmt.Sprintf("could not start shell %q: %v", shell, execErr.Err)
	}
	return err.Error()
}

func outputBlock(output string) string {
	s := strings.TrimRight(output, "\r\n")
	if s == "" {
		return ""
	}
	return "\n--- output ---\n" + s
}

type capWriter struct {
	mu      sync.Mutex
	head    []byte
	tail    []byte
	dropped int
}

func (w *capWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if room := bashHeadKeep - len(w.head); room > 0 {
		take := min(room, len(p))
		w.head = append(w.head, p[:take]...)
		p = p[take:]
	}
	w.tail = append(w.tail, p...)
	if len(w.tail) > 2*bashTailKeep {
		cut := len(w.tail) - bashTailKeep
		w.dropped += cut
		w.tail = append(w.tail[:0], w.tail[cut:]...)
	}
	return n, nil
}

func (w *capWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	head, tail, dropped := w.head, w.tail, w.dropped
	if extra := len(tail) - bashTailKeep; extra > 0 {
		tail = tail[extra:]
		dropped += extra
	}
	if dropped == 0 {
		return string(head) + string(tail)
	}
	for len(head) > 0 {
		r, size := utf8.DecodeLastRune(head)
		if r != utf8.RuneError || size > 1 {
			break
		}
		head = head[:len(head)-1]
		dropped++
	}
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
		dropped++
	}
	return string(head) + fmt.Sprintf("\n... [%d bytes elided] ...\n", dropped) + string(tail)
}

var inspectCommands = map[string]bool{
	"ls": true, "pwd": true, "cat": true, "head": true, "tail": true, "wc": true,
	"echo": true, "which": true, "where": true, "grep": true, "type": true,
	"get-childitem": true, "get-content": true, "get-location": true,
	"select-string": true, "test-path": true,
}

var gitInspect = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true,
	"rev-parse": true, "ls-files": true, "blame": true,
}

var riskyFlags = []string{"--output", "--ext-diff", "--pre", "--exec", "-exec", "-delete"}

func inspectOnly(command string) bool {
	specs := permission.SplitCommand(command)
	for _, s := range specs {
		if !inspectSegment(s.Value) {
			return false
		}
	}
	return len(specs) > 0
}

func inspectSegment(seg string) bool {
	fields := strings.Fields(seg)
	if len(fields) == 0 || strings.ContainsAny(fields[0], `/\`) {
		return false
	}
	for _, f := range fields[1:] {
		lf := strings.ToLower(f)
		for _, r := range riskyFlags {
			if lf == r || strings.HasPrefix(lf, r+"=") {
				return false
			}
		}
	}
	name := commandName(fields[0])
	if name == "git" {
		return len(fields) > 1 && gitInspect[fields[1]]
	}
	return inspectCommands[name]
}

func dangerousCommand(cmd string) bool {
	lowered := strings.ToLower(cmd)
	for _, pattern := range []string{
		"rm -rf", "rm -fr", "rmdir /s", "del /f", "del /s", "format ",
		"mkfs", "dd if=", "> /dev/sd", "git reset --hard", "git clean -fd",
		"git clean -f", "git push --force", "git push -f", "shutdown",
		"drop database", "drop table", "truncate table", "taskkill /f",
		"chmod 777", "chown -r", ":(){", "mv /* ",
	} {
		if strings.Contains(lowered, pattern) {
			return true
		}
	}
	return false
}

var (
	shellLookPath = exec.LookPath
	shellExists   = func(p string) bool {
		info, err := os.Stat(p)
		return err == nil && !info.IsDir()
	}
)

func ResolveShell(explicit string) Shell {
	if explicit != "" {
		return shellFor(explicit)
	}
	if runtime.GOOS == "windows" {
		if p, ok := gitBash(); ok {
			return Shell{Path: p, Args: []string{"-c"}, Name: shellGitBash}
		}
		for _, name := range []string{"pwsh", "powershell", "cmd"} {
			if p, err := shellLookPath(name); err == nil {
				return shellFor(p)
			}
		}
		return Shell{Path: "cmd", Args: []string{"/C"}, Name: shellCmd}
	}
	for _, name := range []string{"bash", "sh"} {
		if p, err := shellLookPath(name); err == nil {
			return shellFor(p)
		}
	}
	return Shell{Path: "sh", Args: []string{"-c"}, Name: "sh"}
}

func gitBash() (string, bool) {
	git, err := shellLookPath("git")
	if err != nil {
		return "", false
	}
	dir := filepath.Dir(git)
	for _, candidate := range []string{
		filepath.Join(dir, "..", "bin", "bash.exe"),
		filepath.Join(dir, "..", "..", "bin", "bash.exe"),
	} {
		if shellExists(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func shellFor(p string) Shell {
	switch name := commandName(p); name {
	case "pwsh":
		return Shell{Path: p, Args: []string{"-NoProfile", "-NonInteractive", "-Command"}, Name: shellPwsh}
	case "powershell":
		return Shell{Path: p, Args: []string{"-NoProfile", "-NonInteractive", "-Command"}, Name: shellPowerShell}
	case "cmd":
		return Shell{Path: p, Args: []string{"/C"}, Name: shellCmd}
	case "bash":
		return Shell{Path: p, Args: []string{"-c"}, Name: shellBash}
	default:
		return Shell{Path: p, Args: []string{"-c"}, Name: name}
	}
}

func commandName(s string) string {
	s = strings.ReplaceAll(s, `\`, "/")
	return strings.TrimSuffix(strings.ToLower(path.Base(s)), ".exe")
}

func shellNote(name string) string {
	switch name {
	case "":
		return ""
	case shellPowerShell:
		return "\nCommands run under Windows PowerShell 5.1: use PowerShell syntax and chain commands with ; because && is not supported."
	case shellPwsh:
		return "\nCommands run under PowerShell 7: use PowerShell syntax."
	case shellCmd:
		return "\nCommands run under cmd.exe: use cmd syntax."
	}
	return "\nCommands run under " + name + "."
}

const bashSchema = `{
  "type": "object",
  "properties": {
    "command":    {"type": "string", "description": "Command to run in the workspace shell."},
    "dir":        {"type": "string", "description": "Working directory relative to the workspace root. Defaults to the root."},
    "timeout_ms": {"type": "integer", "description": "Timeout in milliseconds. Defaults to 120000, capped at 600000."}
  },
  "required": ["command"],
  "additionalProperties": false
}`

const bashDescription = `Run a shell command in the workspace and return its combined stdout and stderr.
Non-zero exit, timeout and cancellation are reported as errors with the output attached.
Output is capped; the middle is elided with a byte count. Prefer read, glob and grep over shell equivalents.`

var _ agent.Tool = (*Bash)(nil)
var _ permission.Reviewable = (*Bash)(nil)
