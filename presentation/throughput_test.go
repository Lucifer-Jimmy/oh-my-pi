package presentation

import (
	"fmt"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

func transcriptEvents(lines int) []agent.Event {
	var evs []agent.Event
	var seq int64
	emit := func(e agent.Event) {
		seq++
		e.Seq = seq
		e.Timestamp = seq
		evs = append(evs, e)
	}
	for i := 0; i < lines; i++ {
		emit(agent.Event{Kind: agent.EventTextDelta, Text: fmt.Sprintf("line %d of a long transcript\n", i)})
		if i%50 == 0 {
			id := fmt.Sprintf("call-%d", i)
			emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: id, ToolName: "read"})
			emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: id, ToolResult: &agent.ToolResult{}})
			emit(agent.Event{Kind: agent.EventUsage})
		}
	}
	return evs
}

func replay(evs []agent.Event) State {
	var s State
	s.Submit("go", 0)
	for _, ev := range evs {
		s.Apply(ev)
	}
	return s
}

func TestReducerKeepsEveryEventAtTenThousandLines(t *testing.T) {
	evs := transcriptEvents(10_000)
	s := replay(evs)
	if s.lastSeq != int64(len(evs)) {
		t.Fatalf("reducer dropped events: last seq %d, want %d", s.lastSeq, len(evs))
	}
	var text int
	for _, it := range s.Items {
		text += len(it.Text)
	}
	want := len("go")
	for _, ev := range evs {
		want += len(ev.Text)
	}
	if text != want {
		t.Fatalf("text lost in the reducer: %d bytes kept, want %d", text, want)
	}
	wantItems := 2 + 2*(10_000/50)
	if len(s.Items) != wantItems {
		t.Fatalf("item count = %d, want %d (prompt, then a tool card and a text segment per 50 lines)", len(s.Items), wantItems)
	}
}

func TestReducerThroughputBudget(t *testing.T) {
	const perSecond = 200
	evs := transcriptEvents(10_000)
	budget := len(evs) / perSecond
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			replay(evs)
		}
	})
	perRun := result.NsPerOp()
	gotPerSecond := float64(len(evs)) / (float64(perRun) / 1e9)
	t.Logf("reducer: %d events in %.2f ms (%.0f events/s), budget %d events/s",
		len(evs), float64(perRun)/1e6, gotPerSecond, perSecond)
	if gotPerSecond < perSecond {
		t.Fatalf("reducer is slower than the requirement: %.0f < %d events/s", gotPerSecond, perSecond)
	}
	if result.Extra["allocs/op"] > 0 && result.AllocsPerOp()/int64(len(evs)) > 4 {
		t.Errorf("allocations per event above 4: %d", result.AllocsPerOp()/int64(len(evs)))
	}
	t.Logf("a full 10k-line transcript replays in %s (budget %ds)", result.T, budget)
}

func BenchmarkApplyTranscript(b *testing.B) {
	evs := transcriptEvents(10_000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		replay(evs)
	}
}
