package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

var _ agent.Tool = (*Glob)(nil)

func newGlob(t *testing.T) (*Glob, string) {
	t.Helper()
	dir := t.TempDir()
	return &Glob{CWD: dir}, dir
}

func runGlob(t *testing.T, g *Glob, args string) (string, bool) {
	t.Helper()
	res := g.Execute(context.Background(), &fakeCtx{cwd: g.CWD}, json.RawMessage(args))
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func mkfile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func lines(got string) []string {
	var out []string
	for _, l := range strings.Split(got, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "...") {
			continue
		}
		out = append(out, l)
	}
	return out
}

func TestGlobBaseName(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "a.go", "a")
	mkfile(t, dir, "b.txt", "b")
	mkfile(t, dir, "sub/c.go", "c")

	got, isErr := runGlob(t, g, `{"pattern":"*.go"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	got2 := lines(got)
	if len(got2) != 2 {
		t.Fatalf("want 2 go files, got %v", got2)
	}
	for _, p := range got2 {
		if !strings.HasSuffix(p, ".go") {
			t.Errorf("non-go file leaked: %q", p)
		}
	}
}

func TestGlobPathPattern(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "src/a.ts", "a")
	mkfile(t, dir, "src/deep/b.ts", "b")
	mkfile(t, dir, "other/c.ts", "c")

	got, _ := runGlob(t, g, `{"pattern":"src/**/*.ts"}`)
	got2 := lines(got)
	if len(got2) != 2 {
		t.Fatalf("want 2 matches under src, got %v", got2)
	}
	for _, p := range got2 {
		if !strings.HasPrefix(p, "src/") {
			t.Errorf("match outside src: %q", p)
		}
	}
}

func TestGlobSkipsGitignore(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, ".gitignore", "ignored.txt\nbuild/\n")
	mkfile(t, dir, "keep.go", "k")
	mkfile(t, dir, "ignored.txt", "x")
	mkfile(t, dir, "build/out.go", "o")

	got, _ := runGlob(t, g, `{"pattern":"*"}`)
	for _, p := range lines(got) {
		if p == "ignored.txt" || strings.HasPrefix(p, "build/") {
			t.Errorf("gitignored path leaked: %q", p)
		}
	}
}

func TestGlobSkipsDotGit(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, ".git/config", "x")
	mkfile(t, dir, "a.go", "a")

	got, _ := runGlob(t, g, `{"pattern":"*"}`)
	for _, p := range lines(got) {
		if strings.HasPrefix(p, ".git/") {
			t.Errorf(".git leaked: %q", p)
		}
	}
}

func TestGlobNoDirectories(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "sub/inner.txt", "x")

	got, _ := runGlob(t, g, `{"pattern":"*"}`)
	for _, p := range lines(got) {
		if strings.HasSuffix(p, "/") {
			t.Errorf("directory returned: %q", p)
		}
	}
}

func TestGlobNoMatch(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "a.go", "a")

	got, isErr := runGlob(t, g, `{"pattern":"*.nomatch"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if got != "(no files matched)" {
		t.Errorf("got %q", got)
	}
}

func TestGlobLimit(t *testing.T) {
	g, dir := newGlob(t)
	for i := 0; i < 10; i++ {
		mkfile(t, dir, "f"+string(rune('a'+i))+".go", "x")
	}

	got, _ := runGlob(t, g, `{"pattern":"*.go","limit":3}`)
	if n := len(lines(got)); n != 3 {
		t.Errorf("want 3 results, got %d: %v", n, lines(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("missing truncation notice: %q", got)
	}
}

func TestGlobTimeOrderNewestFirst(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "old.go", "o")
	mkfile(t, dir, "new.go", "n")

	old := filepath.Join(dir, "old.go")
	now := time.Now()
	if err := os.Chtimes(old, now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	got, _ := runGlob(t, g, `{"pattern":"*.go"}`)
	got2 := lines(got)
	if len(got2) < 2 || got2[0] != "new.go" {
		t.Errorf("want newest first, got %v", got2)
	}
}

func TestGlobTimeoutReportsPartial(t *testing.T) {
	g, dir := newGlob(t)
	for i := 0; i < 200; i++ {
		mkfile(t, dir, filepath.ToSlash(filepath.Join("deep", "d"+string(rune('a'+i%26)), "f"+string(rune('a'+i%26))+".go")), "x")
	}
	g.Timeout = time.Nanosecond

	got, isErr := runGlob(t, g, `{"pattern":"*.go"}`)
	if isErr {
		t.Fatalf("timeout is not a hard error: %s", got)
	}
	if !strings.Contains(got, "timed out") || !strings.Contains(got, "NOT proof of absence") {
		t.Errorf("timeout notice missing: %q", got)
	}
}

func TestGlobScopedPath(t *testing.T) {
	g, dir := newGlob(t)
	mkfile(t, dir, "a/x.go", "x")
	mkfile(t, dir, "b/y.go", "y")

	got, _ := runGlob(t, g, `{"pattern":"*.go","path":"a"}`)
	got2 := lines(got)
	if len(got2) != 1 || got2[0] != "a/x.go" {
		t.Errorf("got %v", got2)
	}
}

func TestGlobOutsideWorkspace(t *testing.T) {
	g, _ := newGlob(t)

	got, isErr := runGlob(t, g, `{"pattern":"*","path":".."}`)
	if !isErr {
		t.Fatalf("escape must fail, got %q", got)
	}
}

func TestGlobMissingPattern(t *testing.T) {
	g, _ := newGlob(t)

	got, isErr := runGlob(t, g, `{}`)
	if !isErr || !strings.Contains(got, "pattern is required") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestMatchSegments(t *testing.T) {
	cases := []struct {
		pat, path string
		want      bool
	}{
		{"**/*.ts", "src/a.ts", true},
		{"**/*.ts", "src/deep/a.ts", true},
		{"**/*.ts", "a.ts", true},
		{"src/**", "src/a/b.go", true},
		{"src/*.go", "src/a/b.go", false},
		{"src/*.go", "src/a.go", true},
		{"a/**/z", "a/b/c/z", true},
		{"a/**/z", "a/z", true},
	}
	for _, c := range cases {
		got := matchPath(c.pat, c.path)
		if got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.pat, c.path, got, c.want)
		}
	}
}

func TestGitignoreNegation(t *testing.T) {
	gi := &gitignore{}
	gi.rules = []ignoreRule{
		{pattern: "*.log", matchFull: false},
		{pattern: "keep.log", negate: true, matchFull: false},
	}
	gi.hasSegmentRule = true

	if !gi.ignored("a.log", false) {
		t.Error("a.log should be ignored")
	}
	if gi.ignored("keep.log", false) {
		t.Error("keep.log should be re-included")
	}
}

func TestGitignoreDirOnly(t *testing.T) {
	gi := &gitignore{rules: []ignoreRule{{pattern: "build", dirOnly: true, matchFull: false}}, hasSegmentRule: true}

	if !gi.ignored("build", true) {
		t.Error("build dir should be ignored")
	}
	if gi.ignored("build", false) {
		t.Error("a file named build should not be ignored by a dir-only rule")
	}
}

func TestGitignoreAnchored(t *testing.T) {
	gi := &gitignore{rules: []ignoreRule{{pattern: "top.txt", anchored: true, matchFull: true}}}

	if !gi.ignored("top.txt", false) {
		t.Error("anchored root match failed")
	}
	if gi.ignored("sub/top.txt", false) {
		t.Error("anchored pattern must not match nested path")
	}
}
