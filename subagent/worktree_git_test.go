package subagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeCommitMergeAndRemove(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt.dir); err != nil {
		t.Fatalf("the worktree directory must exist: %v", err)
	}
	if strings.HasPrefix(filepath.Clean(wt.dir), filepath.Clean(root)+string(filepath.Separator)) {
		t.Fatalf("the worktree must live outside the repository, got %q", wt.dir)
	}
	if err := os.WriteFile(filepath.Join(wt.dir, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, changed, err := wt.commit("work")
	if err != nil || !changed || sha == "" {
		t.Fatalf("commit = %q %v %v", sha, changed, err)
	}
	p, err := wt.patch(sha)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p), "b.txt") || !strings.Contains(string(p), "+two") {
		t.Fatalf("the patch must carry both the edit and the new file:\n%s", p)
	}
	if err := wt.apply(p); err != nil {
		t.Fatalf("a clean patch must apply: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || strings.TrimSpace(string(got)) != "two" {
		t.Fatalf("the parent working tree must have the edited file, got %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, "b.txt")); err != nil {
		t.Fatalf("the parent working tree must have the new file: %v", err)
	}
	if err := wt.remove(); err != nil {
		t.Fatalf("removing the worktree must succeed: %v", err)
	}
	if _, err := os.Stat(wt.dir); !os.IsNotExist(err) {
		t.Fatalf("the worktree directory must be gone, stat err = %v", err)
	}
	if list := gitIn(t, root, "worktree", "list"); strings.Contains(list, wt.dir) {
		t.Fatalf("git must forget the worktree:\n%s", list)
	}
}

func TestWorktreeWithNoChangesCommitsNothing(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.remove()
	sha, changed, err := wt.commit("work")
	if err != nil {
		t.Fatal(err)
	}
	if changed || sha != "" {
		t.Fatalf("an untouched worktree must not commit, got %q %v", sha, changed)
	}
}

func TestWorktreeConflictDoesNotTouchTheParentAndKeepsThePatch(t *testing.T) {
	gitAvailable(t)
	root := repoWithFile(t, "a.txt", "one\n")
	wt, err := createWorktree(root)
	if err != nil {
		t.Fatal(err)
	}
	defer wt.remove()
	if err := os.WriteFile(filepath.Join(wt.dir, "a.txt"), []byte("child\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, changed, err := wt.commit("work")
	if err != nil || !changed {
		t.Fatalf("commit = %q %v %v", sha, changed, err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("parent\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := wt.patch(sha)
	if err != nil {
		t.Fatal(err)
	}
	if err := wt.apply(p); err == nil {
		t.Fatal("a patch onto a diverged file must not apply silently")
	}
	got, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "parent" {
		t.Fatalf("a failed apply must leave the file untouched, got %q", got)
	}
	path, err := savePatch(wt.name, p)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(p) {
		t.Fatalf("the saved patch must match, err = %v", err)
	}
}

func TestCreateWorktreeNeedsACommittedRepository(t *testing.T) {
	gitAvailable(t)
	if _, err := createWorktree(t.TempDir()); err == nil {
		t.Fatal("a directory without git history must not yield a worktree")
	}
	if _, err := createWorktree(""); err == nil || !strings.Contains(err.Error(), "git repository") {
		t.Fatalf("an empty root must say why, got %v", err)
	}
}
