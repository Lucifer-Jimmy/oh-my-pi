package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Create(dir, "s1", "/work", "gw/m1")
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func userMessage(text string) llm.Message { return llm.UserText(text) }

func assistantToolCall(id, name string) llm.Message {
	return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
		llm.TextBlock("calling " + name),
		{Type: llm.BlockToolUse, ID: id, Name: name, Input: json.RawMessage(`{"path":"a.go"}`)},
	}}
}

func toolResult(id, text string) llm.Message {
	return llm.Message{Role: llm.RoleUser, Content: []llm.ContentBlock{llm.ToolResultText(id, text, false)}}
}

func TestRoundTripKeepsEveryMessageByteIdentical(t *testing.T) {
	s, dir := open(t)
	msgs := []llm.Message{
		userMessage("fix the build"),
		assistantToolCall("t1", "read"),
		toolResult("t1", "package main\n\nfunc main() {}\n"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("done")}},
	}
	for _, m := range msgs {
		s.Append(m)
	}
	s.AppendCompaction(Compaction{Trigger: TriggerAuto, Elided: 3, TokensBefore: 900, TokensAfter: 300})
	recorded := s.Messages()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, r, err := Load(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()

	if r.Repaired {
		t.Errorf("a clean file needs no repair: %s", r)
	}
	if r.Messages != len(msgs) || r.Compactions != 1 {
		t.Errorf("report counts are wrong: %+v", r)
	}
	got := loaded.Messages()
	if len(got) != len(recorded) {
		t.Fatalf("messages: got %d, want %d", len(got), len(recorded))
	}
	for i := range recorded {
		want, err := json.Marshal(recorded[i])
		if err != nil {
			t.Fatal(err)
		}
		have, err := json.Marshal(got[i])
		if err != nil {
			t.Fatal(err)
		}
		if string(have) != string(want) {
			t.Errorf("message %d changed:\n got %s\nwant %s", i, have, want)
		}
	}
	cp, ok := loaded.LastCompaction()
	if !ok || cp.Elided != 3 || cp.TokensAfter != 300 || cp.Trigger != TriggerAuto {
		t.Errorf("compaction record is wrong: %+v", cp)
	}
}

func TestASessionWrittenWithCostStillLoads(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("hi"))
	s.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("hello")},
		Usage: &llm.Usage{InputTokens: 120, OutputTokens: 7}})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1"+FileSuffix)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	old := `"cache_write_tokens":0}`
	if strings.Count(string(b), old) != 1 {
		t.Fatalf("the fixture must carry exactly one usage object:\n%s", b)
	}
	b = []byte(strings.Replace(string(b), old, `"cache_write_tokens":0,"cost_usd":0.0123}`, 1))
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, r, err := Load(path)
	if err != nil {
		t.Fatalf("a file from a build that still priced usage must load: %v", err)
	}
	defer loaded.Close()
	if r.Repaired || r.Messages != 2 {
		t.Fatalf("the old field is ignored, not repaired: %+v", r)
	}
	if u := loaded.Messages()[1].Usage; u == nil || u.InputTokens != 120 || u.OutputTokens != 7 {
		t.Errorf("the token counts survive: %+v", u)
	}
}

func TestFileStartsWithHeaderAndEndsWithNewline(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("hi"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want header + 1 entry, got %d lines:\n%s", len(lines), b)
	}
	var h header
	if err := json.Unmarshal([]byte(lines[0]), &h); err != nil {
		t.Fatal(err)
	}
	if h.Type != "header" || h.Version != FileVersion || h.ID != "s1" || h.CWD != "/work" || h.Model != "gw/m1" {
		t.Errorf("header = %+v", h)
	}
	if !strings.HasSuffix(string(b), "\n") {
		t.Error("the file must end with a newline so a crash cannot glue two entries")
	}
	var e Entry
	if err := json.Unmarshal([]byte(lines[1]), &e); err != nil {
		t.Fatal(err)
	}
	if e.Type != EntryMessage || e.Message == nil || e.Message.Text() != "hi" {
		t.Errorf("entry = %+v", e)
	}
	if e.ID == "" || e.ParentID != "" {
		t.Errorf("the first entry has an id and no parent: %+v", e)
	}
}

