package prompt

import (
	"runtime"
	"strings"
	"testing"
)

var shipped = []string{"read", "glob", "grep"}

func TestBuildReturnsTwoBlocks(t *testing.T) {
	blocks := Build(Context{CWD: `C:\work`, Shell: "PowerShell", Date: "2026-11-12"}, shipped)
	if len(blocks) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(blocks))
	}
	if CacheBoundary != 1 {
		t.Errorf("CacheBoundary must match the block layout, got %d", CacheBoundary)
	}
	if strings.Contains(blocks[0], "<project-context>") {
		t.Error("static block must not carry per-run context")
	}
	if !strings.HasPrefix(blocks[1], "<project-context>") {
		t.Errorf("second block must be the context block, got %q", blocks[1][:40])
	}
}

func TestStaticBlockIsStable(t *testing.T) {
	a := Build(Context{CWD: `C:\a`, Date: "2026-11-12"}, shipped)[0]
	b := Build(Context{CWD: `D:\b`, Date: "2027-01-01"}, shipped)[0]
	if a != b {
		t.Error("first block must not vary between runs, or the prompt cache never hits")
	}
}

func TestStaticBlockIsOrderIndependent(t *testing.T) {
	a := Build(Context{}, []string{"read", "glob", "grep"})[0]
	b := Build(Context{}, []string{"grep", "read", "glob"})[0]
	if a != b {
		t.Error("tool description order must not depend on registration order")
	}
}

func TestContextBlockCarriesEnvironment(t *testing.T) {
	got := Build(Context{CWD: `C:\proj`, Shell: "PowerShell", Date: "2026-11-12"}, shipped)[1]
	for _, want := range []string{
		`C:\proj`,
		runtime.GOOS + "/" + runtime.GOARCH,
		"2026-11-12",
		"PowerShell",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("context block missing %q:\n%s", want, got)
		}
	}
}

func TestContextBlockOmitsEmptyFields(t *testing.T) {
	got := Build(Context{}, shipped)[1]
	if strings.Contains(got, "Working directory") {
		t.Errorf("empty CWD must not render a line: %s", got)
	}
	if strings.Contains(got, "Date") {
		t.Errorf("empty date must not render a line: %s", got)
	}
}

func TestStaticBlockNamesOnlyRegisteredTools(t *testing.T) {
	block := Build(Context{}, shipped)[0]
	for _, tool := range shipped {
		if !strings.Contains(block, tool) {
			t.Errorf("static block must describe %q, the model cannot use what it is not told about", tool)
		}
	}

	start := strings.Index(block, "# Tools")
	end := strings.Index(block, "# Working rules")
	if start < 0 || end < 0 || end < start {
		t.Fatalf("missing tool or working-rules section:\n%s", block)
	}
	tools := block[start:end]
	for _, absent := range []string{"bash", "edit", "write", "todo", "task", "lsp"} {
		if strings.Contains(tools, absent) {
			t.Errorf("tool section advertises %q, which is not registered", absent)
		}
	}
}

func TestStaticBlockGainsBashWhenRegistered(t *testing.T) {
	withBash := Build(Context{}, []string{"read", "glob", "grep", "bash"})[0]
	if !strings.Contains(withBash, "bash:") {
		t.Error("bash must appear in the tool section once registered")
	}
	if strings.Contains(withBash, "cannot edit files or run commands") {
		t.Error("registering bash must remove 'cannot run commands'")
	}
	if !strings.Contains(withBash, "cannot edit files") {
		t.Error("editing is still unavailable and must remain declared")
	}
}

func TestStaticBlockGainsEditWhenRegistered(t *testing.T) {
	withEdit := Build(Context{}, []string{"read", "glob", "grep", "edit", "write"})[0]
	if !strings.Contains(withEdit, "edit:") || !strings.Contains(withEdit, "write:") {
		t.Error("edit and write must appear once registered")
	}
	if strings.Contains(withEdit, "cannot edit files") {
		t.Error("registering edit must remove 'cannot edit files'")
	}
	if !strings.Contains(withEdit, "cannot run commands") {
		t.Error("bash is still unavailable and must remain declared")
	}
}

func TestStaticBlockDeclaresMissingCapabilities(t *testing.T) {
	block := Build(Context{}, shipped)[0]
	if !strings.Contains(block, "cannot edit files or run commands") {
		t.Error("block must say editing and command execution are unavailable, or the model will promise them")
	}
}

func TestStaticBlockWithNoTools(t *testing.T) {
	block := Build(Context{}, nil)[0]
	if !strings.Contains(block, "no tools") {
		t.Error("empty tool set must be stated explicitly")
	}
	if strings.Contains(block, "glob:") || strings.Contains(block, "read:") {
		t.Error("no tool descriptions may appear when nothing is registered")
	}
}

func TestGrepRulesOnlyWithGrep(t *testing.T) {
	without := Build(Context{}, []string{"read"})[0]
	if strings.Contains(without, "Locate it with glob") || strings.Contains(without, "empty search result") {
		t.Error("grep rules must not appear when grep is absent")
	}
	with := Build(Context{}, []string{"read", "grep"})[0]
	if !strings.Contains(with, "empty search result") {
		t.Error("grep rules must appear when grep is present")
	}
}

func TestContextFromEnv(t *testing.T) {
	c := ContextFromEnv(`C:\x`, "Git Bash")
	if c.CWD != `C:\x` || c.Shell != "Git Bash" {
		t.Errorf("CWD or shell not carried: %+v", c)
	}
	if c.Date == "" || len(c.Date) != len("2006-01-02") {
		t.Errorf("date must be YYYY-MM-DD, got %q", c.Date)
	}
}
