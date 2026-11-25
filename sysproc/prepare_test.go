package sysproc

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestPrepareSetsThePlatformAttributes(t *testing.T) {
	cmd := exec.Command("echo")
	Prepare(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("Prepare must set SysProcAttr")
	}
	if cmd.Cancel == nil {
		t.Fatal("Prepare must set Cancel so a cancelled context kills the tree")
	}
}

func TestCancelKillsTheProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("covered by the bash tree-kill acceptance test on Windows")
	}
	sh := "/bin/sh"
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, sh, "-c", "sleep 30 & wait")
	Prepare(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	start := time.Now()
	cancel()
	err := cmd.Wait()
	if err == nil {
		t.Fatal("a cancelled command must not exit successfully")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s: the child outlived the shell, so the process group was not killed", elapsed)
	}
}
