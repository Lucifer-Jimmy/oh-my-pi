package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type turn struct {
	text string
	err  error
}

type scripted struct {
	turns []turn
	calls int
}

func (s *scripted) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		t := s.turns[s.calls]
		s.calls++
		if t.err != nil {
			yield(llm.StreamEvent{}, t.err)
			return
		}
		for _, ev := range []llm.StreamEvent{
			{Type: llm.SEMessageStart},
			{Type: llm.SEBlockStart, Block: llm.ContentBlock{Type: llm.BlockText}},
			{Type: llm.SETextDelta, Text: t.text},
			{Type: llm.SEBlockStop},
			{Type: llm.SEMessageDelta, StopReason: llm.StopReasonStop},
			{Type: llm.SEMessageStop},
		} {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func printWith(ctx context.Context, s *scripted, prompts ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	loop := agent.NewLoop(agent.NewRegistry(), agent.RunDeps{CallModel: s.call}, nil, agent.Options{})
	code := runPrint(ctx, agent.RunnerFunc(loop.Run), session.NewMemory(nil), prompts, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRunPrintCompleted(t *testing.T) {
	s := &scripted{turns: []turn{{text: "first"}, {text: "second"}}}
	code, out, errOut := printWith(context.Background(), s, "a", "b")
	if code != 0 || out != "second\n" || errOut != "Working...\n" || s.calls != 2 {
		t.Errorf("code=%d out=%q err=%q calls=%d", code, out, errOut, s.calls)
	}
}

func TestRunPrintErrorStopsEarly(t *testing.T) {
	s := &scripted{turns: []turn{{err: errors.New("boom")}, {text: "never"}}}
	code, out, errOut := printWith(context.Background(), s, "a", "b")
	if code != 1 || out != "" || !strings.Contains(errOut, "boom") || s.calls != 1 {
		t.Errorf("code=%d out=%q err=%q calls=%d", code, out, errOut, s.calls)
	}
}

func TestRunPrintInterruptedIsQuiet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// 先取消 ctx，模拟用户按了中断键
	cancel()
	code, out, errOut := printWith(ctx, &scripted{}, "a")
	if code != 1 || out != "" || errOut != "Working...\n" {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestSanitize(t *testing.T) {
	in := "\x1b[31mred\x1b[0m \x1b]52;c;aGk=\x07ok\r\nnext\tline\x00\x7f\u0085"
	if got, want := sanitize(in), "red ok\nnext\tline"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildPrompts(t *testing.T) {
	cases := []struct {
		piped string
		args  []string
		want  []string
	}{
		{"", []string{"a", "b"}, []string{"a", "b"}},
		{"code", nil, []string{"code"}},
		{"code", []string{"q", "b"}, []string{"code\nq", "b"}},
	}
	for _, c := range cases {
		if got := buildPrompts(c.piped, c.args); !slices.Equal(got, c.want) {
			t.Errorf("buildPrompts(%q, %q) = %q, want %q", c.piped, c.args, got, c.want)
		}
	}
}

func TestReadPiped(t *testing.T) {
	if got, _ := readPiped(strings.NewReader(" \n\t"), io.Discard); got != "" {
		t.Errorf("blank stdin should count as none, got %q", got)
	}
	if got, _ := readPiped(strings.NewReader("data\n"), io.Discard); got != "data\n" {
		t.Errorf("got %q", got)
	}
}
