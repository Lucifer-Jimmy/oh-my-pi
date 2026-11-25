package background

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

const (
	tailLines    = 8
	summaryChars = 200
)

type Task struct {
	ID      string
	Command string
	LogPath string
	Started time.Time
	Done    bool
	Exit    int
	Err     string
	cmd     *exec.Cmd

	mu sync.Mutex
}

type Manager struct {
	dir   string
	shell tools.Shell
	mu    sync.Mutex
	tasks map[string]*Task
	order []string
	queue []agent.Notification
}

func New(dir string, shell tools.Shell) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{dir: dir, shell: shell, tasks: map[string]*Task{}}, nil
}

func NewDefault(shell tools.Shell) (*Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(home, ".omp", "background"), shell)
}

func (m *Manager) nextID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("bg-%d", len(m.order)+1)
}

func (m *Manager) Start(cwd, command string) (*Task, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("background: command is required")
	}
	id := m.nextID()
	logPath := filepath.Join(m.dir, id+".log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	args := append(append([]string{}, m.shell.Args...), command)
	cmd := exec.Command(m.shell.Path, args...)
	cmd.Dir = cwd
	cmd.Stdout, cmd.Stderr = logFile, logFile
	task := &Task{ID: id, Command: command, LogPath: logPath, Started: time.Now(), Exit: -1, cmd: cmd}

	m.mu.Lock()
	m.tasks[id] = task
	m.order = append(m.order, id)
	m.mu.Unlock()

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		m.complete(task, -1, err.Error())
		return task, nil
	}
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		code := 0
		msg := ""
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code, msg = -1, err.Error()
			}
		}
		m.complete(task, code, msg)
	}()
	return task, nil
}

func (m *Manager) complete(t *Task, code int, errMsg string) {
	t.mu.Lock()
	t.Exit, t.Err = code, errMsg
	t.mu.Unlock()
	m.queueNotification(t)
	t.mu.Lock()
	t.Done = true
	t.mu.Unlock()
}

func (m *Manager) queueNotification(t *Task) {
	t.mu.Lock()
	var status string
	switch {
	case t.Exit == 0:
		status = "succeeded"
	case t.Exit > 0:
		status = fmt.Sprintf("exited with code %d", t.Exit)
	default:
		status = "failed to start: " + t.Err
	}
	t.mu.Unlock()
	text := fmt.Sprintf("%s (started %s)\nlog: %s\nlast lines:\n%s",
		status, t.Started.Format("15:04:05"), t.LogPath, tailOf(t.LogPath, tailLines))
	m.mu.Lock()
	m.queue = append(m.queue, agent.Notification{ID: t.ID, Text: text})
	m.mu.Unlock()
}

func (m *Manager) Take() []agent.Notification {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.queue
	m.queue = nil
	return out
}

func (m *Manager) Wait(ctx context.Context, id string, timeout time.Duration) (*Task, error) {
	m.mu.Lock()
	t, ok := m.tasks[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("background: no task %q", id)
	}
	deadline := time.Now().Add(timeout)
	for {
		if t.finished() {
			return t, nil
		}
		if time.Now().After(deadline) {
			return t, nil
		}
		select {
		case <-ctx.Done():
			return t, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (t *Task) finished() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Done
}

func (t *Task) Status() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case !t.Done:
		return "running"
	case t.Exit == 0:
		return "succeeded"
	case t.Exit > 0:
		return fmt.Sprintf("exited with code %d", t.Exit)
	}
	return "failed to start: " + t.Err
}

func (t *Task) Tail() string { return tailOf(t.LogPath, tailLines) }

func tailOf(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(log unreadable)"
	}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return "(no output yet)"
	}
	lines := strings.Split(text, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := strings.Join(lines, "\n")
	if len(out) > summaryChars*4 {
		out = out[len(out)-summaryChars*4:]
	}
	return out
}
