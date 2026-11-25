package background

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

type fakeCtx struct{ cwd string }

func (c fakeCtx) CWD() string        { return c.cwd }
func (fakeCtx) Workspaces() []string { return nil }
func (fakeCtx) Emit(agent.Event)     {}
func (fakeCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, agent.ErrNoUI
}
func (fakeCtx) RecentMessages() []llm.Message { return nil }

func newManager(t *testing.T) *Manager {
	t.Helper()
	m, err := New(t.TempDir(), tools.ResolveShell(""))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return m
}

func TestStartRunsTheCommandAndQueuesAPointer(t *testing.T) {
	m := newManager(t)
	task, err := m.Start(t.TempDir(), "echo hello-background")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waited, err := m.Wait(context.Background(), task.ID, 30*time.Second)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if waited.Status() != "succeeded" {
		t.Fatalf("status = %q (err %q)", waited.Status(), waited.Err)
	}
	body, err := os.ReadFile(task.LogPath)
	if err != nil || !strings.Contains(string(body), "hello-background") {
		t.Fatalf("the full output must be in the log: %q %v", body, err)
	}
	notes := m.Take()
	if len(notes) != 1 || notes[0].ID != task.ID {
		t.Fatalf("notifications = %+v", notes)
	}
	text := notes[0].Text
	if !strings.Contains(text, task.LogPath) || !strings.Contains(text, "hello-background") {
		t.Errorf("the notification must carry the log path and the tail: %q", text)
	}
	if strings.Contains(text, "exited") {
		t.Errorf("a successful task must not look like a failure: %q", text)
	}
	if again := m.Take(); len(again) != 0 {
		t.Errorf("notifications are delivered once: %+v", again)
	}
}

func TestFailingCommandIsReportedWithItsCode(t *testing.T) {
	m := newManager(t)
	task, err := m.Start(t.TempDir(), "exit 3")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	waited, _ := m.Wait(context.Background(), task.ID, 30*time.Second)
	if got := waited.Status(); !strings.Contains(got, "code 3") {
		t.Fatalf("status = %q", got)
	}
	if notes := m.Take(); len(notes) != 1 || !strings.Contains(notes[0].Text, "code 3") {
		t.Fatalf("notification = %+v", notes)
	}
}

func TestWaitTimesOutAndSaysStillRunning(t *testing.T) {
	m := newManager(t)
	task, err := m.Start(t.TempDir(), "sleep 1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	waited, err := m.Wait(context.Background(), task.ID, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("a timeout is not an error: %v", err)
	}
	if waited.Status() != "running" {
		t.Fatalf("status = %q", waited.Status())
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("wait ignored its timeout: %s", elapsed)
	}
	if _, err := m.Wait(context.Background(), task.ID, 30*time.Second); err != nil {
		t.Fatalf("second wait: %v", err)
	}
}

func TestMissingCommandAndUnknownID(t *testing.T) {
	m := newManager(t)
	if _, err := m.Start(t.TempDir(), "   "); err == nil || !strings.Contains(err.Error(), "command is required") {
		t.Errorf("err = %v", err)
	}
	if _, err := m.Wait(context.Background(), "nope", time.Second); err == nil || !strings.Contains(err.Error(), "no task") {
		t.Errorf("err = %v", err)
	}
}

func runTool(t *testing.T, tool *Tool, cwd string, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), fakeCtx{cwd: cwd}, raw)
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestToolStartWaitAndList(t *testing.T) {
	m := newManager(t)
	tool := &Tool{Manager: m}
	dir := t.TempDir()

	got, isErr := runTool(t, tool, dir, map[string]any{"action": "start", "command": "echo tool-ran"})
	if isErr || !strings.Contains(got, "bg-1") || !strings.Contains(got, "log:") {
		t.Fatalf("start = %q err=%v", got, isErr)
	}
	got, isErr = runTool(t, tool, dir, map[string]any{"action": "wait", "id": "bg-1", "timeout_seconds": 30})
	if isErr || !strings.Contains(got, "tool-ran") {
		t.Fatalf("wait = %q err=%v", got, isErr)
	}
	got, _ = runTool(t, tool, dir, map[string]any{"action": "list"})
	if !strings.Contains(got, "bg-1") || !strings.Contains(got, "succeeded") {
		t.Fatalf("list = %q", got)
	}
}

func TestToolRejectsBadActions(t *testing.T) {
	tool := &Tool{Manager: newManager(t)}
	if got, isErr := runTool(t, tool, t.TempDir(), map[string]any{"action": "explode"}); !isErr || !strings.Contains(got, `"start"`) {
		t.Errorf("bad action: %q %v", got, isErr)
	}
	if got, isErr := runTool(t, tool, t.TempDir(), map[string]any{"action": "wait"}); !isErr || !strings.Contains(got, "id is required") {
		t.Errorf("missing id: %q %v", got, isErr)
	}
	if got, isErr := runTool(t, tool, t.TempDir(), map[string]any{"action": "start"}); !isErr || !strings.Contains(got, "command is required") {
		t.Errorf("missing command: %q %v", got, isErr)
	}
}

func TestToolApprovalIsExec(t *testing.T) {
	tool := &Tool{Manager: newManager(t)}
	if tool.Approval(nil).Tier != agent.TierExec {
		t.Errorf("a background command runs code and must be exec tier")
	}
	if tool.IsConcurrencySafe(nil) {
		t.Errorf("background runs must not share a slot with other tools")
	}
}

func TestLogPathIsInsideTheManagerDir(t *testing.T) {
	dir := t.TempDir()
	m, err := New(dir, tools.ResolveShell(""))
	if err != nil {
		t.Fatal(err)
	}
	task, err := m.Start(t.TempDir(), "echo x")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	m.Wait(context.Background(), task.ID, 30*time.Second)
	if filepath.Dir(task.LogPath) != dir {
		t.Errorf("log path %q is outside %q", task.LogPath, dir)
	}
}
