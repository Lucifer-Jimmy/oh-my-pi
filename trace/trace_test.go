package trace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func readLines(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("line is not JSON: %v: %q", err, line)
		}
		out = append(out, m)
	}
	return out
}

func TestRecorderWritesHeaderEventsSummary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "run.jsonl")
	r, err := New(path, Meta{CWD: "C:/repo", Model: "m"})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	fixed := time.Unix(1700000000, 0)
	r.now = func() time.Time { return fixed }
	r.Write(agent.Event{Kind: agent.EventTextDelta, Text: "hi"})
	r.Write(agent.Event{Kind: agent.EventResult, Result: &agent.Result{
		Reason: agent.ReasonCompleted,
		Turns:  2,
		Usage:  llm.Usage{InputTokens: 10, OutputTokens: 3},
	}})
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	lines := readLines(t, path)
	if len(lines) != 4 {
		t.Fatalf("want 4 lines (header, 2 events, summary), got %d", len(lines))
	}
	if lines[0]["type"] != "header" || lines[0]["version"].(float64) != Version {
		t.Fatalf("header wrong: %v", lines[0])
	}
	if lines[0]["cwd"] != "C:/repo" || lines[0]["model"] != "m" {
		t.Fatalf("header meta wrong: %v", lines[0])
	}
	if lines[1]["type"] != "event" {
		t.Fatalf("event line wrong: %v", lines[1])
	}
	sum := lines[3]
	if sum["type"] != "summary" {
		t.Fatalf("last line is not summary: %v", sum)
	}
	if sum["reason"] != "completed" || sum["turns"].(float64) != 2 || sum["events"].(float64) != 2 {
		t.Fatalf("summary wrong: %v", sum)
	}
	usage := sum["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 10 || usage["output_tokens"].(float64) != 3 {
		t.Fatalf("usage wrong: %v", usage)
	}
	if int64(sum["finished_at"].(float64)) != fixed.UnixMilli() {
		t.Fatalf("finished_at should come from the injected clock: %v", sum["finished_at"])
	}
}

func TestWriteAfterCloseIsIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	r, err := New(path, Meta{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	r.Write(agent.Event{Kind: agent.EventTextDelta, Text: "late"})
	if err := r.Close(); err != nil {
		t.Fatalf("second close must be idempotent: %v", err)
	}
	lines := readLines(t, path)
	for _, l := range lines[1:] {
		if l["type"] == "event" {
			t.Fatalf("an event was written after Close: %v", l)
		}
	}
}

func TestBadPathReturnsErrorNotPanic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "as-file", "x.jsonl")
	if err := os.WriteFile(filepath.Join(t.TempDir(), "unused"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Dir(path), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path, Meta{}); err == nil {
		t.Fatalf("want an error when the trace directory cannot be created")
	}
}

func TestSummaryFoldsToolsRequestsAndContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	r, err := New(path, Meta{Window: func() int { return 1000 }})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	result := func(id string, isErr bool, ms int64) agent.Event {
		return agent.Event{Kind: agent.EventToolResult, ToolUseID: id, DurationMs: ms, ToolResult: &agent.ToolResult{IsError: isErr}}
	}
	for _, ev := range []agent.Event{
		{Kind: agent.EventToolUse, ToolUseID: "a1", ToolName: "read"},
		{Kind: agent.EventToolUse, ToolUseID: "b1", ToolName: "bash"},
		result("a1", false, 30),
		result("b1", true, 70),
		{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 300, OutputTokens: 50}, StopReason: llm.StopReasonToolUse, DurationMs: 400},
		{Kind: agent.EventToolUse, ToolUseID: "a2", ToolName: "read"},
		result("a2", false, 10),
		{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 500, OutputTokens: 100}, StopReason: llm.StopReasonLength, DurationMs: 200},
		{Kind: agent.EventCompaction, Compaction: &session.Compaction{}},
		{Kind: agent.EventCompaction, Message: "Compaction failed, continuing without it: boom"},
		{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 200, OutputTokens: 20}, StopReason: llm.StopReasonStop, DurationMs: 100},
		{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonCompleted, Turns: 2, Usage: llm.Usage{InputTokens: 1000, OutputTokens: 170}}},
		{Kind: agent.EventResult, Result: &agent.Result{Reason: agent.ReasonAborted, Err: "stopped", Turns: 1, Usage: llm.Usage{InputTokens: 5, OutputTokens: 1}}},
	} {
		r.Write(ev)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	lines := readLines(t, path)
	sum := lines[len(lines)-1]
	num := func(k string) float64 {
		v, _ := sum[k].(float64)
		return v
	}
	if num("requests") != 3 || num("request_ms") != 700 || num("truncations") != 1 || num("compactions") != 1 {
		t.Errorf("model calls, their time, truncations and successful compactions: %v", sum)
	}
	tools, _ := sum["tools"].(map[string]any)
	read, _ := tools["read"].(map[string]any)
	bash, _ := tools["bash"].(map[string]any)
	if read["calls"] != 2.0 || read["total_ms"] != 40.0 || read["errors"] != nil {
		t.Errorf("read stats wrong: %v", read)
	}
	if bash["calls"] != 1.0 || bash["errors"] != 1.0 || bash["total_ms"] != 70.0 {
		t.Errorf("bash stats wrong: %v", bash)
	}
	if num("peak_context_tokens") != 600 || num("context_window") != 1000 || num("peak_context_utilization") != 0.6 {
		t.Errorf("peak context and its share of the window: %v", sum)
	}
	if num("turns") != 3 || sum["reason"] != "aborted" || sum["err"] != "stopped" {
		t.Errorf("turns add up across runs and the last run decides the reason: %v", sum)
	}
	if usage, _ := sum["usage"].(map[string]any); usage["input_tokens"] != 1005.0 || usage["output_tokens"] != 171.0 {
		t.Errorf("usage adds up across runs: %v", usage)
	}
}

func TestSummaryOmitsUtilizationWithoutAWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	r, err := New(path, Meta{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	r.Write(agent.Event{Kind: agent.EventUsage, Usage: &llm.Usage{InputTokens: 300}})
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	lines := readLines(t, path)
	sum := lines[len(lines)-1]
	if sum["peak_context_tokens"] != 300.0 {
		t.Errorf("the peak is known without a window: %v", sum)
	}
	if _, ok := sum["peak_context_utilization"]; ok {
		t.Errorf("no window, no share: %v", sum)
	}
	if _, ok := sum["tools"]; ok {
		t.Errorf("a run without tools has no tools entry: %v", sum)
	}
}

func TestSummaryDefaultsWithoutResult(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run.jsonl")
	r, err := New(path, Meta{})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	r.Write(agent.Event{Kind: agent.EventTextDelta, Text: "partial"})
	if err := r.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	sum := readLines(t, path)[2]
	if sum["type"] != "summary" || sum["events"].(float64) != 1 {
		t.Fatalf("summary must still be written: %v", sum)
	}
	if sum["reason"] != nil && sum["reason"] != "" {
		t.Fatalf("reason must be empty without a result: %v", sum["reason"])
	}
}