func TestParentsFormALine(t *testing.T) {
	s, dir := open(t)
	for _, text := range []string{"one", "two", "three"} {
		s.Append(userMessage(text))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	var prev string
	ids := map[string]bool{}
	for i, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")[1:] {
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		if e.ParentID != prev {
			t.Errorf("entry %d parent = %q, want %q", i, e.ParentID, prev)
		}
		if ids[e.ID] {
			t.Errorf("duplicate entry id %q", e.ID)
		}
		ids[e.ID] = true
		prev = e.ID
	}
}

func TestTruncatedLastLineIsDropped(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("kept"))
	s.Append(userMessage("lost"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1"+FileSuffix)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cut := strings.LastIndex(strings.TrimSuffix(string(b), "\n"), "\n")
	if err := os.WriteFile(path, append(b[:cut+1], []byte(`{"type":"message","id":"s1-2","mess`)...), 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, r, err := Load(path)
	if err != nil {
		t.Fatalf("a half-written last line must not fail the load: %v", err)
	}
	defer loaded.Close()
	if !r.Repaired || r.Dropped != 1 {
		t.Errorf("the torn entry must be reported: %+v", r)
	}
	got := loaded.Messages()
	if len(got) != 1 || got[0].Text() != "kept" {
		t.Errorf("the complete entries survive: %+v", got)
	}
}

func TestMiddleCorruptionFails(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("one"))
	s.Append(userMessage("two"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1"+FileSuffix)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	lines[1] = "not json at all"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("damage in the middle is not a torn write; it must be reported")
	}
}

func TestNewSessionAppendsAfterLoad(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("before"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s1"+FileSuffix)
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Append(userMessage("after"))
	if err := loaded.Close(); err != nil {
		t.Fatal(err)
	}
	again, r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if r.Messages != 2 {
		t.Fatalf("want 2 messages, got %+v", r)
	}
	got := again.Messages()
	if got[0].Text() != "before" || got[1].Text() != "after" {
		t.Errorf("append after load must continue the file: %+v", got)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\n"); n != 3 {
		t.Errorf("exactly one new line was added, got %d lines", n)
	}
}

func TestEmptyAndHeaderOnlyFiles(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty"+FileSuffix)
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(empty); err == nil {
		t.Error("an empty file is not a session")
	}

	head := filepath.Join(dir, "head"+FileSuffix)
	b, _ := json.Marshal(header{Type: "header", Version: FileVersion, ID: "h1"})
	if err := os.WriteFile(head, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	s, r, err := Load(head)
	if err != nil {
		t.Fatalf("a header-only session is valid and empty: %v", err)
	}
	defer s.Close()
	if r.Messages != 0 || r.Repaired {
		t.Errorf("report = %+v", r)
	}
}

func TestNewerVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "future"+FileSuffix)
	b, _ := json.Marshal(header{Type: "header", Version: FileVersion + 1, ID: "f1"})
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "newer omp") {
		t.Fatalf("a future version must be refused with a clear reason, got %v", err)
	}
}

func TestNotAHeaderIsRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad"+FileSuffix)
	if err := os.WriteFile(path, []byte(`{"type":"message","message":{}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "header") {
		t.Fatalf("the first line must be a header, got %v", err)
	}
}

func TestDanglingToolCallGetsAnOutcomeUnknownResult(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("run the tests"))
	s.Append(assistantToolCall("t1", "bash"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	loaded, r, err := Load(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !r.Repaired || r.Synthetic != 1 {
		t.Fatalf("a tool call without a result is repaired: %+v", r)
	}
	got := loaded.Messages()
	last := got[len(got)-1]
	if last.Role != llm.RoleUser || len(last.Content) != 1 || last.Content[0].Type != llm.BlockToolResult {
		t.Fatalf("want one synthetic tool result, got %+v", last)
	}
	b := last.Content[0]
	if b.ToolUseID != "t1" || !b.IsError || !strings.Contains(b.Content[0].Text, "outcome unknown") {
		t.Errorf("synthetic result = %+v", b)
	}
}

func TestToolResultsAreRepairedPerCall(t *testing.T) {
	s, dir := open(t)
	s.Append(assistantToolCall("t1", "read"))
	s.Append(toolResult("t1", "ok"))
	s.Append(assistantToolCall("t2", "grep"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, r, err := Load(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if r.Synthetic != 1 {
		t.Fatalf("only the unanswered call is repaired: %+v", r)
	}
	got := loaded.Messages()
	var ids []string
	for _, m := range got[len(got)-1].Content {
		ids = append(ids, m.ToolUseID)
	}
	if len(ids) != 1 || ids[0] != "t2" {
		t.Errorf("want a synthetic result for t2 only, got %v", ids)
	}
}

func TestOrphanToolResultIsDropped(t *testing.T) {
	s, dir := open(t)
	s.Append(userMessage("hello"))
	s.Append(toolResult("ghost", "stale"))
	s.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("done")}})
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, r, err := Load(filepath.Join(dir, "s1"+FileSuffix))
	if err != nil {
		t.Fatal(err)
	}
	defer loaded.Close()
	if !r.Repaired || r.Dropped != 1 {
		t.Fatalf("a result with no call is dropped: %+v", r)
	}
	for _, m := range loaded.Messages() {
		for _, b := range m.Content {
			if b.Type == llm.BlockToolResult {
				t.Errorf("orphan survived: %+v", b)
			}
		}
	}
}

func TestListIsNewestFirstAndSkipsDamage(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"a", "b"} {
		s, err := Create(dir, id, "/work", "m")
		if err != nil {
			t.Fatal(err)
		}
		s.Append(userMessage("task " + id))
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "broken"+FileSuffix), []byte("junk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want the two readable sessions, got %+v", list)
	}
	for _, info := range list {
		if info.Messages != 1 || !strings.HasPrefix(info.Title, "task ") {
			t.Errorf("info = %+v", info)
		}
	}
	latest, ok, err := Latest(dir)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if filepath.Base(latest) != "b"+FileSuffix && filepath.Base(latest) != "a"+FileSuffix {
		t.Errorf("latest = %s", latest)
	}
}

func TestListOfMissingDirectoryIsEmpty(t *testing.T) {
	list, err := List(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(list) != 0 {
		t.Fatalf("a missing directory is not an error: %v %v", list, err)
	}
}

func TestWriteFailureIsLatchedAndReported(t *testing.T) {
	s, dir := open(t)
	path := filepath.Join(dir, "s1"+FileSuffix)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	loaded, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.file.Close()

	loaded.Append(userMessage("lost"))
	if loaded.Err() == nil {
		t.Fatal("a closed file must surface as an error")
	}
	if got := loaded.Messages(); len(got) != 1 || got[0].Text() != "lost" {
		t.Error("the turn still proceeds in memory")
	}
	loaded.Append(userMessage("also lost"))
	if err := loaded.Close(); err == nil {
		t.Error("Close reports the latched failure")
	}
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	first, err := Create(dir, "dup", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if _, err := Create(dir, "dup", "/work", "m"); err == nil {
		t.Fatal("a session id is never reused")
	}
}

func TestReportString(t *testing.T) {
	if got := (Report{}).String(); got != "" {
		t.Errorf("a clean load says nothing, got %q", got)
	}
	got := Report{Repaired: true, Dropped: 2, Synthetic: 1}.String()
	for _, want := range []string{"repaired", "2 damaged", "1 tool call"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q must mention %q", got, want)
		}
	}
}

func TestCreateInMissingDirectoryMakesIt(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	s, err := Create(dir, "x", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(filepath.Join(dir, "x"+FileSuffix)); err != nil {
		t.Error(err)
	}
}
