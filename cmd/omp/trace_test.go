package main

import (
	"context"
	"encoding/json"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/trace"
)

type napTool struct{}

func (napTool) Name() string            { return "nap" }
func (napTool) Description() string     { return "sleeps briefly" }
func (napTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (napTool) Execute(context.Context, agent.ToolContext, json.RawMessage) agent.ToolResult {
	time.Sleep(30 * time.Millisecond)
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("rested")}}
}

func TestTraceSummaryMeasuresARealLoop(t *testing.T) {
	reg := agent.NewRegistry()
	reg.Register(napTool{})
	m := &stepModel{steps: []step{{id: "n1", tool: "nap", input: map[string]any{}}, {text: "done"}}}
	loop := agent.NewLoop(reg, agent.RunDeps{CallModel: m.call}, nil, agent.Options{})
	path := filepath.Join(t.TempDir(), "trace.jsonl")
	rec, err := trace.New(path, trace.Meta{Window: func() int { return 100_000 }})
	if err != nil {
		t.Fatalf("trace.New: %v", err)
	}
	collect(traceRunner(rec, loop))
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var sum struct {
		Type     string `json:"type"`
		Turns    int    `json:"turns"`
		Requests int    `json:"requests"`
		Tools    map[string]struct {
			Calls   int   `json:"calls"`
			TotalMs int64 `json:"total_ms"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &sum); err != nil {
		t.Fatal(err)
	}
	if sum.Type != "summary" || sum.Turns != 1 || sum.Requests != 2 {
		t.Fatalf("one tool round and two model calls: %+v", sum)
	}
	if nap := sum.Tools["nap"]; nap.Calls != 1 || nap.TotalMs < 30 {
		t.Errorf("the kernel's measured tool time must reach the summary: %+v", sum.Tools)
	}
}

func fakeRunner(evs ...agent.Event) agent.Runner {
	return agent.RunnerFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			for _, ev := range evs {
				if !yield(ev, nil) {
					return
				}
			}
		}
	})
}

func collect(run agent.Runner) []agent.Event {
	var out []agent.Event
	for ev := range run.Run(context.Background(), session.NewMemory(nil), "hi") {
		out = append(out, ev)
	}
	return out
}

func TestTraceRunnerPassesEventsThrough(t *testing.T) {
	path := t.TempDir() + "/trace.jsonl"
	rec, err := trace.New(path, trace.Meta{})
	if err != nil {
		t.Fatalf("trace.New: %v", err)
	}
	evs := []agent.Event{
		{Kind: agent.EventTextDelta, Text: "a"},
		{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted}},
	}
	got := collect(traceRunner(rec, fakeRunner(evs...)))
	if len(got) != 2 || got[0].Text != "a" || got[1].Kind != agent.EventResult {
		t.Fatalf("traceRunner must forward every event unchanged, got %+v", got)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if rec.Err() != nil {
		t.Fatalf("recorder reported an error: %v", rec.Err())
	}
}

func TestTraceRunnerNilRecorderIsTheRunner(t *testing.T) {
	evs := []agent.Event{{Kind: agent.EventTextDelta, Text: "x"}}
	run := traceRunner(nil, fakeRunner(evs...))
	if got := collect(run); len(got) != 1 {
		t.Fatalf("nil recorder must pass the runner through, got %d events", len(got))
	}
}

func TestTraceCompactForwardsAndToleratesNil(t *testing.T) {
	if traceCompact(nil, nil) != nil {
		t.Fatalf("nil compact must stay nil")
	}
	path := t.TempDir() + "/trace.jsonl"
	rec, err := trace.New(path, trace.Meta{})
	if err != nil {
		t.Fatalf("trace.New: %v", err)
	}
	defer rec.Close()
	compact := func(ctx context.Context, sess session.Session, instructions string) iter.Seq2[agent.Event, error] {
		return func(yield func(agent.Event, error) bool) {
			yield(agent.Event{Kind: agent.EventCompaction}, nil)
		}
	}
	var n int
	for range traceCompact(rec, compact)(context.Background(), session.NewMemory(nil), "") {
		n++
	}
	if n != 1 {
		t.Fatalf("traceCompact must forward events, got %d", n)
	}
}
