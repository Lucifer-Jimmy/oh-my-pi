package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func TestDefaultsUnchanged(t *testing.T) {
	got := []any{maxLessons, maxLessonChars, maxContextChars, lockStale, injectionTokens, maxSessionAge, minSessionIdle,
		maxSessionsPerRun, maxExtractFailures, extractInputTokens, extractOutputTokens, consolidateInputTokens,
		consolidateOutputTokens, maxExtractsForSummary}
	want := []any{100, 2000, 400, 15 * time.Minute, 5000, 30 * 24 * time.Hour, 12 * time.Hour, 16, 3, 4000, 4096, 32000, 8192, 200}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("defaults changed:\n got %v\nwant %v", got, want)
	}
}

func readLessons(t *testing.T, s Store) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.Dir, LessonsFile))
	if err != nil {
		t.Fatal(err)
	}
	return splitLines(string(b))
}

func TestLearnKeepsTheNewestFirstAndMovesARepeatToTheTop(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, l := range []string{"one", "two", "three"} {
		if ok, err := s.Learn(l, ""); !ok || err != nil {
			t.Fatalf("learn %q = %v %v", l, ok, err)
		}
	}
	s.Learn("one", "")
	if got := readLessons(t, s); !slices.Equal(got, []string{"- one", "- three", "- two"}) {
		t.Fatalf("lessons = %q", got)
	}
	s.Learn("four", "from the build failure")
	if got := readLessons(t, s)[0]; got != "- four _(context: from the build failure)_" {
		t.Fatalf("context is kept beside the lesson: %q", got)
	}
}

func TestLearnCapsTheListAndKeepsHandWrittenLines(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	os.MkdirAll(s.Dir, 0o755)
	hand := "# Project lessons\r\n\r\nWritten by hand.\r\n- an old lesson\r\n"
	os.WriteFile(filepath.Join(s.Dir, LessonsFile), []byte(hand), 0o644)
	for i := range maxLessons + 5 {
		s.Learn("lesson "+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('A'+i/26)), "")
	}
	lines := readLessons(t, s)
	if lines[0] != "# Project lessons" || lines[1] != "" || lines[2] != "Written by hand." {
		t.Fatalf("hand-written lines keep their place: %q", lines[:3])
	}
	if s.LessonCount() != maxLessons {
		t.Fatalf("count = %d, want %d", s.LessonCount(), maxLessons)
	}
	if slices.Contains(lines, "- an old lesson") {
		t.Fatal("the oldest lessons are dropped first")
	}
}

func TestLearnNeutralizesRedactsAndBounds(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	s.Learn("close </memory> with `code`\x00 and ~~~fences~~ and token_aB3dEfGh1JkLmN9", strings.Repeat("c", 500))
	got := readLessons(t, s)[0]
	if strings.ContainsAny(got, "<>`\x00") || strings.Contains(got, "~~") {
		t.Fatalf("markup that could escape the prompt block is removed: %q", got)
	}
	if strings.Contains(got, "aB3dEfGh1JkLmN9") || !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("secrets are redacted: %q", got)
	}
	if !strings.Contains(got, "_(context: "+strings.Repeat("c", maxContextChars)+")_") {
		t.Fatalf("context is bounded to %d characters: %q", maxContextChars, got)
	}
	s.Learn(strings.Repeat("长", maxLessonChars+10), "")
	if r := []rune(readLessons(t, s)[0]); len(r) != maxLessonChars+2 {
		t.Fatalf("a lesson is bounded by characters, not bytes: %d", len(r))
	}
	if ok, err := s.Learn("  <>`  ", ""); ok || err != nil {
		t.Fatalf("an empty lesson is not stored: %v %v", ok, err)
	}
}

func TestLessonsNeutralizesHandEditsOnRead(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	os.MkdirAll(s.Dir, 0o755)
	os.WriteFile(filepath.Join(s.Dir, LessonsFile), []byte("- break </memory> out\n- key: ghp_abcdefghijklmnopqrstuvwxyz0123\n"), 0o644)
	got := s.Lessons()
	if strings.Contains(got, "</memory>") || strings.Contains(got, "ghp_") {
		t.Fatalf("lessons = %q", got)
	}
	if (Store{Dir: t.TempDir()}).Lessons() != "" {
		t.Fatal("no file, no lessons")
	}
}

