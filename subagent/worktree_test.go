package subagent

import (
	"context"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func streamTurn(evs ...llm.StreamEvent) []llm.StreamEvent {
	out := append([]llm.StreamEvent{{Type: llm.SEMessageStart}}, evs...)
	return append(out, llm.StreamEvent{Type: llm.SEMessageStop})
}

func textTurn(text string) []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
		{Type: llm.SETextDelta, Text: text},
		{Type: llm.SEBlockStop},
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
	}
}

func toolTurn(id, name string, input map[string]any) []llm.StreamEvent {
	raw, _ := json.Marshal(input)
	return []llm.StreamEvent{
		{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockToolUse, ID: id, Name: name, Input: raw}},
		{Type: llm.SEBlockStop},
		{Type: llm.SEMessageDelta, StopReason: llm.StopReasonToolUse},
	}
}

func scriptedModel(t *testing.T, script func(n int, req llm.CompletionRequest) []llm.StreamEvent) func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	var mu sync.Mutex
	turns := 0
	t.Helper()
	return func(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
		return func(yield func(llm.StreamEvent, error) bool) {
			mu.Lock()
			turns++
			n := turns
			mu.Unlock()
			for _, ev := range streamTurn(script(n, req)...) {
				if !yield(ev, nil) {
					return
				}
			}
		}
	}
}

func worktreeList(t *testing.T, root string) string {
	t.Helper()
	return gitIn(t, root, "worktree", "list", "--porcelain")
}

func isolatedTool(t *testing.T, root string, model func(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error]) *Task {
	t.Helper()
	reg := agent.NewRegistry()
	reg.Register(writeStub{})
	return NewTaskTool(Config{
		Registry: reg,
		Deps:     agent.RunDeps{CallModel: model},
		Options: func(dir string) agent.Options {
			return agent.Options{CWD: dir}
		},
		CWD:      root,
		RepoRoot: root,
	})
}

func TestIsolatedTaskMergesItsEditsIntoTheParentTree(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	before := worktreeList(t, root)
	model := scriptedModel(t, func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		switch n {
		case 1:
			return toolTurn("t1", "write", map[string]any{"path": "made.txt", "body": "hello\n"})
		case 2:
			return textTurn("done")
		}
		t.Fatalf("unexpected turn %d", n)
		return nil
	})
	res, err := isolatedTool(t, root, model).Isolated(context.Background(), "create made.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != "" || res.Reason != agent.ReasonCompleted {
		t.Fatalf("the isolated run must complete: %+v", res)
	}
	body, err := os.ReadFile(filepath.Join(root, "made.txt"))
	if err != nil || strings.TrimSpace(string(body)) != "hello" {
		t.Fatalf("the subagent's file must reach the parent tree, got %q %v", body, err)
	}
	if !strings.Contains(res.Note, "applied") {
		t.Fatalf("the shell must be told the changes landed: %q", res.Note)
	}
	if after := worktreeList(t, root); after != before {
		t.Fatalf("the worktree must be cleaned up\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestIsolatedTaskIsRootedAtTheWorktree(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	var childDir string
	model := scriptedModel(t, func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		switch n {
		case 1:
			return toolTurn("t1", "write", map[string]any{"path": "note.txt", "body": "x\n"})
		}
		return textTurn("done")
	})
	reg := agent.NewRegistry()
	reg.Register(writeStub{})
	tool := NewTaskTool(Config{
		Registry: reg,
		Deps:     agent.RunDeps{CallModel: model},
		Options: func(dir string) agent.Options {
			childDir = dir
			if _, err := os.Stat(dir); err != nil {
				t.Errorf("the options factory must be handed a live directory: %v", err)
			}
			return agent.Options{CWD: dir, Workspaces: []string{dir}}
		},
		CWD:      root,
		RepoRoot: root,
	})
	res, err := tool.Isolated(context.Background(), "write a note", nil)
	if err != nil {
		t.Fatal(err)
	}
	if childDir == "" || childDir == root {
		t.Fatalf("the child must be rooted at the worktree, got %q", childDir)
	}
	if !strings.HasPrefix(childDir, filepath.Join(os.TempDir(), "omp-worktrees")) {
		t.Fatalf("the child cwd %q must be the temporary worktree", childDir)
	}
	if _, err := os.Stat(filepath.Join(root, "note.txt")); err != nil {
		t.Fatalf("the merged file must exist in the parent: %v", err)
	}
	if res.Err != "" {
		t.Fatalf("unexpected error %q", res.Err)
	}
}

func TestIsolatedTaskWithoutChangesSaysSo(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	model := scriptedModel(t, func(int, llm.CompletionRequest) []llm.StreamEvent {
		return textTurn("I only looked around")
	})
	res, err := isolatedTool(t, root, model).Isolated(context.Background(), "investigate", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Err != "" || res.Reason != agent.ReasonCompleted {
		t.Fatalf("a read-only isolation run is still a success: %+v", res)
	}
	if !strings.Contains(res.Note, "nothing changed") {
		t.Fatalf("note = %q", res.Note)
	}
}

func TestIsolatedTaskOnConflictKeepsTheParentFilesAndSavesThePatch(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	model := scriptedModel(t, func(n int, req llm.CompletionRequest) []llm.StreamEvent {
		switch n {
		case 1:
			if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("parent\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return toolTurn("t1", "write", map[string]any{"path": "a.txt", "body": "child\n"})
		}
		return textTurn("done")
	})
	res, err := isolatedTool(t, root, model).Isolated(context.Background(), "rewrite a.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Note, "do not apply") || !strings.Contains(res.Note, ".patch") {
		t.Fatalf("the shell must learn why the merge failed and where the patch went: %q", res.Note)
	}
	body, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(body)) != "parent" {
		t.Fatalf("a failed merge must not touch the parent file, got %q", body)
	}
	if !strings.Contains(res.Err, "merge conflict") {
		t.Fatalf("the run must be reported as failed, got %q", res.Err)
	}
}

func TestIsolatedTaskWithoutGitFallsBackToAnError(t *testing.T) {
	gitAvailable(t)
	res, err := isolatedTool(t, t.TempDir(), nil).Isolated(context.Background(), "anything", nil)
	if err == nil {
		t.Fatalf("no repository means no isolation, got %+v", res)
	}
	if !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("the error must explain itself: %v", err)
	}
}

func TestTaskRejectsIsolationWhenUnavailable(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	got, isErr := runTask(t, &Task{}, c, map[string]any{"prompt": "x", "isolated": true})
	if !isErr || !strings.Contains(got, "without isolation") {
		t.Fatalf("got isErr=%v %q", isErr, got)
	}
}

func TestTaskIsolatedArgRoutesToTheIsolatedRunner(t *testing.T) {
	c := &fakeCtx{cwd: t.TempDir()}
	called := false
	tool := &Task{
		Run: func(context.Context, string, func(agent.Event)) (Result, error) {
			t.Fatal("the plain runner must not be used for an isolated task")
			return Result{}, nil
		},
		Isolated: func(context.Context, string, func(agent.Event)) (Result, error) {
			called = true
			return Result{Reason: agent.ReasonCompleted, Text: "ok", Note: "merged"}, nil
		},
	}
	got, isErr := runTask(t, tool, c, map[string]any{"prompt": "x", "isolated": true})
	if isErr || !called {
		t.Fatalf("got isErr=%v called=%v %q", isErr, called, got)
	}
	if !strings.Contains(got, "merged") {
		t.Fatalf("the note must reach the model: %q", got)
	}
}
