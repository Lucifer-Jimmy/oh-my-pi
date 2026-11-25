package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type fakeCtx struct{}

func (fakeCtx) CWD() string          { return "" }
func (fakeCtx) Workspaces() []string { return nil }
func (fakeCtx) Emit(agent.Event)     {}
func (fakeCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, agent.ErrNoUI
}
func (fakeCtx) RecentMessages() []llm.Message { return nil }

func writeSkill(t *testing.T, root, name, front, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(front+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func homeDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	return filepath.Join(home, filepath.FromSlash(userDir))
}

func TestDiscoverReadsFrontmatterAndBody(t *testing.T) {
	user := homeDir(t)
	writeSkill(t, user, "release", "---\nname: release\ndescription: cut a release\n---\n",
		"# Release\n1. bump\n2. tag\n")
	got := Discover(t.TempDir())
	if len(got) != 1 {
		t.Fatalf("skills = %+v", got)
	}
	s := got[0]
	if s.Name != "release" || s.Description != "cut a release" || !strings.Contains(s.Body, "2. tag") {
		t.Errorf("parsed wrongly: %+v", s)
	}
	if strings.Contains(s.Body, "---") {
		t.Errorf("frontmatter leaked into the body: %q", s.Body)
	}
}

func TestProjectSkillWinsOverUserSkill(t *testing.T) {
	user := homeDir(t)
	writeSkill(t, user, "deploy", "---\nname: deploy\ndescription: user version\n---\n", "user body\n")
	cwd := t.TempDir()
	writeSkill(t, filepath.Join(cwd, filepath.FromSlash(projectDir)), "deploy",
		"---\nname: deploy\ndescription: project version\n---\n", "project body\n")
	got := Discover(cwd)
	if len(got) != 1 || got[0].Description != "project version" || !strings.Contains(got[0].Body, "project body") {
		t.Fatalf("the project skill must win: %+v", got)
	}
}

func TestBrokenSkillsAreSkippedNotFatal(t *testing.T) {
	user := homeDir(t)
	writeSkill(t, user, "no-desc", "---\nname: no-desc\n---\n", "body\n")
	writeSkill(t, user, "no-body", "---\nname: no-body\ndescription: d\n---\n", "   \n")
	if err := os.MkdirAll(filepath.Join(user, "no-file"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, user, "ok", "---\nname: ok\ndescription: fine\n---\n", "body\n")
	got := Discover(t.TempDir())
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("only the valid skill should load: %+v", got)
	}
}

func TestCRLFFrontmatterIsHandled(t *testing.T) {
	user := homeDir(t)
	dir := filepath.Join(user, "win")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\r\nname: win\r\ndescription: windows file\r\n---\r\nstep one\r\n"
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Discover(t.TempDir())
	if len(got) != 1 || got[0].Description != "windows file" {
		t.Fatalf("CRLF must not break parsing: %+v", got)
	}
	if strings.Contains(got[0].Body, "\r") {
		t.Errorf("body should be normalised to LF: %q", got[0].Body)
	}
}

func run(t *testing.T, tool *Tool, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	res := tool.Execute(context.Background(), fakeCtx{}, raw)
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestToolReturnsTheBodyOnDemand(t *testing.T) {
	tool := &Tool{Skills: []Skill{{Name: "release", Description: "d", Body: "step one\nstep two\n"}}}
	got, isErr := run(t, tool, map[string]any{"name": "RELEASE"})
	if isErr || !strings.Contains(got, "step two") {
		t.Fatalf("got isErr=%v %q", isErr, got)
	}
}

func TestToolListsNamesWhenUnknown(t *testing.T) {
	tool := &Tool{Skills: []Skill{{Name: "a", Description: "d"}, {Name: "b", Description: "d"}}}
	got, isErr := run(t, tool, map[string]any{"name": "c"})
	if !isErr || !strings.Contains(got, "a, b") {
		t.Errorf("got isErr=%v %q", isErr, got)
	}
}

func TestSummariesCarryNoBody(t *testing.T) {
	sums := Summaries([]Skill{{Name: "a", Description: "d", Body: strings.Repeat("x", 1000)}})
	if len(sums) != 1 || sums[0].Name != "a" || sums[0].Description != "d" {
		t.Fatalf("summaries = %+v", sums)
	}
}
