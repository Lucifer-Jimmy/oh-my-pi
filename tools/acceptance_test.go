package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAcceptanceUnicodePathRoundTrip(t *testing.T) {
	f := newFileTools(t)
	rel := filepath.Join("子目录", "文件名 with space.txt")
	body := "第一行\r\nsecond line\r\n"
	if err := os.MkdirAll(filepath.Join(f.dir, "子目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, rel), []byte(body), 0o644); err != nil {
		t.Skipf("filesystem cannot store the name: %v", err)
	}

	got, isErr := call(t, f.read, f.dir, map[string]any{"path": rel})
	if isErr || !strings.Contains(got, "第一行") {
		t.Fatalf("read a Unicode path: isErr=%v text=%q", isErr, got)
	}

	if got, isErr := call(t, f.edit, f.dir, map[string]any{
		"path": rel, "old_string": "second line", "new_string": "第二行",
	}); isErr {
		t.Fatalf("edit a Unicode path failed: %q", got)
	}
	if body := readFile(t, filepath.Join(f.dir, rel)); body != "第一行\r\n第二行\r\n" {
		t.Errorf("CRLF must survive a Unicode-path edit, got %q", body)
	}

	if got, isErr := call(t, f.write, f.dir, map[string]any{
		"path": filepath.Join("新建", "另存.txt"), "content": "ok\n",
	}); isErr {
		t.Fatalf("write a Unicode path failed: %q", got)
	}
	if body := readFile(t, filepath.Join(f.dir, "新建", "另存.txt")); body != "ok\n" {
		t.Errorf("written body = %q", body)
	}
}

func TestAcceptanceGlobAndGrepSeeUnicodeNames(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "目录"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "目录", "文件.go")
	if err := os.WriteFile(target, []byte("package 目录\n// 标记\n"), 0o644); err != nil {
		t.Skipf("filesystem cannot store the name: %v", err)
	}

	g := &Glob{CWD: dir}
	got, isErr := call(t, g, dir, map[string]any{"pattern": "**/*.go"})
	if isErr || !strings.Contains(got, "文件.go") {
		t.Fatalf("glob must find a Unicode name: isErr=%v text=%q", isErr, got)
	}

	s := &Grep{CWD: dir}
	got, isErr = call(t, s, dir, map[string]any{"pattern": "标记"})
	if isErr || !strings.Contains(got, "文件.go") {
		t.Fatalf("grep must match non-ASCII content in a Unicode path: isErr=%v text=%q", isErr, got)
	}
}

func TestAcceptanceCRLFEditAcrossWholeFile(t *testing.T) {
	f := newFileTools(t)
	path := filepath.Join(f.dir, "crlf.txt")
	if err := os.WriteFile(path, []byte("a\r\nb\r\nc\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	call(t, f.read, f.dir, map[string]any{"path": "crlf.txt"})
	if got, isErr := call(t, f.edit, f.dir, map[string]any{
		"path": "crlf.txt", "old_string": "b", "new_string": "B",
	}); isErr {
		t.Fatalf("edit failed: %q", got)
	}
	if body := readFile(t, path); body != "a\r\nB\r\nc\r\n" {
		t.Errorf("a one-character edit must not change line endings: %q", body)
	}
}

func TestAcceptanceNoExternalBinaryNeeded(t *testing.T) {
	f := newFileTools(t)
	mkfile(t, f.dir, "a.txt", "needle\n")
	for name, run := range map[string]func() (string, bool){
		"read": func() (string, bool) {
			return call(t, f.read, f.dir, map[string]any{"path": "a.txt"})
		},
		"glob": func() (string, bool) {
			return call(t, &Glob{CWD: f.dir}, f.dir, map[string]any{"pattern": "*.txt"})
		},
		"grep": func() (string, bool) {
			return call(t, &Grep{CWD: f.dir}, f.dir, map[string]any{"pattern": "needle"})
		},
	} {
		if got, isErr := run(); isErr || got == "" {
			t.Errorf("%s must work without external binaries: isErr=%v text=%q", name, isErr, got)
		}
	}
}

func TestAcceptanceShellIsAlwaysNamed(t *testing.T) {
	sh := ResolveShell("")
	if sh.Path == "" || sh.Name == "" {
		t.Fatalf("a shell must always resolve, even with nothing installed: %+v", sh)
	}
	if runtime.GOOS == "windows" && sh.Name == shellCmd && len(sh.Args) == 0 {
		t.Fatalf("cmd must carry /C: %+v", sh)
	}
	if len(sh.Args) == 0 {
		t.Fatalf("every shell needs an argument prefix: %+v", sh)
	}
}
