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

type fileTools struct {
	dir   string
	state *FileState
	read  *Read
	write *Write
	edit  *Edit
}

func newFileTools(t *testing.T) fileTools {
	t.Helper()
	dir := t.TempDir()
	state := NewFileState()
	return fileTools{
		dir:   dir,
		state: state,
		read:  &Read{CWD: dir, State: state},
		write: &Write{CWD: dir, State: state},
		edit:  &Edit{CWD: dir, State: state},
	}
}

func call(t *testing.T, tool agent.Tool, cwd string, args map[string]any) (string, bool) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res := tool.Execute(context.Background(), &fakeCtx{cwd: cwd}, raw)
	var sb strings.Builder
	for _, c := range res.Content {
		sb.WriteString(c.Text)
	}
	return sb.String(), res.IsError
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func touchLater(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
}

func TestWriteCreatesFileAndParents(t *testing.T) {
	f := newFileTools(t)
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "a/b/c.txt", "content": "hello\n"})
	if isErr || !strings.HasPrefix(got, "Created") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if body := readFile(t, filepath.Join(f.dir, "a", "b", "c.txt")); body != "hello\n" {
		t.Errorf("content = %q", body)
	}
}

func TestWriteExistingRequiresRead(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "x.txt", "original\n")
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "x.txt", "content": "new\n"})
	if !isErr || !strings.Contains(got, "has not been read") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if body := readFile(t, filepath.Join(f.dir, "x.txt")); body != "original\n" {
		t.Errorf("file must be untouched, got %q", body)
	}
}

func TestWriteAfterReadOverwrites(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "x.txt", "original\n")
	call(t, f.read, f.dir, map[string]any{"path": "x.txt"})
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "x.txt", "content": "new\n"})
	if isErr || !strings.HasPrefix(got, "Overwrote") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if body := readFile(t, filepath.Join(f.dir, "x.txt")); body != "new\n" {
		t.Errorf("content = %q", body)
	}
}

func TestWriteRefusesStaleFile(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "x.txt", "original\n")
	call(t, f.read, f.dir, map[string]any{"path": "x.txt"})
	touchLater(t, filepath.Join(f.dir, "x.txt"), "changed by someone else\n")

	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "x.txt", "content": "mine\n"})
	if !isErr || !strings.Contains(got, "changed on disk") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteConsecutiveWritesNeedNoReread(t *testing.T) {
	f := newFileTools(t)
	call(t, f.write, f.dir, map[string]any{"path": "x.txt", "content": "one\n"})
	if got, isErr := call(t, f.write, f.dir, map[string]any{"path": "x.txt", "content": "two\n"}); isErr {
		t.Fatalf("own write must count as a read: %q", got)
	}
}

func TestWritePreservesCRLFAndBOM(t *testing.T) {
	f := newFileTools(t)
	path := filepath.Join(f.dir, "win.txt")
	if err := os.WriteFile(path, []byte(utf8BOM+"one\r\ntwo\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call(t, f.read, f.dir, map[string]any{"path": "win.txt"})
	if got, isErr := call(t, f.write, f.dir, map[string]any{"path": "win.txt", "content": "alpha\nbeta\n"}); isErr {
		t.Fatalf("write failed: %q", got)
	}
	if body := readFile(t, path); body != utf8BOM+"alpha\r\nbeta\r\n" {
		t.Errorf("format not preserved: %q", body)
	}
}

func TestWriteRefusesTruncatedReadOutput(t *testing.T) {
	f := newFileTools(t)
	content := "line one\nline two\n\n" + readNoticePrefix + "1-2 of 900; use offset=3 to continue)\n"
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "big.txt", "content": content})
	if !isErr || !strings.Contains(got, "truncation notice") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "big.txt")); !os.IsNotExist(err) {
		t.Error("refused content must not be written")
	}
}

func TestWriteRefusesCutLine(t *testing.T) {
	f := newFileTools(t)
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "a.txt", "content": "abc" + readLineCutMarker + "\n"})
	if !isErr || !strings.Contains(got, "cut short") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteRefusesLineNumberedContent(t *testing.T) {
	f := newFileTools(t)
	got, isErr := call(t, f.write, f.dir, map[string]any{"path": "a.go", "content": "     1\tpackage a\n     2\t\n     3\tfunc A() {}\n"})
	if !isErr || !strings.Contains(got, "line-number prefixes") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteAllowsTabularNumbers(t *testing.T) {
	f := newFileTools(t)
	if got, isErr := call(t, f.write, f.dir, map[string]any{"path": "data.tsv", "content": "1\tapple\n2\tpear\n"}); isErr {
		t.Errorf("ordinary TSV must not be mistaken for read output: %q", got)
	}
}

func TestWriteRefusesDirectory(t *testing.T) {
	f := newFileTools(t)
	if err := os.Mkdir(filepath.Join(f.dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, isErr := call(t, f.write, f.dir, map[string]any{"path": "sub", "content": "x"}); !isErr || !strings.Contains(got, "not a regular file") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteOutsideWorkspace(t *testing.T) {
	f := newFileTools(t)
	if got, isErr := call(t, f.write, f.dir, map[string]any{"path": "../escape.txt", "content": "x"}); !isErr || !strings.Contains(got, "outside the workspace") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteWithoutStateFails(t *testing.T) {
	w := &Write{CWD: t.TempDir()}
	if got, isErr := call(t, w, w.CWD, map[string]any{"path": "a.txt", "content": "x"}); !isErr || !strings.Contains(got, "FileState") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestWriteLeavesNoTempFiles(t *testing.T) {
	f := newFileTools(t)
	call(t, f.write, f.dir, map[string]any{"path": "a.txt", "content": "one\n"})
	call(t, f.write, f.dir, map[string]any{"path": "a.txt", "content": "two\n"})
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.txt" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("atomic write left extra files: %v", names)
	}
}

func TestFileApprovalForcesPromptInsideGit(t *testing.T) {
	cases := map[string]bool{
		".git/hooks/pre-commit":     true,
		`C:\repo\.git\config`:       true,
		"sub/.GIT/config":           true,
		".github/workflows/ci.yml":  false,
		"src/main.go":               false,
		"docs/.gitignore-notes.txt": false,
	}
	for p, force := range cases {
		a := fileApproval(p)
		if a.ForcePrompt != force || a.Tier != agent.TierWrite {
			t.Errorf("%q: got %+v, want ForcePrompt=%v", p, a, force)
		}
	}
}

func TestWriteReviewSpecsAreWorkspaceRelative(t *testing.T) {
	f := newFileTools(t)
	specs := f.write.ReviewSpecs(json.RawMessage(`{"path":"a/b.txt"}`))
	if len(specs) != 1 || specs[0].Value != "a/b.txt" {
		t.Errorf("got %+v", specs)
	}
}
