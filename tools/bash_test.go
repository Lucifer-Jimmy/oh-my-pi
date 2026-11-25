package tools

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

func newBash(t *testing.T) (*Bash, string) {
	t.Helper()
	dir := t.TempDir()
	return &Bash{CWD: dir}, dir
}

func runBash(t *testing.T, b *Bash, args string) (string, bool) {
	t.Helper()
	res := b.Execute(context.Background(), &fakeCtx{cwd: b.CWD}, json.RawMessage(args))
	var sb strings.Builder
	for _, c := range res.Content {
		sb.WriteString(c.Text)
	}
	return sb.String(), res.IsError
}

func bashApproval(b *Bash, cmd string) agent.Approval {
	raw, _ := json.Marshal(map[string]string{"command": cmd})
	return b.Approval(raw)
}

func TestBashEcho(t *testing.T) {
	b, _ := newBash(t)
	got, isErr := runBash(t, b, `{"command":"echo hello-omp"}`)
	if isErr || !strings.Contains(got, "hello-omp") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashNoOutput(t *testing.T) {
	b, _ := newBash(t)
	got, isErr := runBash(t, b, `{"command":"cd ."}`)
	if isErr || got != "(no output)" {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashNonZeroExitIsError(t *testing.T) {
	b, _ := newBash(t)
	got, isErr := runBash(t, b, `{"command":"exit 3"}`)
	if !isErr || !strings.Contains(got, "code 3") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashMissingCommand(t *testing.T) {
	b, _ := newBash(t)
	if got, isErr := runBash(t, b, `{}`); !isErr || !strings.Contains(got, "command is required") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashInvalidJSON(t *testing.T) {
	b, _ := newBash(t)
	if got, isErr := runBash(t, b, `{"command":`); !isErr || !strings.Contains(got, "invalid arguments") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashDirOutsideWorkspace(t *testing.T) {
	b, _ := newBash(t)
	if got, isErr := runBash(t, b, `{"command":"echo x","dir":".."}`); !isErr || !strings.Contains(got, "outside the workspace") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashRunsInSubdirectory(t *testing.T) {
	b, dir := newBash(t)
	mkfile(t, dir, "sub/marker.txt", "content-from-sub\n")
	got, isErr := runBash(t, b, `{"command":"cat marker.txt","dir":"sub"}`)
	if isErr || !strings.Contains(got, "content-from-sub") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}

func TestBashCancellationIsReported(t *testing.T) {
	b, _ := newBash(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if res := b.Execute(ctx, &fakeCtx{cwd: b.CWD}, json.RawMessage(`{"command":"echo hi"}`)); !res.IsError {
		t.Error("a cancelled call must report an error")
	}
}

func TestBashTimeoutKillsProcessTree(t *testing.T) {
	old := bashWaitDelay
	bashWaitDelay = 10 * time.Second
	t.Cleanup(func() { bashWaitDelay = old })

	b, _ := newBash(t)
	start := time.Now()
	got, isErr := runBash(t, b, `{"command":"sleep 30","timeout_ms":100}`)
	elapsed := time.Since(start)
	if !isErr || !strings.Contains(got, "timed out") {
		t.Fatalf("got isErr=%v text=%q", isErr, got)
	}
	if elapsed >= 8*time.Second {
		t.Errorf("took %s: the child outlived the shell and held the pipe until WaitDelay, so the tree was not killed", elapsed)
	}
}

func TestBashApprovalTiers(t *testing.T) {
	b := &Bash{Shell: Shell{Path: "bash", Name: shellGitBash}}
	cases := map[string]agent.Tier{
		"git status":          agent.TierWrite,
		"git log --oneline":   agent.TierWrite,
		"ls -la | grep x":     agent.TierWrite,
		"cat a.txt":           agent.TierWrite,
		"go test ./...":       agent.TierExec,
		"python script.py":    agent.TierExec,
		"npm run build":       agent.TierExec,
		"make":                agent.TierExec,
		"./deploy.sh":         agent.TierExec,
		"./ls":                agent.TierExec,
		"git push":            agent.TierExec,
		"git -c x=y status":   agent.TierExec,
		"git diff --output=f": agent.TierExec,
		"cat a > b":           agent.TierExec,
		"echo $(id)":          agent.TierExec,
		"git status; make":    agent.TierExec,
	}
	for cmd, want := range cases {
		if got := bashApproval(b, cmd).Tier; got != want {
			t.Errorf("%q: tier %q, want %q", cmd, got, want)
		}
	}
}

func TestBashNeverReadTier(t *testing.T) {
	b := &Bash{Shell: Shell{Path: "bash", Name: shellGitBash}}
	for _, cmd := range []string{"ls", "cat a.txt", "git log", "grep x f", "pwd"} {
		if a := bashApproval(b, cmd); a.Tier == agent.TierRead {
			t.Errorf("%q must not be read tier: plan mode would let it run", cmd)
		}
	}
}

func TestBashCmdShellNeverDowngrades(t *testing.T) {
	b := &Bash{Shell: Shell{Path: "cmd", Name: shellCmd}}
	if a := bashApproval(b, "dir"); a.Tier != agent.TierExec {
		t.Errorf("cmd.exe searches the current directory first, so nothing may be downgraded, got %q", a.Tier)
	}
}

func TestBashDangerousCommandsForcePrompt(t *testing.T) {
	b, _ := newBash(t)
	for _, cmd := range []string{"rm -rf /", "git reset --hard HEAD", "git push --force", "del /s important", "shutdown /s"} {
		a := bashApproval(b, cmd)
		if !a.ForcePrompt || a.Tier != agent.TierExec {
			t.Errorf("%q: got %+v", cmd, a)
		}
	}
}

func TestBashUnreadableApprovalIsExec(t *testing.T) {
	b, _ := newBash(t)
	if a := b.Approval(json.RawMessage(`not json`)); a.Tier != agent.TierExec {
		t.Errorf("unreadable input must fail closed, got %+v", a)
	}
}

func TestBashReviewSpecsSplits(t *testing.T) {
	b, _ := newBash(t)
	if specs := b.ReviewSpecs(json.RawMessage(`{"command":"git status; rm -rf /"}`)); len(specs) != 2 {
		t.Fatalf("want 2 specs, got %+v", specs)
	}
}

func TestBashExclusive(t *testing.T) {
	b, _ := newBash(t)
	if b.IsReadOnly() || b.IsConcurrencySafe(nil) {
		t.Error("bash must be neither read-only nor concurrency-safe")
	}
}

func TestBashDescriptionNamesShell(t *testing.T) {
	b := &Bash{Shell: Shell{Path: "powershell.exe", Name: shellPowerShell}}
	if !strings.Contains(b.Description(), "&& is not supported") {
		t.Errorf("Windows PowerShell must be named with its && limitation: %q", b.Description())
	}
	g := &Bash{Shell: Shell{Path: "bash.exe", Name: shellGitBash}}
	if !strings.Contains(g.Description(), "Git Bash") {
		t.Errorf("shell name missing: %q", g.Description())
	}
}

func TestCapWriterBoundsMemory(t *testing.T) {
	w := &capWriter{}
	chunk := []byte(strings.Repeat("x", 64*1024))
	total := 0
	for range 80 {
		w.Write(chunk)
		total += len(chunk)
		if len(w.head)+len(w.tail) > bashHeadKeep+2*bashTailKeep+len(chunk) {
			t.Fatalf("buffer grew to %d bytes", len(w.head)+len(w.tail))
		}
	}
	out := w.String()
	if !strings.Contains(out, "bytes elided") {
		t.Fatal("missing elision marker")
	}
	if len(out) > bashHeadKeep+bashTailKeep+64 {
		t.Errorf("output not capped: %d bytes", len(out))
	}
}

func TestCapWriterKeepsUTF8Valid(t *testing.T) {
	w := &capWriter{}
	w.Write([]byte(strings.Repeat("a", bashHeadKeep-1) + "中"))
	w.Write([]byte(strings.Repeat("b", 3*bashTailKeep)))
	w.Write([]byte("文" + strings.Repeat("c", bashTailKeep-1)))
	if out := w.String(); !utf8.ValidString(out) {
		t.Error("cut points must land on rune boundaries")
	}
}

func TestCapWriterShortPassesThrough(t *testing.T) {
	w := &capWriter{}
	w.Write([]byte("short"))
	if got := w.String(); got != "short" {
		t.Errorf("got %q", got)
	}
}

func fakeLookPath(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

func stubShells(t *testing.T, found map[string]string, exists map[string]bool) {
	t.Helper()
	oldLook, oldExists := shellLookPath, shellExists
	shellLookPath = fakeLookPath(found)
	shellExists = func(p string) bool { return exists[filepath.Clean(p)] }
	t.Cleanup(func() { shellLookPath, shellExists = oldLook, oldExists })
}

func TestResolveShellFindsGitBashThroughGit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only resolution")
	}
	gitExe := filepath.Join(`C:\Program Files\Git`, "cmd", "git.exe")
	bashExe := filepath.Join(`C:\Program Files\Git`, "bin", "bash.exe")
	stubShells(t, map[string]string{
		"git":  gitExe,
		"bash": `C:\Windows\system32\bash.exe`,
	}, map[string]bool{bashExe: true})

	sh := ResolveShell("")
	if sh.Name != shellGitBash || filepath.Clean(sh.Path) != bashExe {
		t.Errorf("want Git Bash next to git.exe, got %+v", sh)
	}
}

func TestResolveShellNeverPicksWSLLauncher(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only resolution")
	}
	stubShells(t, map[string]string{
		"bash":       `C:\Windows\system32\bash.exe`,
		"powershell": `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
	}, nil)

	sh := ResolveShell("")
	if sh.Name != shellPowerShell || !strings.Contains(strings.Join(sh.Args, " "), "-Command") {
		t.Errorf("without Git Bash the fallback is PowerShell, never the WSL bash.exe; got %+v", sh)
	}
}

func TestResolveShellExplicit(t *testing.T) {
	sh := ResolveShell(`C:\tools\pwsh.exe`)
	if sh.Name != shellPwsh || sh.Path != `C:\tools\pwsh.exe` {
		t.Errorf("got %+v", sh)
	}
}

func TestBashPowerShellOutputIsUTF8(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only encoding")
	}
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("powershell not available")
	}
	b := &Bash{CWD: t.TempDir(), Shell: ResolveShell(ps)}
	got, isErr := runBash(t, b, `{"command":"Write-Output '中文输出'"}`)
	if isErr || !strings.Contains(got, "中文输出") {
		t.Errorf("non-ASCII output must survive: isErr=%v text=%q", isErr, got)
	}
}

func TestBashMissingShellIsReported(t *testing.T) {
	b := &Bash{CWD: t.TempDir(), Shell: Shell{Path: filepath.Join(t.TempDir(), "no-such-shell"), Args: []string{"-c"}, Name: "missing"}}
	got, isErr := runBash(t, b, `{"command":"echo x"}`)
	if !isErr || !strings.Contains(got, "could not start shell") {
		t.Errorf("got isErr=%v text=%q", isErr, got)
	}
}
