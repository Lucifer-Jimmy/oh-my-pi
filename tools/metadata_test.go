package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

func metadataTable() []agent.Tool {
	state := NewFileState()
	return []agent.Tool{
		&Read{CWD: ".", State: state},
		&Glob{CWD: "."},
		&Grep{CWD: "."},
		&Edit{CWD: ".", State: state},
		&Write{CWD: ".", State: state},
		&Bash{CWD: "."},
	}
}

func TestEveryBuiltinDeclaresMetadata(t *testing.T) {
	for _, tool := range metadataTable() {
		d, ok := tool.(agent.MetadataDeclarer)
		if !ok {
			t.Fatalf("%s does not implement MetadataDeclarer", tool.Name())
		}
		m := d.Metadata(nil)
		if m.MaxResultSize <= 0 {
			t.Errorf("%s declares no result cap", tool.Name())
		}
		if got := agent.MetadataOf(tool, nil); got != m {
			t.Errorf("%s: MetadataOf returned %+v, want the declaration %+v", tool.Name(), got, m)
		}
	}
}

func TestMetadataRiskFlags(t *testing.T) {
	want := map[string]struct{ destructive, openWorld bool }{
		"read":  {false, false},
		"glob":  {false, false},
		"grep":  {false, false},
		"edit":  {true, false},
		"write": {true, false},
		"bash":  {true, true},
	}
	for _, tool := range metadataTable() {
		w, ok := want[tool.Name()]
		if !ok {
			t.Fatalf("no expectation for %s", tool.Name())
		}
		m := agent.MetadataOf(tool, nil)
		if m.Destructive != w.destructive || m.OpenWorld != w.openWorld {
			t.Errorf("%s: destructive=%v openWorld=%v, want %v/%v",
				tool.Name(), m.Destructive, m.OpenWorld, w.destructive, w.openWorld)
		}
	}
}

func TestUndeclaredToolFallsBackToConservativeMetadata(t *testing.T) {
	m := agent.MetadataOf(undeclared{}, nil)
	if !m.Destructive || !m.OpenWorld || m.MaxResultSize <= 0 {
		t.Fatalf("an undeclared tool must look maximally risky, got %+v", m)
	}
}

type undeclared struct{}

func (undeclared) Name() string            { return "undeclared" }
func (undeclared) Description() string     { return "" }
func (undeclared) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (undeclared) Execute(context.Context, agent.ToolContext, json.RawMessage) agent.ToolResult {
	return agent.ToolResult{}
}

func TestReadStaysWithinDeclaredCap(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	long := strings.Repeat("x", 4000)
	if err := os.WriteFile(path, []byte(strings.Repeat(long+"\n", 40)), 0o644); err != nil {
		t.Fatal(err)
	}
	r := &Read{CWD: dir, State: NewFileState()}
	out, isErr := run(t, r, `{"path":"big.txt"}`)
	if isErr {
		t.Fatalf("read failed: %s", out)
	}
	if cap := agent.MetadataOf(r, nil).MaxResultSize; len(out) > cap {
		t.Fatalf("read returned %d bytes, above its declared cap of %d", len(out), cap)
	}
}

func TestBashStaysWithinDeclaredCap(t *testing.T) {
	if shell := ResolveShell(""); shell.Path == "" {
		t.Skip("no shell available")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("line of output\n", 20000)), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Bash{CWD: dir}
	out, isErr := runBash(t, b, `{"command":"cat big.txt"}`)
	if isErr {
		t.Fatalf("bash failed: %s", out)
	}
	if cap := agent.MetadataOf(b, nil).MaxResultSize; len(out) > cap {
		t.Fatalf("bash returned %d bytes, above its declared cap of %d", len(out), cap)
	}
}

func TestGrepSkipsDeniedPaths(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, "a.go", "secret\n")
	mkfile(t, dir, ".env", "secret\n")
	g.Denied = func(rel string) bool { return rel == ".env" }

	out, isErr := runGrep(t, g, `{"pattern":"secret"}`)
	if isErr {
		t.Fatalf("grep failed: %s", out)
	}
	if strings.Contains(out, ".env") {
		t.Errorf("a denied path must not be searched, got %q", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("other files must still be searched, got %q", out)
	}
}

func TestGrepDeniedSingleFileYieldsNoFiles(t *testing.T) {
	g, dir := newGrep(t)
	mkfile(t, dir, ".env", "secret\n")
	g.Denied = func(rel string) bool { return rel == ".env" }

	out, isErr := runGrep(t, g, `{"pattern":"secret","path":".env"}`)
	if isErr {
		t.Fatalf("grep failed: %s", out)
	}
	if strings.Contains(out, "secret") {
		t.Errorf("a denied file must not be read, got %q", out)
	}
}

func TestGlobSkipsDeniedPaths(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "a.go", "package a\n")
	mkfile(t, dir, ".env", "secret\n")
	g := &Glob{CWD: dir, Denied: func(rel string) bool { return rel == ".env" }}

	out, isErr := runGlob(t, g, `{"pattern":"*"}`)
	if isErr {
		t.Fatalf("glob failed: %s", out)
	}
	if strings.Contains(out, ".env") {
		t.Errorf("a denied path must not be listed, got %q", out)
	}
	if !strings.Contains(out, "a.go") {
		t.Errorf("other files must still be listed, got %q", out)
	}
}
