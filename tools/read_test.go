package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

var _ agent.Tool = (*Read)(nil)

type fakeCtx struct {
	cwd        string
	workspaces []string
}

func (f *fakeCtx) CWD() string                   { return f.cwd }
func (f *fakeCtx) Workspaces() []string          { return f.workspaces }
func (f *fakeCtx) Emit(agent.Event)              {}
func (f *fakeCtx) RecentMessages() []llm.Message { return nil }
func (f *fakeCtx) Ask(context.Context, agent.AskRequest) (agent.AskResponse, error) {
	return agent.AskResponse{}, nil
}

func run(t *testing.T, r *Read, args string) (string, bool) {
	t.Helper()
	res := r.Execute(context.Background(), &fakeCtx{cwd: r.CWD}, json.RawMessage(args))
	var sb strings.Builder
	for _, b := range res.Content {
		sb.WriteString(b.Text)
	}
	return sb.String(), res.IsError
}

func newRead(t *testing.T) (*Read, string) {
	t.Helper()
	dir := t.TempDir()
	return &Read{CWD: dir}, dir
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return full
}

func TestReadNumbered(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "a.txt", "one\ntwo\nthree\n")

	got, isErr := run(t, r, `{"path":"a.txt"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	want := "     1\tone\n     2\ttwo\n     3\tthree\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadOffsetLimit(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "a.txt", "one\ntwo\nthree\nfour\n")

	got, isErr := run(t, r, `{"path":"a.txt","offset":2,"limit":3}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	want := "     2\ttwo\n     3\tthree\n     4\tfour\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("reading to EOF must not report truncation: %q", got)
	}
}

func TestReadLimitStopsBeforeEOF(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "a.txt", "one\ntwo\nthree\nfour\n")

	got, _ := run(t, r, `{"path":"a.txt","offset":2,"limit":2}`)
	if !strings.Contains(got, "     2\ttwo\n     3\tthree\n") {
		t.Errorf("wrong lines shown: %q", got)
	}
	if !strings.Contains(got, "showed lines 2-3 of 4") {
		t.Errorf("must report remaining line 4: %q", got)
	}
	if !strings.Contains(got, "use offset=4 to continue") {
		t.Errorf("wrong continuation offset: %q", got)
	}
}

func TestReadTruncationNotice(t *testing.T) {
	r, dir := newRead(t)
	var b strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&b, "l%d\n", i)
	}
	write(t, dir, "a.txt", b.String())

	got, isErr := run(t, r, `{"path":"a.txt","limit":3}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "showed lines 1-3 of 10") {
		t.Errorf("missing truncation notice: %q", got)
	}
	if !strings.Contains(got, "use offset=4 to continue") {
		t.Errorf("missing continuation offset: %q", got)
	}
	if strings.Contains(got, "l4") {
		t.Errorf("line beyond limit leaked: %q", got)
	}
}

func TestReadTruncationFromOffset(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "a.txt", "l1\nl2\nl3\nl4\nl5\nl6\nl7\nl8\nl9\nl10\n")

	got, isErr := run(t, r, `{"path":"a.txt","offset":5,"limit":2}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "showed lines 5-6 of 10") {
		t.Errorf("wrong range: %q", got)
	}
	if !strings.Contains(got, "use offset=7 to continue") {
		t.Errorf("wrong next offset: %q", got)
	}
}

func TestReadNoTruncationAtEOF(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "a.txt", "only\n")

	got, _ := run(t, r, `{"path":"a.txt","limit":50}`)
	if strings.Contains(got, "truncated") {
		t.Errorf("must not report truncation at EOF: %q", got)
	}
}

