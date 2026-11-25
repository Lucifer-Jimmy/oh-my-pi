package verifier

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func TestCommandPasses(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	res, err := Command{Command: "exit 0", Dir: dir}.Verify(ctx, session.NewMemory(nil))
	if err != nil || !res.Passed {
		t.Fatalf("res = %+v err = %v", res, err)
	}
}

func TestCommandReportsTheOutput(t *testing.T) {
	ctx := context.Background()
	sh := commandShell(t)
	res, err := Command{Command: failCommand(sh), Dir: t.TempDir(), Shell: sh}.Verify(ctx, session.NewMemory(nil))
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatal("a failing command must not pass")
	}
	if !strings.Contains(res.Feedback, "boom") {
		t.Errorf("the feedback must carry the command output: %q", res.Feedback)
	}
}

func TestCommandTimesOut(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	sh := commandShell(t)
	start := time.Now()
	res, err := Command{Command: sleepCommand(sh, 5), Dir: t.TempDir(), Shell: sh}.Verify(ctx, session.NewMemory(nil))
	if err == nil {
		t.Fatalf("a cancelled context must surface as an error, got res = %+v", res)
	}
	if time.Since(start) > 3*time.Second {
		t.Error("the timeout was not enforced")
	}
}

func TestCommandOutputIsBounded(t *testing.T) {
	ctx := context.Background()
	sh := commandShell(t)
	res, err := Command{Command: noisyCommand(sh), Dir: t.TempDir(), Shell: sh}.Verify(ctx, session.NewMemory(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Feedback) > outputCap+200 {
		t.Errorf("feedback is %d bytes, the cap is %d", len(res.Feedback), outputCap)
	}
	if !strings.Contains(res.Feedback, "trimmed") {
		t.Errorf("the cut must be visible: %q", res.Feedback[len(res.Feedback)-100:])
	}
}

func TestCommandAbortedByContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sh := commandShell(t)
	if _, err := (Command{Command: sleepCommand(sh, 5), Dir: t.TempDir(), Shell: sh}).Verify(ctx, session.NewMemory(nil)); err == nil {
		t.Fatal("a cancelled run reports the context error")
	}
}
