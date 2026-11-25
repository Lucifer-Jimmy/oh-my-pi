package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func TestTitleIsAppendedAndLastOneWins(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("first prompt decides the default title"))
	s.SetTitle("release work")
	s.SetTitle("release work, second take")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	infos, err := List(dir)
	if err != nil || len(infos) != 1 {
		t.Fatalf("list: %v %v", infos, err)
	}
	if infos[0].Title != "release work, second take" {
		t.Fatalf("title = %q", infos[0].Title)
	}

	reopened, report, err := Load(infos[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if report.Repaired || report.Dropped != 0 {
		t.Fatalf("a title entry must not look like damage: %+v", report)
	}
	if got := reopened.Messages(); len(got) != 1 {
		t.Fatalf("title entries must not become messages, got %d", len(got))
	}

	body, err := os.ReadFile(infos[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"type":"title"`) {
		t.Fatalf("the title entry must be in the file:\n%s", body)
	}
}

func TestTitleFallsBackToTheFirstUserLine(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("fix the flaky test\nand then commit"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	infos, _ := List(dir)
	if len(infos) != 1 || infos[0].Title != "fix the flaky test" {
		t.Fatalf("fallback title = %+v", infos)
	}
}

func TestSetTitleIgnoresBlank(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.SetTitle("kept")
	s.SetTitle("   ")
	path := s.Path()
	s.Close()
	infos, _ := List(dir)
	if len(infos) != 1 || infos[0].Title != "kept" {
		t.Fatalf("a blank title must not replace the previous one: %+v (path %s)", infos, path)
	}
}

func TestRenameAndDeleteByPath(t *testing.T) {
	dir := t.TempDir()
	keep, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	keep.Append(llm.UserText("keep me"))
	keepPath := keep.Path()
	keep.Close()

	gone, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	gone.Append(llm.UserText("delete me"))
	gonePath := gone.Path()
	gone.Close()

	if err := renameOnDisk(keepPath, "renamed on disk"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	infos, _ := List(dir)
	for _, info := range infos {
		if info.Path == keepPath && info.Title != "renamed on disk" {
			t.Fatalf("title on disk = %q", info.Title)
		}
	}

	if err := DeleteByPath(gonePath); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := os.Stat(gonePath); !os.IsNotExist(err) {
		t.Fatalf("the session file must be gone, stat err = %v", err)
	}
	if _, err := os.Stat(keepPath); err != nil {
		t.Fatalf("an unrelated session must survive: %v", err)
	}
}

func TestDeleteRefusesPathsOutsideSessionFiles(t *testing.T) {
	dir := t.TempDir()
	other := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(other, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := DeleteByPath(other); err == nil {
		t.Fatalf("only .jsonl session files may be deleted")
	}
	if err := renameOnDisk(other, "x"); err == nil {
		t.Fatalf("rename must only touch session files")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("the non-session file must survive: %v", err)
	}
}

func TestMarkdownExportRendersRoles(t *testing.T) {
	md := Markdown([]llm.Message{
		{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.TextBlock("what broke?")}},
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("the build did")}},
	})
	for _, want := range []string{"**user**", "what broke?", "**assistant**", "the build did"} {
		if !strings.Contains(md, want) {
			t.Errorf("export missing %q:\n%s", want, md)
		}
	}
	if got := fmt.Sprint(Markdown(nil)); !strings.Contains(got, "") {
		t.Errorf("an empty session exports without error")
	}
}

func renameOnDisk(path, title string) error {
	s, _, err := Load(path)
	if err != nil {
		return err
	}
	defer s.Close()
	s.SetTitle(title)
	return s.Err()
}

func TestGoalIsAppendedAndTheLastOneComesBack(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("make the tests pass"))
	s.SetGoal(json.RawMessage(`{"id":"g1","status":"active","tokens_used":10}`))
	s.SetGoal(json.RawMessage(`{"id":"g1","status":"paused","tokens_used":42}`))
	s.SetGoal(json.RawMessage(`not json`))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, report, err := Load(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if report.Repaired || report.Dropped != 0 {
		t.Fatalf("a goal entry must not look like damage: %+v", report)
	}
	if got := reopened.Messages(); len(got) != 1 {
		t.Fatalf("goal entries must not become messages, got %d", len(got))
	}
	if got := string(reopened.Goal()); got != `{"id":"g1","status":"paused","tokens_used":42}` {
		t.Fatalf("the last goal entry wins, got %s", got)
	}
}

func TestReadReplaysWithoutOpeningForAppend(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(dir, NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("what changed"))
	s.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("nothing")}})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	msgs, err := Read(s.Path())
	if err != nil || len(msgs) != 2 {
		t.Fatalf("read = %d messages, %v", len(msgs), err)
	}
	after, _ := os.ReadFile(s.Path())
	if string(before) != string(after) {
		t.Fatal("reading must not touch the file")
	}
	if _, err := Read(filepath.Join(dir, "missing.jsonl")); err == nil {
		t.Fatal("a missing file is an error")
	}
}
