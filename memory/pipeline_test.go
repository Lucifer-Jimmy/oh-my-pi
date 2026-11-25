package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

var testNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)

type model struct {
	mu       sync.Mutex
	extracts []string
	merges   []string
	reply    func(phase, input string) (string, error)
}

func (m *model) complete(ctx context.Context, req llm.CompletionRequest) (string, error) {
	input := req.Messages[0].Content[0].Text
	phase := "extract"
	if req.System[0] == consolidateSystem {
		phase = "consolidate"
	}
	m.mu.Lock()
	if phase == "extract" {
		m.extracts = append(m.extracts, input)
	} else {
		m.merges = append(m.merges, input)
	}
	m.mu.Unlock()
	if m.reply != nil {
		return m.reply(phase, input)
	}
	if phase == "consolidate" {
		return "## Project memory\n- merged", nil
	}
	return `{"summary":"worked","memory":"- remembered"}`, nil
}

func (m *model) calls() (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.extracts), len(m.merges)
}

func writeSession(t *testing.T, dir, id string, modified time.Time, texts ...string) {
	t.Helper()
	s, err := session.Create(dir, id, dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range texts {
		if i%2 == 0 {
			s.Append(llm.UserText(text))
		} else {
			s.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(text)}})
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Path(), modified, modified); err != nil {
		t.Fatal(err)
	}
}

func newPipeline(t *testing.T, m *model) (Pipeline, string) {
	t.Helper()
	sessions := t.TempDir()
	return Pipeline{
		Store:       Store{Dir: filepath.Join(t.TempDir(), "memory")},
		SessionsDir: sessions,
		Current:     "current",
		Complete:    m.complete,
		Now:         func() time.Time { return testNow },
	}, sessions
}

func ago(d time.Duration) time.Time { return testNow.Add(-d) }

func TestRunReadsIdleSessionsAndBuildsTheSummary(t *testing.T) {
	m := &model{reply: func(phase, input string) (string, error) {
		switch {
		case phase == "consolidate":
			return "```markdown\n## Conventions\n- run make check before landing\n```", nil
		case strings.Contains(input, "chit-chat"):
			return `{"summary": "", "memory": ""}`, nil
		}
		return "Here you go:\n```json\n{\"summary\": \"fixed the build\", \"memory\": \"- run make check before landing\"}\n```", nil
	}}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "useful", ago(13*time.Hour), "the build fails", "make check fixes it")
	writeSession(t, dir, "chatter", ago(20*time.Hour), "chit-chat", "hello")
	writeSession(t, dir, "current", ago(48*time.Hour), "the running session", "is never read")
	writeSession(t, dir, "recent", ago(time.Hour), "still in use", "wait")
	writeSession(t, dir, "ancient", ago(31*24*time.Hour), "too old", "skip")
	writeSession(t, dir, "empty", ago(48*time.Hour))

	st, err := p.Run(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if st.Read != 2 || st.Useful != 1 || st.Failed != 0 || !st.Summarized || st.Sessions != 1 {
		t.Fatalf("status = %+v", st)
	}
	if n, merges := m.calls(); n != 2 || merges != 1 {
		t.Fatalf("calls: %d extractions, %d merges", n, merges)
	}
	for _, in := range m.extracts {
		if !strings.Contains(in, "<transcript>") || strings.Contains(in, "never read") || strings.Contains(in, "too old") || strings.Contains(in, "still in use") {
			t.Fatalf("only idle, recent-enough sessions other than the current one are read:\n%s", in)
		}
	}
	if !strings.Contains(m.merges[0], "run make check") || strings.Contains(m.merges[0], "chit-chat") {
		t.Fatalf("only durable memory reaches the merge:\n%s", m.merges[0])
	}
	if got := p.Store.Summary(); got != "## Conventions\n- run make check before landing" {
		t.Fatalf("summary = %q", got)
	}
	if got, ok := p.Store.status(); !ok || got.FinishedAt != testNow.UnixMilli() {
		t.Fatalf("the status is saved for /memory: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(p.Store.Dir, lockFile)); !os.IsNotExist(err) {
		t.Fatal("the lock is released")
	}
}

func TestRunReadsASessionAgainOnlyWhenItChanged(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(13*time.Hour), "q", "a")
	p.Run(context.Background(), false)
	p.Run(context.Background(), false)
	if n, merges := m.calls(); n != 1 || merges != 1 {
		t.Fatalf("a second run with nothing new makes no calls: %d %d", n, merges)
	}
	path := filepath.Join(dir, "a"+session.FileSuffix)
	later := ago(12*time.Hour + time.Minute)
	os.Chtimes(path, later, later)
	m.reply = func(phase, input string) (string, error) {
		if phase == "consolidate" {
			return "- newer merge", nil
		}
		return `{"summary":"more","memory":"- something else"}`, nil
	}
	st, _ := p.Run(context.Background(), false)
	if n, merges := m.calls(); n != 2 || merges != 2 || !st.Summarized {
		t.Fatalf("a changed session is read again and the summary rebuilt: %d %d %+v", n, merges, st)
	}
}

