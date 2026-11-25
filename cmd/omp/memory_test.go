package main

import (
	"bytes"
	"context"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

func useHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return home
}

func TestMemoryBackendIsReadFromTheConfigFile(t *testing.T) {
	f, err := loadConfigFile(writeConfig(t, "memory:\n  backend: local\nmemories:\n  maxRolloutsPerStartup: 8\n"))
	if err != nil {
		t.Fatal(err)
	}
	c, err := applyConfig(config{model: "m"}, f)
	if err != nil || c.memoryBackend != "local" {
		t.Fatalf("backend = %q, %v", c.memoryBackend, err)
	}
}

func TestOpenMemoryIsOffUnlessTheBackendIsLocal(t *testing.T) {
	useHome(t)
	cwd := t.TempDir()
	for _, backend := range []string{"", "off", "OFF"} {
		if m, note := openMemory(config{memoryBackend: backend}, cwd); m != nil || note != "" {
			t.Errorf("%q: memory must be off quietly", backend)
		}
	}
	if m, note := openMemory(config{memoryBackend: "hindsight"}, cwd); m != nil || !strings.Contains(note, `"hindsight" is not supported`) {
		t.Errorf("an unsupported backend is reported, not fatal: %q", note)
	}
	m, note := openMemory(config{memoryBackend: "Local"}, cwd)
	if m == nil || note != "" || m.snapshot != "" {
		t.Fatalf("local with no memory yet: %+v %q", m, note)
	}
	m.store.Learn("run go vet before go test", "")
	m, _ = openMemory(config{memoryBackend: "local"}, cwd)
	if !strings.Contains(m.snapshot, "run go vet before go test") {
		t.Fatalf("a saved lesson is part of the next session's memory:\n%s", m.snapshot)
	}
}

func TestMemoryJoinsThePromptAndTheToolSet(t *testing.T) {
	useHome(t)
	dir := t.TempDir()
	m, _ := openMemory(config{memoryBackend: "local"}, dir)
	m.store.Learn("the tests need -race", "")
	m, _ = openMemory(config{memoryBackend: "local"}, dir)

	steps := &stepModel{steps: []step{
		{id: "l1", tool: "learn", input: map[string]any{"memory": "build with go build ./... first"}},
		{text: "noted"},
	}}
	var system []string
	deps := agent.RunDeps{CallModel: func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		if system == nil {
			system = req.System
		}
		return steps.call(ctx, req)
	}}
	policy := permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{})
	reg := assemble(dir, tools.ResolveShell(""), policy, deps, config{}, nil, nil, nil, nil, nil)
	opts := agent.Options{System: []string{"base"}, CWD: dir, Workspaces: []string{dir}}
	m.attach(reg, &opts)
	if _, ok := reg.Get("learn"); !ok {
		t.Fatal("learn is registered when memory is on")
	}
	var out, errb bytes.Buffer
	if code := runPrint(context.Background(), agent.RunnerFunc(agent.NewLoop(reg, deps, nil, opts).Run), session.NewMemory(nil), []string{"go"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(system) != 2 || !strings.Contains(system[1], "<memory>") || !strings.Contains(system[1], "the tests need -race") {
		t.Fatalf("the memory block is the last system block: %q", system)
	}
	if !strings.Contains(m.store.Lessons(), "build with go build ./... first") {
		t.Fatalf("the model's lesson is saved: %q", m.store.Lessons())
	}

	var off *memorySetup
	reg2 := agent.NewRegistry()
	opts2 := agent.Options{System: []string{"base"}}
	off.attach(reg2, &opts2)
	if _, ok := reg2.Get("learn"); ok || len(opts2.System) != 1 {
		t.Fatal("memory off adds nothing")
	}
}

func TestMemoryControlReadsPastSessionsOnlyForSavedSessions(t *testing.T) {
	useHome(t)
	cwd := t.TempDir()
	m, _ := openMemory(config{memoryBackend: "local"}, cwd)

	unsaved := &conversation{sess: session.NewMemory(nil)}
	if c := m.control(context.Background(), unsaved, nil); c == nil || c.Pipeline != nil {
		t.Fatal("an unsaved session gets /memory but no pipeline")
	}
	if (*memorySetup)(nil).control(context.Background(), unsaved, nil) != nil {
		t.Fatal("memory off has no control")
	}

	root := t.TempDir()
	dir := session.ProjectDir(root, cwd)
	old, err := session.Create(dir, "20261101-100000-aaaaaaaa", cwd, "m")
	if err != nil {
		t.Fatal(err)
	}
	old.Append(llm.UserText("we always run make check"))
	old.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("ok")}})
	old.Close()
	idle := time.Now().Add(-13 * time.Hour)
	os.Chtimes(old.Path(), idle, idle)

	conv, err := openConversation(config{sessionsDir: root}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	defer conv.Close()
	replies := []string{`{"summary":"set up checks","memory":"- run make check before landing"}`, "## Checks\n- run make check before landing"}
	provider := &scriptedProvider{replies: replies}
	c := m.control(context.Background(), conv, provider)
	if c.Pipeline == nil || c.Pipeline.Current != conv.sessionID() || c.Pipeline.SessionsDir != conv.dir {
		t.Fatalf("pipeline = %+v", c.Pipeline)
	}
	st, err := c.Pipeline.Run(context.Background(), false)
	if err != nil || st.Read != 1 || !st.Summarized {
		t.Fatalf("status = %+v, %v", st, err)
	}
	next, _ := openMemory(config{memoryBackend: "local"}, cwd)
	if !strings.Contains(next.snapshot, "## Checks") {
		t.Fatalf("the next session starts with the summary:\n%s", next.snapshot)
	}
	if !strings.Contains(m.store.Dir, filepath.Join(".omp", "memory")) {
		t.Fatalf("memory lives under ~/.omp/memory: %s", m.store.Dir)
	}
}

type scriptedProvider struct {
	replies []string
	calls   int
}

func (p *scriptedProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return nil
}

func (p *scriptedProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.StopReason, llm.Usage, error) {
	text := p.replies[p.calls%len(p.replies)]
	p.calls++
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(text)}}, llm.StopReasonStop, llm.Usage{}, nil
}
