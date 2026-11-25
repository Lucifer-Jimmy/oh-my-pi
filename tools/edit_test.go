package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readThenEdit(t *testing.T, f fileTools, name, content string, args map[string]any) (string, bool) {
	t.Helper()
	mkfile(t, f.dir, name, content)
	call(t, f.read, f.dir, map[string]any{"path": name})
	args["path"] = name
	return call(t, f.edit, f.dir, args)
}

func TestEditRequiresRead(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "a.go", "package a\n")
	got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "a.go", "old_string": "a", "new_string": "b"})
	if !isErr || !strings.Contains(got, "has not been read") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditReplacesUniqueMatch(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.go", "package a\n\nfunc Old() {}\n", map[string]any{"old_string": "func Old()", "new_string": "func New()"})
	if isErr || !strings.Contains(got, "1 replacement(s) at line 3") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if body := readFile(t, filepath.Join(f.dir, "a.go")); body != "package a\n\nfunc New() {}\n" {
		t.Errorf("content = %q", body)
	}
}

func TestEditAmbiguousListsLines(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.txt", "x\ntarget\ny\nz\ntarget\n", map[string]any{"old_string": "target", "new_string": "hit"})
	if !isErr || !strings.Contains(got, "matches 2 places") || !strings.Contains(got, "lines 2, 5") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditReplaceAll(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.txt", "a\nb\na\n", map[string]any{"old_string": "a", "new_string": "c", "replace_all": true})
	if isErr || !strings.Contains(got, "2 replacement(s)") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if body := readFile(t, filepath.Join(f.dir, "a.txt")); body != "c\nb\nc\n" {
		t.Errorf("content = %q", body)
	}
}

func TestEditNotFoundPointsAtIndentation(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.go", "func a() {\n\t\treturn nil\n}\n", map[string]any{"old_string": "    return nil\n}", "new_string": "return err\n}"})
	if !isErr || !strings.Contains(got, "line 2") || !strings.Contains(got, "indentation") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditNotFoundPointsAtTypographicQuotes(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.go", "say(\"hi\")\n", map[string]any{"old_string": "say(“hi”)", "new_string": "say(\"bye\")"})
	if !isErr || !strings.Contains(got, "typographic") || !strings.Contains(got, "line 1") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditNotFoundPlain(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.txt", "alpha\n", map[string]any{"old_string": "omega", "new_string": "x"})
	if !isErr || !strings.Contains(got, "read the file again") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditPreservesCRLFAndBOM(t *testing.T) {
	f := newFileTools(t)
	path := filepath.Join(f.dir, "win.txt")
	if err := os.WriteFile(path, []byte(utf8BOM+"one\r\ntwo\r\nthree\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call(t, f.read, f.dir, map[string]any{"path": "win.txt"})
	if got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "win.txt", "old_string": "one\ntwo", "new_string": "uno\ndos"}); isErr {
		t.Fatalf("edit failed: %q", got)
	}
	if body := readFile(t, path); body != utf8BOM+"uno\r\ndos\r\nthree\r\n" {
		t.Errorf("format not preserved: %q", body)
	}
}

func TestEditAcceptsCRLFInArguments(t *testing.T) {
	f := newFileTools(t)
	got, isErr := readThenEdit(t, f, "a.txt", "one\ntwo\n", map[string]any{"old_string": "one\r\ntwo", "new_string": "1\r\n2"})
	if isErr {
		t.Fatalf("CRLF in arguments must be normalised: %q", got)
	}
	if body := readFile(t, filepath.Join(f.dir, "a.txt")); body != "1\n2\n" {
		t.Errorf("content = %q", body)
	}
}

func TestEditRefusesStaleFile(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "a.txt", "alpha\n")
	call(t, f.read, f.dir, map[string]any{"path": "a.txt"})
	touchLater(t, filepath.Join(f.dir, "a.txt"), "alpha changed elsewhere\n")
	got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "a.txt", "old_string": "alpha", "new_string": "beta"})
	if !isErr || !strings.Contains(got, "changed on disk") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditConsecutiveEditsNeedNoReread(t *testing.T) {
	f := newFileTools(t)
	if got, isErr := readThenEdit(t, f, "a.txt", "one two\n", map[string]any{"old_string": "one", "new_string": "1"}); isErr {
		t.Fatalf("first edit: %q", got)
	}
	if got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "a.txt", "old_string": "two", "new_string": "2"}); isErr {
		t.Fatalf("own edit must count as a read: %q", got)
	}
	if body := readFile(t, filepath.Join(f.dir, "a.txt")); body != "1 2\n" {
		t.Errorf("content = %q", body)
	}
}

func TestEditRejectsDegenerateArguments(t *testing.T) {
	f := newFileTools(t)
	if got, isErr := readThenEdit(t, f, "a.txt", "x\n", map[string]any{"old_string": "", "new_string": "y"}); !isErr || !strings.Contains(got, "must not be empty") {
		t.Errorf("empty old_string: isErr=%v text=%q", isErr, got)
	}
	if got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "a.txt", "old_string": "x", "new_string": "x"}); !isErr || !strings.Contains(got, "identical") {
		t.Errorf("identical strings: isErr=%v text=%q", isErr, got)
	}
}

func TestEditMissingFilePointsAtWrite(t *testing.T) {
	f := newFileTools(t)
	if got, isErr := call(t, f.edit, f.dir, map[string]any{"path": "nope.txt", "old_string": "a", "new_string": "b"}); !isErr || !strings.Contains(got, "use write") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestEditWithoutStateFails(t *testing.T) {
	e := &Edit{CWD: t.TempDir()}
	if got, isErr := call(t, e, e.CWD, map[string]any{"path": "a.txt", "old_string": "a", "new_string": "b"}); !isErr || !strings.Contains(got, "FileState") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}