func TestFailuresAreRetriedThenParkedUntilTheSessionChanges(t *testing.T) {
	m := &model{reply: func(phase, input string) (string, error) {
		if phase == "extract" {
			return "", errors.New("gateway down")
		}
		return "- merged", nil
	}}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(13*time.Hour), "q", "a")
	for range maxExtractFailures + 2 {
		st, _ := p.Run(context.Background(), false)
		if st.Read != 0 {
			t.Fatalf("a failed extraction is not counted as read: %+v", st)
		}
	}
	if n, _ := m.calls(); n != maxExtractFailures {
		t.Fatalf("a failing session is tried %d times, then left alone: %d", maxExtractFailures, n)
	}
	st, _ := p.Store.status()
	if !strings.Contains(st.Error, "gateway down") || st.Parked != 1 {
		t.Fatalf("a parked session stays visible with its last error: %+v", st)
	}
	later := ago(12*time.Hour + time.Minute)
	os.Chtimes(filepath.Join(dir, "a"+session.FileSuffix), later, later)
	p.Run(context.Background(), false)
	if n, _ := m.calls(); n != maxExtractFailures+1 {
		t.Fatal("a changed session gets a fresh try")
	}
}

func TestSyncReadsRecentSessionsAndRebuilds(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "recent", ago(time.Minute), "q", "a")
	p.Run(context.Background(), true)
	p.Run(context.Background(), true)
	if n, merges := m.calls(); n != 1 || merges != 2 {
		t.Fatalf("a forced run reads recent sessions and always rebuilds: %d %d", n, merges)
	}
}

func TestOneRunReadsAtMostTheCap(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	for i := range maxSessionsPerRun + 2 {
		writeSession(t, dir, fmt.Sprintf("s%02d", i), ago(13*time.Hour+time.Duration(i)*time.Minute), "q", "a")
	}
	st, _ := p.Run(context.Background(), false)
	if st.Read != maxSessionsPerRun || st.Pending != 2 {
		t.Fatalf("status = %+v", st)
	}
	st, _ = p.Run(context.Background(), false)
	if st.Read != 2 || st.Pending != 0 {
		t.Fatalf("the rest is read next time: %+v", st)
	}
}

func TestABusyLockSkipsTheRun(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(13*time.Hour), "q", "a")
	release, err := p.Store.lock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := p.Run(context.Background(), false); !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v", err)
	}
	if n, _ := m.calls(); n != 0 {
		t.Fatal("no model call while another process holds the lock")
	}
}

func TestASummaryWithNothingBehindItIsRemoved(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(13*time.Hour), "q", "a")
	p.Run(context.Background(), false)
	if p.Store.Summary() == "" {
		t.Fatal("setup: summary expected")
	}
	m.reply = func(phase, input string) (string, error) { return `{"summary":"","memory":""}`, nil }
	later := ago(12*time.Hour + time.Minute)
	os.Chtimes(filepath.Join(dir, "a"+session.FileSuffix), later, later)
	p.Run(context.Background(), false)
	if p.Store.Summary() != "" {
		t.Fatal("when no session holds anything durable, the summary goes")
	}
}

