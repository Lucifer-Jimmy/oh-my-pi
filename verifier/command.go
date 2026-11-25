package verifier

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/sysproc"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

const (
	DefaultTimeout = 120 * time.Second
	outputCap      = 8 * 1024

	envCI       = "CI=1"
	envNoColor  = "NO_COLOR=1"
	envTermDumb = "TERM=dumb"
	envPager    = "PAGER=cat"
)

type Command struct {
	Command string
	Dir     string
	Shell   tools.Shell
}

func (c Command) Verify(ctx context.Context, _ session.Session) (agent.CheckResult, error) {
	if strings.TrimSpace(c.Command) == "" {
		return agent.CheckResult{}, errors.New("verifier: command is empty")
	}
	runCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	sh := c.Shell
	if sh.Path == "" {
		sh = tools.ResolveShell("")
	}
	command := c.Command
	if sh.Name == "powershell" || sh.Name == "pwsh" {
		command = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; " + command
	}

	cmd := exec.CommandContext(runCtx, sh.Path, append(append([]string{}, sh.Args...), command)...)
	cmd.Dir = c.Dir
	cmd.Env = append(os.Environ(), envCI, envNoColor, envTermDumb, envPager)
	cmd.WaitDelay = 5 * time.Second
	sysproc.Prepare(cmd)

	out := &bounded{}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()

	switch {
	case ctx.Err() != nil:
		return agent.CheckResult{}, ctx.Err()
	case runCtx.Err() == context.DeadlineExceeded:
		return agent.CheckResult{Feedback: fmt.Sprintf("%s\nthe check did not finish within %s", c.Command, DefaultTimeout)}, nil
	case err == nil:
		return agent.CheckResult{Passed: true}, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return agent.CheckResult{Feedback: fmt.Sprintf("%s\nexit status %d\n%s", c.Command, exitErr.ExitCode(), out.String())}, nil
	}
	return agent.CheckResult{}, fmt.Errorf("verifier: %s: %w", c.Command, err)
}

type bounded struct {
	mu   sync.Mutex
	b    strings.Builder
	more bool
}

func (w *bounded) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	if room := outputCap - w.b.Len(); room > 0 {
		if len(p) > room {
			w.b.Write(p[:room])
			w.more = true
		} else {
			w.b.Write(p)
		}
	} else {
		w.more = true
	}
	return n, nil
}

func (w *bounded) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.TrimSpace(w.b.String())
	if w.more {
		s += fmt.Sprintf("\n... (output trimmed at %d bytes)", outputCap)
	}
	return s
}
