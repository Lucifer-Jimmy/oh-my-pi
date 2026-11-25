package subagent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func gitAvailable(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repoWithFile(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", ".")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	return dir
}

type writeStub struct{}

func (writeStub) Name() string            { return "write" }
func (writeStub) Description() string     { return "writes a file" }
func (writeStub) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (writeStub) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return agent.ToolResult{IsError: true}
	}
	full := a.Path
	if !filepath.IsAbs(full) {
		full = filepath.Join(tc.CWD(), a.Path)
	}
	if err := os.WriteFile(full, []byte(a.Body), 0o644); err != nil {
		return agent.ToolResult{
			Content: []llm.ContentBlock{llm.TextBlock("write: " + err.Error())},
			IsError: true,
		}
	}
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("wrote " + a.Path)}}
}
