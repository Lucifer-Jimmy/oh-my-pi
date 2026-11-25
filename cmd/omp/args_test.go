package main

import (
	"errors"
	"io"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/spf13/pflag"
)

func TestParseArgs(t *testing.T) {
	c, err := parseArgs([]string{"-p", "--model", "m", "hi", "there"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !c.print || c.api != "" || c.model != "m" || len(c.prompts) != 2 {
		t.Errorf("got %+v", c)
	}
	c, err = parseArgs([]string{"--api", "openai-completions", "--model", "m", "hi"}, io.Discard)
	if err != nil || c.api != llm.ProtocolOpenAICompletions {
		t.Errorf("explicit --api: %+v, %v", c, err)
	}
	c, err = parseArgs([]string{"-p", "--model", "m", "--", "-1 is negative"}, io.Discard)
	if err != nil || len(c.prompts) != 1 || c.prompts[0] != "-1 is negative" {
		t.Errorf("-- before dash prompt: %+v, %v", c, err)
	}
}

func TestParseArgsErrors(t *testing.T) {
	for _, args := range [][]string{
		{"--model", "m", "--api", "gemini", "hi"},
		{"--model", "m", "--thinking", "huge", "hi"},
		{"--model", "m", "--max-tokens", "-1", "hi"},
		{"--model", "m", "--max-turns", "-1", "hi"},
		// 命令行不能传 key，只能写在配置文件里
		{"--model", "m", "--api-key", "k", "hi"},
		{"--nope"},
	} {
		if _, err := parseArgs(args, io.Discard); err == nil {
			t.Errorf("%q: want error", args)
		}
	}
}

func TestParseArgsInterspersed(t *testing.T) {
	c, err := parseArgs([]string{"-p", "--model", "m", "hi", "--thinking", "high"}, io.Discard)
	if err != nil {
		t.Fatalf("pflag must allow flags after positionals: %v", err)
	}
	if c.thinking != llm.ThinkingHigh || len(c.prompts) != 1 || c.prompts[0] != "hi" {
		t.Errorf("got %+v", c)
	}
}

func TestParseArgsHelp(t *testing.T) {
	_, err := parseArgs([]string{"--help"}, io.Discard)
	if !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("want pflag.ErrHelp, got %v", err)
	}
}

func TestApprovalModeDefaultsToAcceptEdits(t *testing.T) {
	c, err := parseArgs([]string{"-p", "--model", "m", "hi"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if got := c.approvalMode(); got != permission.ModeAcceptEdits {
		t.Errorf("print mode must default to acceptEdits, got %q", got)
	}
	c, err = parseArgs([]string{"-p", "--model", "m", "--approval", "plan", "hi"}, io.Discard)
	if err != nil || c.approvalMode() != permission.ModePlan {
		t.Errorf("explicit --approval must win: %v %v", c.approvalMode(), err)
	}
	if _, err := parseArgs([]string{"-p", "--model", "m", "--approval", "yolo", "hi"}, io.Discard); err == nil {
		t.Error("unknown approval mode must be rejected")
	}
}
