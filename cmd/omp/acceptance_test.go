package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/background"
	"github.com/Lucifer-Jimmy/oh-my-pi/hook"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/mcp"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/skill"
	"github.com/Lucifer-Jimmy/oh-my-pi/subagent"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

const helperEnv = "OMP_TEST_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "mcp":
		runMCPHelper()
		return
	case "hook":
		runHookHelper()
		return
	}
	if os.Getenv(killEnv) != "" {
		runKillHelper()
		return
	}
	os.Exit(m.Run())
}

func runMCPHelper() {
	err := mcp.ServeStdio(context.Background(), "browser", "1", []mcp.StdioTool{{
		Name:        "screenshot",
		Description: "capture a page",
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"url": map[string]any{"type": "string"}},
			"required":   []string{"url"},
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			url, _ := args["url"].(string)
			if url == "" {
				return "", fmt.Errorf("url is required")
			}
			return "captured " + url, nil
		},
	}})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runHookHelper() {
	marker := os.Getenv("OMP_HOOK_MARKER")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		os.Exit(1)
	}
	var ev struct {
		Event  string `json:"event"`
		Tool   string `json:"tool"`
		Result *struct {
			Text    string `json:"text"`
			IsError bool   `json:"is_error"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(line), &ev); err != nil {
		os.Exit(1)
	}
	f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		os.Exit(1)
	}
	fmt.Fprintf(f, "%s %s\n", ev.Event, ev.Tool)
	f.Close()
	if ev.Event == "post_tool" && ev.Result != nil && !ev.Result.IsError {
		out, _ := json.Marshal(map[string]any{
			"result": map[string]any{"text": ev.Result.Text + "\n[hooked]"},
		})
		fmt.Println(string(out))
	}
}

func helperHook(t *testing.T, marker string) hook.Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return hook.Spec{
		Command: exe,
		Env:     map[string]string{helperEnv: "hook", "OMP_HOOK_MARKER": marker},
		Timeout: 30 * time.Second,
	}
}

func gitOK(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type switcher struct {
	mu     sync.Mutex
	active func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]
}

func (s *switcher) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	s.mu.Lock()
	fn := s.active
	s.mu.Unlock()
	return fn(ctx, req)
}

func (s *switcher) use(fn func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]) {
	s.mu.Lock()
	s.active = fn
	s.mu.Unlock()
}

func TestM4FeatureSetWorksTogetherInOneSession(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skillDir := filepath.Join(dir, ".omp", "skills", "deploy")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"),
		[]byte("---\nname: deploy\ndescription: ships the service\n---\n\nRun the release script and report the version.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOK(t, dir, "init", "-q", ".")
	gitOK(t, dir, "add", "-A")
	gitOK(t, dir, "commit", "-q", "-m", "base")

	marker := filepath.Join(dir, "hooks.log")
	spec := helperHook(t, marker)
	cfg := config{}
	cfg.hooks = hooksSection{
		PreTool:  []hookEntry{{Command: spec.Command, Env: spec.Env}},
		PostTool: []hookEntry{{Command: spec.Command, Env: spec.Env}},
	}

	shell := tools.ResolveShell("")
	bg, err := background.New(filepath.Join(dir, "bg"), shell)
	if err != nil {
		t.Fatal(err)
	}
	skills := skill.Discover(dir)
	if len(skills) != 1 {
		t.Fatalf("the project skill must be discovered, got %d", len(skills))
	}

	parent := &stepModel{steps: []step{
		{id: "td", tool: "todo", input: map[string]any{"items": []map[string]any{
			{"content": "load the deploy skill", "status": "in_progress"},
			{"content": "call the browser tool", "status": "pending"},
			{"content": "delegate the edit", "status": "pending"},
		}}},
		{id: "sk", tool: "skill", input: map[string]any{"name": "deploy"}},
		{id: "mc", tool: "mcp__browser_screenshot", input: map[string]any{"url": "https://example.com"}},
		{id: "bg1", tool: "background", input: map[string]any{"action": "start", "command": "echo ready"}},
		{id: "bg2", tool: "background", input: map[string]any{"action": "wait", "id": "bg-1", "timeout_seconds": 30}},
		{id: "tk", tool: "task", input: map[string]any{
			"description": "make the file",
			"prompt":      "write made.txt containing hello",
			"isolated":    true,
		}},
		{id: "rd", tool: "read", input: map[string]any{"path": "note.txt"}},
		{id: "md", tool: "read", input: map[string]any{"path": "made.txt"}},
		{text: "all seven features are live"},
	}}
	sub := &stepModel{steps: []step{
		{id: "cw", tool: "write", input: map[string]any{"path": "made.txt", "content": "hello from the subagent\n"}},
		{text: "wrote made.txt"},
	}}
	swap := &switcher{active: parent.call}

	connect := func(ctx context.Context) []agent.Tool {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		pool := new(mcp.Pool)
		t.Cleanup(func() { pool.Close() })
		srv, err := pool.Connect(ctx, "browser", mcp.ServerConfig{Command: exe, Env: map[string]string{helperEnv: "mcp"}})
		if err != nil {
			t.Fatalf("mcp connect: %v", err)
		}
		return srv.Tools()
	}

	deps := agent.RunDeps{CallModel: swap.call, Notify: bg.Take}
	build := newTaskTool
	newTaskTool = func(c subagent.Config) *subagent.Task {
		ft := build(c)
		plain, isolated := ft.Run, ft.Isolated
		ft.Run = func(ctx context.Context, prompt string, progress func(agent.Event)) (subagent.Result, error) {
			swap.use(sub.call)
			defer swap.use(parent.call)
			return plain(ctx, prompt, progress)
		}
		ft.Isolated = func(ctx context.Context, prompt string, progress func(agent.Event)) (subagent.Result, error) {
			swap.use(sub.call)
			defer swap.use(parent.call)
			return isolated(ctx, prompt, progress)
		}
		return ft
	}
	defer func() { newTaskTool = build }()
	reg := assemble(dir, shell, permission.NewPolicy(permission.ModeBypass, permission.Rules{}), deps, cfg, skills, nil, bg, nil, connect)
	opts := options(cfg, dir, reg.Names(), shell.Name, skills)
	h := hookSet(cfg, dir).Hooks()
	opts.Hooks = agent.Hooks{PreTool: h.PreTool, PostTool: h.PostTool}

	loop := agent.NewLoop(reg, deps, nil, opts)
	sess := session.NewMemory(nil)
	results := map[string]agent.ToolResult{}
	var notes, done int
	var final string
	for ev, err := range loop.Run(context.Background(), sess, "do everything") {
		if err != nil {
			t.Fatal(err)
		}
		switch ev.Kind {
		case agent.EventToolResult:
			results[ev.ToolUseID] = *ev.ToolResult
		case agent.EventNotification:
			notes++
		case agent.EventResult:
			done++
			final = ev.Result.Text
			if ev.Result.Reason != agent.ReasonCompleted {
				t.Fatalf("run ended with %s: %s", ev.Result.Reason, ev.Result.Err)
			}
		}
	}
	if done != 1 {
		t.Fatalf("expected exactly one result event, got %d", done)
	}
	if final != "all seven features are live" {
		t.Fatalf("final text = %q", final)
	}

	check := func(id, want string) {
		t.Helper()
		r, ok := results[id]
		if !ok {
			t.Fatalf("%s: no result", id)
		}
		if r.IsError {
			t.Fatalf("%s failed: %s", id, text(r))
		}
		if want != "" && !strings.Contains(text(r), want) {
			t.Fatalf("%s = %q, want it to contain %q", id, text(r), want)
		}
	}
	check("td", "load the deploy skill")
	check("sk", "Run the release script")
	check("mc", "captured https://example.com")
	if b := text(results["bg1"]); !strings.Contains(b, "started") {
		t.Fatalf("background start = %q", b)
	}
	if w := text(results["bg2"]); !strings.Contains(w, "ready") {
		t.Fatalf("background wait must surface the real output, got %q", w)
	}
	if notes == 0 {
		t.Fatal("a finished background task must notify the loop at a round boundary")
	}
	if tk := text(results["tk"]); !strings.Contains(tk, "worktree") {
		t.Fatalf("the isolated task must report how it ran, got %q", tk)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "made.txt")); err != nil || !strings.Contains(string(b), "hello") {
		t.Fatalf("the subagent's file must land in the session tree: %q %v", b, err)
	}
	check("md", "hello from the subagent")
	check("rd", "[hooked]")

	logged, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the hooks never ran: %v", err)
	}
	for _, want := range []string{"pre_tool todo", "pre_tool skill", "pre_tool mcp__browser_screenshot", "post_tool read"} {
		if !strings.Contains(string(logged), want) {
			t.Fatalf("hook log missing %q:\n%s", want, logged)
		}
	}
}
