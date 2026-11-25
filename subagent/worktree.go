package subagent

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const noRepoNote = "the parent session is not inside a git repository with a commit, so nothing can be isolated"

type worktree struct {
	root string
	dir  string
	name string
	base string
}

func gitRun(dir string, stdin []byte, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), fmt.Errorf("git %s: %s", args[0], collapse(msg))
	}
	return out.String(), nil
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func worktreeRoot(root string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New(noRepoNote)
	}
	if _, err := gitRun(root, nil, "rev-parse", "--verify", "HEAD"); err != nil {
		return "", errors.New(noRepoNote)
	}
	return root, nil
}

func createWorktree(root string) (*worktree, error) {
	root, err := worktreeRoot(root)
	if err != nil {
		return nil, err
	}
	base, err := gitRun(root, nil, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	name := worktreeName()
	dir := filepath.Join(os.TempDir(), "omp-worktrees", name)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	if _, err := gitRun(root, nil, "worktree", "add", "--detach", dir, "HEAD"); err != nil {
		discardWorktree(root, dir)
		return nil, err
	}
	return &worktree{root: root, dir: dir, name: name, base: strings.TrimSpace(base)}, nil
}

func worktreeName() string {
	var suffix [6]byte
	rand.Read(suffix[:])
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(suffix[:])
}

func (w *worktree) commit(msg string) (string, bool, error) {
	if _, err := gitRun(w.dir, nil, "add", "-A"); err != nil {
		return "", false, err
	}
	status, err := gitRun(w.dir, nil, "status", "--porcelain")
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(status) == "" {
		return "", false, nil
	}
	_, err = gitRun(w.dir, nil,
		"-c", "user.name=omp", "-c", "user.email=omp@localhost", "-c", "commit.gpgsign=false",
		"commit", "-q", "--no-verify", "-m", msg)
	if err != nil {
		return "", false, err
	}
	sha, err := gitRun(w.dir, nil, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(sha), true, nil
}

func (w *worktree) patch(sha string) ([]byte, error) {
	out, err := gitRun(w.root, nil, "diff", "--binary", "--full-index", w.base, sha)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

func (w *worktree) apply(p []byte) error {
	_, err := gitRun(w.root, p, "apply", "--whitespace=nowarn", "-")
	return err
}

func (w *worktree) remove() error {
	err := discardWorktree(w.root, w.dir)
	gitRun(w.root, nil, "worktree", "prune")
	return err
}

func discardWorktree(root, dir string) error {
	_, err := gitRun(root, nil, "worktree", "remove", "--force", dir)
	if err != nil {
		os.RemoveAll(dir)
	}
	return err
}

func savePatch(name string, p []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "omp-patches")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".patch")
	if err := os.WriteFile(path, p, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
