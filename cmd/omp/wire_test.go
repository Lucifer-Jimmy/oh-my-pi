package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func decodeFrames(t *testing.T, out []byte) []wireFrame {
	t.Helper()
	var frames []wireFrame
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var f wireFrame
		if err := json.Unmarshal(line, &f); err != nil {
			t.Fatalf("frame is not JSON: %v\n%s", err, line)
		}
		if f.V != wireVersion {
			t.Fatalf("frame has version %d, want %d", f.V, wireVersion)
		}
		frames = append(frames, f)
	}
	return frames
}

func loopRunner(m *stepModel) agent.Runner {
	return agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		loop := agent.NewLoop(agent.NewRegistry(), agent.RunDeps{CallModel: m.call}, nil, agent.Options{})
		return loop.Run(ctx, sess, input)
	})
}

func TestJSONModeEmitsTheWholeRunAsFrames(t *testing.T) {
	m := &stepModel{steps: []step{
		{id: "t1", tool: "todo", input: map[string]any{"items": []map[string]any{{"content": "do it", "status": "in_progress"}}}},
		{text: "all done"},
	}}
	var out, errb bytes.Buffer
	code := runJSON(context.Background(), loopRunner(m), session.NewMemory(nil), []string{"go"}, t.TempDir(), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	frames := decodeFrames(t, out.Bytes())
	if len(frames) < 4 {
		t.Fatalf("expected ready, tool, tool result and result frames, got %d", len(frames))
	}
	if frames[0].Type != "ready" {
		t.Fatalf("the first frame announces the run, got %q", frames[0].Type)
	}
	sawTool, sawResult := false, false
	for _, f := range frames {
		if f.Type != "event" || f.Event == nil {
			continue
		}
		switch f.Event.Kind {
		case agent.EventToolUse:
			sawTool = true
			if f.Event.ToolName != "todo" || len(f.Event.ToolInput) == 0 {
				t.Fatalf("the tool frame must carry name and input: %+v", f.Event)
			}
		case agent.EventResult:
			sawResult = true
			if f.Event.Reason != agent.ReasonCompleted || f.Event.Text != "all done" {
				t.Fatalf("the result frame must carry the answer: %+v", f.Event)
			}
			if f.State == nil {
				t.Fatal("the last frame carries a state snapshot for a GUI")
			}
		}
	}
	if !sawTool || !sawResult {
		t.Fatalf("tool=%v result=%v", sawTool, sawResult)
	}
}

func TestWireCarriesTimingsStopReasonsAndProgress(t *testing.T) {
	frame := func(ev agent.Event) map[string]any {
		t.Helper()
		b, err := json.Marshal(wireOf(ev))
		if err != nil {
			t.Fatal(err)
		}
		var f map[string]any
		if err := json.Unmarshal(b, &f); err != nil {
			t.Fatal(err)
		}
		body, _ := f["event"].(map[string]any)
		return body
	}
	res := frame(agent.Event{Kind: agent.EventToolResult, ToolUseID: "t1", DurationMs: 2300, ToolResult: &agent.ToolResult{}})
	if res["duration_ms"] != 2300.0 {
		t.Errorf("a tool result frame carries the measured time: %v", res)
	}
	use := frame(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{OutputTokens: 9}, StopReason: llm.StopReasonLength, DurationMs: 800})
	if use["stop_reason"] != "length" || use["duration_ms"] != 800.0 {
		t.Errorf("a usage frame says why the response stopped and how long the call took: %v", use)
	}
	prog := frame(agent.Event{Kind: agent.EventProgress, ToolUseID: "t1", ToolName: "grep", ToolInput: json.RawMessage(`{"pattern":"x"}`), Usage: &llm.Usage{InputTokens: 40}})
	if prog["tool_use_id"] != "t1" || prog["tool_name"] != "grep" || prog["tool_input"] == nil || prog["usage"] == nil {
		t.Errorf("a progress frame names the card, the child's tool and its tokens: %v", prog)
	}
	text := frame(agent.Event{Kind: agent.EventTextDelta, Text: "hi"})
	if _, ok := text["duration_ms"]; ok {
		t.Errorf("fields without a value stay off the wire: %v", text)
	}
	if _, ok := text["stop_reason"]; ok {
		t.Errorf("fields without a value stay off the wire: %v", text)
	}
}

