package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

var _ agent.Tool = (*Grep)(nil)

func newGrep(t *testing.T) (*Grep, string) {
	t.Helper()
	dir := t.TempDir()
	return &Grep{CWD: dir}, dir
}

func runGrep(t *testing.T, g *Grep, args string) (string, bool) {
	t.Helper()
	res := g.Execute(context.Background(), &fakeCtx{cwd: g.CWD}, json.RawMessage(args))
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func TestGrepContentMode(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.go", "package a\nfunc hello() {}\n")
	mkfile(t, dir, "b.go", "package b\n")

	got, isErr := runGrep(t, g, `{"pattern":"hello"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "a.go:2:func hello() {}") {
		t.Errorf("got %q", got)
	}
	if strings.Contains(got, "b.go") {
		t.Errorf("non-matching file leaked: %q", got)
	}
}

func TestGrepFixedString(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "value is a.b\n")

	got, _ := runGrep(t, g, `{"pattern":"a.b","fixed_string":true}`)
	if !strings.Contains(got, "a.txt:1:value is a.b") {
		t.Errorf("got %q", got)
	}
}

func TestGrepRegexIsRegex(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "axb\n")

	got, _ := runGrep(t, g, `{"pattern":"a.b"}`)
	if !strings.Contains(got, "a.txt:1:axb") {
		t.Errorf("a.b must match axb as regex, got %q", got)
	}
	got, _ = runGrep(t, g, `{"pattern":"a\\.b"}`)
	if !strings.Contains(got, "(no matches)") {
		t.Errorf("escaped dot should not match axb, got %q", got)
	}
}

func TestGrepIgnoreCase(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "HELLO\n")

	if got, _ := runGrep(t, g, `{"pattern":"hello"}`); !strings.Contains(got, "(no matches)") {
		t.Errorf("case-sensitive should miss, got %q", got)
	}
	if got, _ := runGrep(t, g, `{"pattern":"hello","ignore_case":true}`); !strings.Contains(got, "a.txt:1:HELLO") {
		t.Errorf("ignore_case should match, got %q", got)
	}
	if got, _ := runGrep(t, g, `{"pattern":"hello","ignore_case":true,"fixed_string":true}`); !strings.Contains(got, "a.txt:1:HELLO") {
		t.Errorf("fixed+ignore_case should match, got %q", got)
	}
}

func TestGrepFilesWithMatches(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "hit\n")
	mkfile(t, dir, "b.txt", "hit\n")

	got, _ := runGrep(t, g, `{"pattern":"hit","output_mode":"files_with_matches"}`)
	ls := lines(got)
	if len(ls) != 2 {
		t.Fatalf("want 2 files, got %v", ls)
	}
	for _, p := range ls {
		if strings.Contains(p, ":") {
			t.Errorf("files_with_matches must not include line info: %q", p)
		}
	}
}

func TestGrepCount(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "hit\nmiss\nhit\n")

	got, _ := runGrep(t, g, `{"pattern":"hit","output_mode":"count"}`)
	if !strings.Contains(got, "a.txt:2") {
		t.Errorf("got %q", got)
	}
}

func TestGrepGlobFilter(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.go", "target\n")
	mkfile(t, dir, "b.txt", "target\n")

	got, _ := runGrep(t, g, `{"pattern":"target","glob":"*.go"}`)
	if !strings.Contains(got, "a.go") || strings.Contains(got, "b.txt") {
		t.Errorf("got %q", got)
	}
}

func TestGrepSkipsBinary(t *testing.T) {
	g, dir := newGrep(t)
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("target\x00\x01\x02"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	mkfile(t, dir, "a.txt", "target\n")

	got, _ := runGrep(t, g, `{"pattern":"target"}`)
	if strings.Contains(got, "bin.dat") {
		t.Errorf("binary file leaked: %q", got)
	}
	if !strings.Contains(got, "a.txt") {
		t.Errorf("text file missing: %q", got)
	}
	if !strings.Contains(got, "binary files skipped") {
		t.Errorf("missing binary skip note: %q", got)
	}
}

func TestGrepSkipsGitignore(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, ".gitignore", "vendored/\n")
	mkfile(t, dir, "vendored/x.go", "target\n")
	mkfile(t, dir, "a.go", "target\n")

	got, _ := runGrep(t, g, `{"pattern":"target"}`)
	if strings.Contains(got, "vendored/") {
		t.Errorf("gitignored file leaked: %q", got)
	}
}

func TestGrepLimitTruncates(t *testing.T) {
	g, dir := newGrep(t)
	var b strings.Builder
	for i := 0; i < 20; i++ {
		b.WriteString("target\n")
	}
	mkfile(t, dir, "a.txt", b.String())

	got, _ := runGrep(t, g, `{"pattern":"target","limit":5}`)
	if n := strings.Count(got, "a.txt:"); n != 5 {
		t.Errorf("want 5 matches, got %d", n)
	}
	if !strings.Contains(got, "truncated at 5 matches") {
		t.Errorf("missing truncation note: %q", got)
	}
}

func TestGrepSingleFilePath(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "target\n")
	mkfile(t, dir, "b.txt", "target\n")

	got, _ := runGrep(t, g, `{"pattern":"target","path":"a.txt"}`)
	if !strings.Contains(got, "a.txt") || strings.Contains(got, "b.txt") {
		t.Errorf("got %q", got)
	}
}

func TestGrepOutsideWorkspace(t *testing.T) {
	g, _ := newGrep(t)

	got, isErr := runGrep(t, g, `{"pattern":"x","path":".."}`)
	if !isErr {
		t.Fatalf("escape must fail, got %q", got)
	}
}

func TestGrepBadRegex(t *testing.T) {
	g, _ := newGrep(t)

	got, isErr := runGrep(t, g, `{"pattern":"[unclosed"}`)
	if !isErr || !strings.Contains(got, "grep:") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestGrepMissingPattern(t *testing.T) {
	g, _ := newGrep(t)

	got, isErr := runGrep(t, g, `{}`)
	if !isErr || !strings.Contains(got, "pattern is required") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestGrepUnknownOutputMode(t *testing.T) {
	g, _ := newGrep(t)

	got, isErr := runGrep(t, g, `{"pattern":"x","output_mode":"bogus"}`)
	if !isErr || !strings.Contains(got, "unknown output_mode") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestGrepNoMatches(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "nothing here\n")

	got, isErr := runGrep(t, g, `{"pattern":"absent"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if got != "(no matches)" {
		t.Errorf("got %q", got)
	}
}

func TestGrepTimeoutReportsPartial(t *testing.T) {
	g, dir := newGrep(t)
	for i := 0; i < 50; i++ {
		mkfile(t, dir, fmt.Sprintf("f%02d.txt", i), strings.Repeat("target\n", 200))
	}
	g.Timeout = time.Nanosecond

	got, isErr := runGrep(t, g, `{"pattern":"target"}`)
	if isErr {
		t.Fatalf("timeout is not a hard error: %s", got)
	}
	if !strings.Contains(got, "timed out") || !strings.Contains(got, "NOT proof of absence") {
		t.Errorf("timeout notice missing: %q", got)
	}
}

func TestGrepResultsSorted(t *testing.T) {
	g, dir := newGrep(t)
	for _, n := range []string{"c.txt", "a.txt", "b.txt"} {
		mkfile(t, dir, n, "target\n")
	}

	got, _ := runGrep(t, g, `{"pattern":"target","output_mode":"files_with_matches"}`)
	ls := lines(got)
	want := []string{"a.txt", "b.txt", "c.txt"}
	if len(ls) != len(want) {
		t.Fatalf("got %v", ls)
	}
	for i := range want {
		if ls[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, ls[i], want[i])
		}
	}
}

func TestGrepOverlongLineReported(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.txt", "target\n")
	mkfile(t, dir, "huge.txt", "target\n"+strings.Repeat("x", grepScanBufMax+1)+"\n")

	got, isErr := runGrep(t, g, `{"pattern":"target"}`)
	if isErr {
		t.Fatalf("one unreadable file must not fail the whole search: %s", got)
	}
	if !strings.Contains(got, "a.txt:1:target") {
		t.Errorf("readable file missing: %q", got)
	}
	if !strings.Contains(got, "huge.txt:1:target") {
		t.Errorf("matches before the overlong line must be kept: %q", got)
	}
	if !strings.Contains(got, "1 files could not be read fully") {
		t.Errorf("scanner error must surface as a note: %q", got)
	}
}