func TestReadLongLineTruncated(t *testing.T) {
	r, dir := newRead(t)
	long := strings.Repeat("x", readMaxLineLen+500)
	write(t, dir, "a.txt", long+"\n")

	got, isErr := run(t, r, `{"path":"a.txt"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "(line truncated)") {
		t.Errorf("missing long-line marker: %q", got)
	}
	if len(got) > readMaxLineLen+100 {
		t.Errorf("long line not cut, got %d bytes", len(got))
	}
}

func TestReadDirectoryListing(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "b.txt", "b")
	write(t, dir, "a.txt", "a")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, isErr := run(t, r, `{"path":"."}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	want := "a.txt\nb.txt\nsub" + string(filepath.Separator) + "\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReadEmptyDirectory(t *testing.T) {
	r, dir := newRead(t)
	empty := filepath.Join(dir, "empty")
	if err := os.Mkdir(empty, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, _ := run(t, r, `{"path":"empty"}`)
	if got != "(empty directory)" {
		t.Errorf("got %q", got)
	}
}

func TestReadMissingFile(t *testing.T) {
	r, _ := newRead(t)

	got, isErr := run(t, r, `{"path":"nope.txt"}`)
	if !isErr || !strings.Contains(got, "does not exist") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestReadOutsideWorkspace(t *testing.T) {
	r, _ := newRead(t)

	got, isErr := run(t, r, `{"path":"../escape.txt"}`)
	if !isErr || !strings.Contains(got, "outside the workspace") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestReadEmptyArgs(t *testing.T) {
	r, _ := newRead(t)

	got, isErr := run(t, r, ``)
	if !isErr || !strings.Contains(got, "path is required") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestReadInvalidJSON(t *testing.T) {
	r, _ := newRead(t)

	got, isErr := run(t, r, `{"path":`)
	if !isErr || !strings.Contains(got, "invalid arguments") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestReadSubdirectoryFile(t *testing.T) {
	r, dir := newRead(t)
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	write(t, filepath.Join(dir, "pkg"), "x.go", "package x\n")

	got, isErr := run(t, r, `{"path":"pkg/x.go"}`)
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "package x") {
		t.Errorf("got %q", got)
	}
}

func TestReadAbsolutePathInsideWorkspace(t *testing.T) {
	r, dir := newRead(t)
	full := write(t, dir, "a.txt", "abs\n")

	got, isErr := run(t, r, fmt.Sprintf(`{"path":%q}`, full))
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "abs") {
		t.Errorf("got %q", got)
	}
}

func TestReadWorkspaceList(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	write(t, other, "b.txt", "from other\n")
	r := &Read{CWD: dir, Workspaces: []string{dir, other}}

	got, isErr := run(t, r, fmt.Sprintf(`{"path":%q}`, filepath.Join(other, "b.txt")))
	if isErr {
		t.Fatalf("unexpected error: %s", got)
	}
	if !strings.Contains(got, "from other") {
		t.Errorf("got %q", got)
	}
}

func TestReadApprovalTier(t *testing.T) {
	r := &Read{}
	if a := r.Approval(nil); a.Tier != agent.TierRead {
		t.Errorf("want read tier, got %q", a.Tier)
	}
	if !r.IsReadOnly() || !r.IsConcurrencySafe(nil) {
		t.Error("read must be read-only and concurrency-safe")
	}
}

func TestResolveRejectsSiblingPrefix(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, err := resolve([]string{root}, filepath.Join(base, "root2", "x")); err == nil {
		t.Error("sibling dir sharing a name prefix must be rejected")
	}
}

func TestResolveAcceptsRootItself(t *testing.T) {
	root := t.TempDir()
	if _, err := resolve([]string{root}, "."); err != nil {
		t.Errorf("root itself must be allowed: %v", err)
	}
}

func TestExpandHome(t *testing.T) {
	if got, _ := expandHome("relative/x"); got != "relative/x" {
		t.Errorf("plain path must pass through, got %q", got)
	}
	got, err := expandHome("~")
	if err != nil {
		t.Fatalf("expand ~: %v", err)
	}
	if got == "~" || got == "" {
		t.Errorf("~ not expanded: %q", got)
	}
	home, _ := os.UserHomeDir()
	if strings.HasPrefix(got, home) == false {
		t.Errorf("expanded path %q outside home %q", got, home)
	}
}

func TestReadOverlongLineReportsError(t *testing.T) {
	r, dir := newRead(t)
	huge := strings.Repeat("x", readScanBufMax+1)
	write(t, dir, "huge.txt", huge+"\n")

	got, isErr := run(t, r, `{"path":"huge.txt"}`)
	if !isErr {
		t.Fatalf("overlong line must surface an error, got %q", got)
	}
	if !strings.Contains(got, "token too long") {
		t.Errorf("want scanner error text, got %q", got)
	}
}

func TestReadRecordsFileState(t *testing.T) {
	dir := t.TempDir()
	state := NewFileState()
	r := &Read{CWD: dir, State: state}
	full := write(t, dir, "a.txt", "x\n")

	info, _ := os.Stat(full)
	if err := state.verify(full, info); err == nil {
		t.Fatal("unread file must not verify")
	}
	run(t, r, `{"path":"a.txt"}`)
	if err := state.verify(full, info); err != nil {
		t.Errorf("read must record the file: %v", err)
	}
}

func TestReadStripsBOMFromFirstLine(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, "bom.txt", utf8BOM+"first\nsecond\n")
	got, _ := run(t, r, `{"path":"bom.txt"}`)
	if strings.Contains(got, utf8BOM) {
		t.Errorf("BOM must not reach the model: %q", got)
	}
	if !strings.Contains(got, "     1\tfirst") {
		t.Errorf("got %q", got)
	}
}

func TestReadSpecsLetPathRulesApply(t *testing.T) {
	r, dir := newRead(t)
	write(t, dir, ".env", "SECRET=1\n")
	specs := r.ReviewSpecs(json.RawMessage(`{"path":".env"}`))
	d := permission.Evaluate(permission.Request{
		Tool:  r.Name(),
		Specs: specs,
		Tier:  permission.TierRead,
		Mode:  permission.ModeBypass,
		Rules: permission.Rules{Deny: []permission.Rule{{Tool: "Read", Spec: "**/.env"}}},
	})
	if d.Behavior != permission.Deny {
		t.Errorf("Read(**/.env) must stop reading .env, got %v (specs %+v)", d.Behavior, specs)
	}
}

func TestResolveRejectsSymlinkEscapeForNewFile(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolve([]string{root}, filepath.Join("link", "new", "file.txt")); err == nil {
		t.Error("a new file under a symlink that leaves the workspace must be rejected")
	}
	if _, err := resolve([]string{root}, filepath.Join("fresh", "file.txt")); err != nil {
		t.Errorf("a new file in a new plain directory must be allowed: %v", err)
	}
}

func TestRealPathWalksToNearestExistingAncestor(t *testing.T) {
	root := t.TempDir()
	got := realPath(filepath.Join(root, "new", "deeper", "a.txt"))
	want := filepath.Join(realPath(root), "new", "deeper", "a.txt")
	if got != want {
		t.Errorf("realPath = %q, want %q", got, want)
	}
}

func TestBashFollowsTheRunContextNotItsConstructionDirectory(t *testing.T) {
	parent := t.TempDir()
	child := t.TempDir()
	if err := os.WriteFile(filepath.Join(child, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := &Bash{CWD: parent, Shell: Shell{Name: shellBash, Path: "bash"}}
	tc := &fakeCtx{cwd: child}
	roots := runRoots(b.CWD, b.Workspaces, tc)
	if len(roots) != 1 || roots[0] != child {
		t.Fatalf("the run context must win over the construction directory, got %v", roots)
	}
	ws := &fakeCtx{cwd: parent, workspaces: []string{child}}
	if roots := runRoots(b.CWD, b.Workspaces, ws); len(roots) != 1 || roots[0] != child {
		t.Fatalf("the run context's workspaces must win, got %v", roots)
	}
	if roots := runRoots(b.CWD, b.Workspaces, nil); len(roots) != 1 || roots[0] != parent {
		t.Fatalf("without a run context the construction directory still applies, got %v", roots)
	}
}