func TestRPCRoundTripAnswersPromptsAndQuits(t *testing.T) {
	m := &stepModel{steps: []step{{text: "first"}, {text: "first"}}}
	host := newRPCHost()
	inR, inW := io.Pipe()
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- runRPC(context.Background(), host, loopRunner(m), session.NewMemory(nil), nil, inR, &out, &errb)
	}()
	send := func(v any) {
		b, _ := json.Marshal(v)
		inW.Write(append(b, '\n'))
	}
	send(map[string]any{"type": "prompt", "text": "one"})
	send(map[string]any{"type": "prompt", "text": "two"})
	send(map[string]any{"type": "quit"})
	inW.Close()
	if code := <-done; code != 0 {
		t.Fatalf("exit %d stdout: %s stderr: %s", code, out.String(), errb.String())
	}
	frames := decodeFrames(t, out.Bytes())
	texts := 0
	for _, f := range frames {
		if f.Type == "event" && f.Event != nil && f.Event.Kind == agent.EventTextDelta {
			texts++
		}
	}
	if texts == 0 {
		t.Fatalf("the prompts must produce text frames:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"first"`) && !strings.Contains(out.String(), `"first`) {
		t.Fatalf("the first answer must reach the wire:\n%s", out.String())
	}
}

func TestRPCQuitDuringARunEndsCleanly(t *testing.T) {
	m := &stepModel{steps: []step{{text: "slow answer"}, {text: "slow answer"}}}
	host := newRPCHost()
	inR, inW := io.Pipe()
	out := &syncBuffer{}
	done := make(chan int, 1)
	go func() {
		done <- runRPC(context.Background(), host, loopRunner(m), session.NewMemory(nil), nil, inR, out, io.Discard)
	}()
	b, _ := json.Marshal(map[string]any{"type": "prompt", "text": "start"})
	inW.Write(append(b, '\n'))
	q, _ := json.Marshal(map[string]any{"type": "quit"})
	inW.Write(append(q, '\n'))
	inW.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("quit during a run must not hang:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"type":"bye"`) {
		t.Fatalf("the loop must say goodbye:\n%s", out.String())
	}
}

func TestRPCRunsEveryCommandLinePrompt(t *testing.T) {
	m := &stepModel{steps: []step{{text: "first"}, {text: "second"}}}
	var out, errb bytes.Buffer
	code := runRPC(context.Background(), newRPCHost(), loopRunner(m), session.NewMemory(nil), []string{"one", "two"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var answers []string
	idle := 0
	for _, f := range decodeFrames(t, out.Bytes()) {
		if f.Type == "idle" {
			idle++
		}
		if f.Type == "event" && f.Event != nil && f.Event.Kind == agent.EventResult {
			answers = append(answers, f.Event.Text)
		}
	}
	if len(answers) != 2 || answers[0] != "first" || answers[1] != "second" {
		t.Fatalf("every prompt on the command line runs, in order, got %v:\n%s", answers, out.String())
	}
	if idle != 1 {
		t.Fatalf("idle is announced once, after the last queued prompt, got %d:\n%s", idle, out.String())
	}
}

func TestRPCStopsTheQueueAfterARunThatDidNotComplete(t *testing.T) {
	var ran []string
	run := agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		ran = append(ran, input)
		return func(yield func(agent.Event, error) bool) {
			yield(agent.Event{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonMaxTurns}}, nil)
		}
	})
	var out, errb bytes.Buffer
	if code := runRPC(context.Background(), newRPCHost(), run, session.NewMemory(nil), []string{"one", "two"}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("a run that did not complete exits 1, got %d", code)
	}
	if len(ran) != 1 {
		t.Fatalf("a run that did not complete stops the queue, as print and json modes do, ran %v", ran)
	}
}