func TestSecretsNeverReachTheFiles(t *testing.T) {
	m := &model{reply: func(phase, input string) (string, error) {
		if phase == "consolidate" {
			return "- deploy key ghp_abcdefghijklmnopqrstuvwxyz", nil
		}
		return `{"summary":"used AKIAABCDEFGHIJKLMNOP","memory":"- token_aB3dEfGh1JkLmN9 opens the gateway"}`, nil
	}}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(13*time.Hour), "q", "a")
	p.Run(context.Background(), false)
	e, _ := p.Store.readExtract("a")
	b, _ := json.Marshal(e)
	for _, s := range []string{string(b), p.Store.Summary()} {
		if strings.Contains(s, "AKIA") || strings.Contains(s, "aB3dEfGh1JkLmN9") || strings.Contains(s, "ghp_") {
			t.Fatalf("a secret was stored: %s", s)
		}
	}
}

func TestTranscriptKeepsToolsAndBothEnds(t *testing.T) {
	msgs := []llm.Message{
		llm.UserText("fix the failing test"),
		{Role: llm.RoleAssistant, Content: []llm.ContentBlock{
			{Type: llm.BlockThinking, Thinking: "private reasoning"},
			{Type: llm.BlockToolUse, ID: "t1", Name: "bash", Input: json.RawMessage(`{"command":"go test ./..."}`)},
		}},
		{Role: llm.RoleUser, Content: []llm.ContentBlock{{Type: llm.BlockToolResult, ToolUseID: "t1", IsError: true, Content: []llm.ContentBlock{llm.TextBlock("FAIL calc_test.go")}}}},
	}
	got := transcript(msgs, 1000)
	for _, want := range []string{"[user] fix the failing test", "[tool call bash] {\"command\":\"go test ./...\"}", "[tool error] FAIL calc_test.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("transcript lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "private reasoning") {
		t.Error("thinking is not sent back")
	}
	var long []llm.Message
	for i := range 400 {
		long = append(long, llm.UserText(fmt.Sprintf("message %03d %s", i, strings.Repeat("x", 100))))
	}
	cut := transcript(long, 1000)
	if len(cut) > 1000*4 || !strings.Contains(cut, "message 000") || !strings.Contains(cut, "message 399") || !strings.Contains(cut, "truncated") {
		t.Fatalf("an over-long transcript keeps its head and tail within the budget (%d bytes)", len(cut))
	}
}

func TestParseJSONObjectToleratesFencesAndProse(t *testing.T) {
	var r struct{ Memory string }
	for _, in := range []string{`{"memory":"m"}`, "```json\n{\"memory\":\"m\"}\n```", "Sure:\n{\"memory\":\"m\"}\nDone."} {
		r.Memory = ""
		if err := parseJSONObject(in, &r); err != nil || r.Memory != "m" {
			t.Errorf("%q -> %q %v", in, r.Memory, err)
		}
	}
	if parseJSONObject("no json here", &r) == nil {
		t.Error("a reply without an object is an error")
	}
}

func TestControl(t *testing.T) {
	m := &model{}
	p, dir := newPipeline(t, m)
	writeSession(t, dir, "a", ago(time.Minute), "q", "a")
	c := &Control{Store: p.Store, Pipeline: &p, Snapshot: Guidance("", "- a lesson")}
	view, err := c.Command("")
	if err != nil || !strings.Contains(view, "No update has run yet") || !strings.Contains(view, "- a lesson") {
		t.Fatalf("view = %q %v", view, err)
	}
	if _, err := c.Command("sync"); err != nil {
		t.Fatal(err)
	}
	c.wg.Wait()
	view, _ = c.Command("view")
	if !strings.Contains(view, "1 sessions read") || !strings.Contains(view, "summary rebuilt from 1 sessions") {
		t.Fatalf("view after sync = %q", view)
	}
	if _, err := c.Command("clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.Store.Dir); !os.IsNotExist(err) {
		t.Fatal("clear deletes the memory")
	}
	if _, err := c.Command("forget everything"); err == nil {
		t.Fatal("an unknown subcommand is an error")
	}
	none := &Control{Store: p.Store}
	if _, err := none.Command("sync"); err == nil {
		t.Fatal("without a pipeline there is nothing to sync")
	}
	if none.Start(false) {
		t.Fatal("start without a pipeline does nothing")
	}
}