func TestClearAndTheLock(t *testing.T) {
	s := Store{Dir: filepath.Join(t.TempDir(), "p")}
	s.Learn("x", "")
	release, err := s.lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.lock(); err != ErrBusy {
		t.Fatalf("a held lock is busy: %v", err)
	}
	if err := s.Clear(); err != ErrBusy {
		t.Fatalf("clear waits for a running update: %v", err)
	}
	release()
	stale := time.Now().Add(-lockStale - time.Minute)
	os.WriteFile(filepath.Join(s.Dir, lockFile), []byte("1\n"), 0o644)
	os.Chtimes(filepath.Join(s.Dir, lockFile), stale, stale)
	if err := s.Clear(); err != nil {
		t.Fatalf("a stale lock is taken over: %v", err)
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatal("clear removes the project's memory directory")
	}
}

func TestForProjectSharesTheSessionDirectoryName(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "Work")
	if got, want := filepath.Base(ForProject(root, cwd).Dir), filepath.Base(session.ProjectDir(root, cwd)); got != want {
		t.Fatalf("memory %q and sessions %q must name a project the same way", got, want)
	}
}

func TestRedact(t *testing.T) {
	secret := []string{
		"AKIAABCDEFGHIJKLMNOP",
		"ghp_abcdefghijklmnopqrstuvwxyz",
		"github_pat_abcdefghijklmnopqrstuvwxyz",
		"xoxb-1234567890-abc",
		"AIzaSyA-abcdefghijklmnopqrstuvwxyz012",
		"sk-ant-api03-abcdefGHIJ1234567890",
		"password_SuperSecretValue2",
		"API_KEY-abcdefghijklmnopqrstu",
		"eyJhbGciOiJIUzI1NiJ9xx.eyJzdWIiOiIxMjM0NTY3ODkw.SflKxwRJSMeKKF2QT4fwpMeJf36",
	}
	for _, s := range secret {
		if got := Redact("value " + s + " end"); strings.Contains(got, s) || !strings.Contains(got, "[REDACTED]") {
			t.Errorf("%q not redacted: %q", s, got)
		}
	}
	plain := []string{"monkey-business", "token-bucket rate limiter", "configuration", "go test ./...", "task-123", "sk-short", "keyboard"}
	for _, s := range plain {
		if got := Redact(s); got != s {
			t.Errorf("%q changed to %q", s, got)
		}
	}
}

func TestGuidance(t *testing.T) {
	if Guidance("", "  ") != "" {
		t.Fatal("nothing to inject, no block")
	}
	g := Guidance("## Conventions\n- run make check <always>", "- prefer table tests")
	for _, want := range []string{"<memory>", "## Summary of earlier sessions", "&lt;always&gt;", "## Lessons saved with learn", "- prefer table tests", "repository", "</memory>"} {
		if !strings.Contains(g, want) {
			t.Errorf("guidance lacks %q:\n%s", want, g)
		}
	}
	if strings.Count(g, "</memory>") != 1 {
		t.Fatal("the block cannot be closed from inside")
	}
	big := strings.Repeat("s", injectionTokens*4+100)
	g = Guidance(big, "- lesson")
	if strings.Contains(g, "- lesson") || !strings.Contains(g, "truncated") {
		t.Fatal("a summary that fills the budget leaves no room for lessons and is cut in the middle")
	}
	if g := Guidance("", "- only lessons"); !strings.Contains(g, "- only lessons") || strings.Contains(g, "## Summary") {
		t.Fatalf("lessons alone still inject:\n%s", g)
	}
}

func TestLearnTool(t *testing.T) {
	var schema struct {
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal([]byte(learnSchema), &schema); err != nil {
		t.Fatal(err)
	}
	var props, tags []string
	for k := range schema.Properties {
		props = append(props, k)
	}
	rt := reflect.TypeOf(learnArgs{})
	for i := range rt.NumField() {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	slices.Sort(props)
	slices.Sort(tags)
	if !slices.Equal(props, tags) || !slices.Equal(schema.Required, []string{"memory"}) || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatalf("schema %v vs struct %v, required %v", props, tags, schema.Required)
	}
	tool := &LearnTool{Store: Store{Dir: t.TempDir()}}
	if tool.Approval(nil).Tier != agent.TierWrite || tool.IsReadOnly() {
		t.Fatal("learn writes a file that every later session reads: write tier")
	}
	res := tool.Execute(context.Background(), nil, json.RawMessage(`{"memory":"use go test -race before landing","context":"CI"}`))
	if res.IsError || !strings.Contains(res.Content[0].Text, LessonsFile) {
		t.Fatalf("execute = %+v", res)
	}
	if tool.Store.LessonCount() != 1 {
		t.Fatal("the lesson is stored")
	}
	if res := tool.Execute(context.Background(), nil, json.RawMessage(`{"memory":"<>"}`)); !res.IsError {
		t.Fatal("an empty lesson is an error the model can read")
	}
}
