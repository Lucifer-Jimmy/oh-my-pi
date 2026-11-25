package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type stubCompactor struct {
	calls  []CompactRequest
	view   []llm.Message
	cp     *session.Compaction
	err    error
	skipOn session.Trigger
}

func (s *stubCompactor) Compact(ctx context.Context, req CompactRequest) ([]llm.Message, *session.Compaction, error) {
	s.calls = append(s.calls, req)
	if s.err != nil {
		return nil, nil, s.err
	}
	if req.Trigger == s.skipOn {
		return nil, nil, nil
	}
	cp := session.Compaction{Trigger: req.Trigger, MessageCount: len(req.Request.Messages), ElideBefore: 1, TokensBefore: 999, TokensAfter: 99}
	if s.cp != nil {
		cp = *s.cp
	}
	return s.view, &cp, nil
}

func bigSession(n int) *session.Memory {
	msgs := make([]llm.Message, 0, n)
	for i := 0; i < n; i++ {
		msgs = append(msgs, llm.UserText(strings.Repeat("x", 4000)))
	}
	return session.NewMemory(msgs)
}

func TestLoopCompactsBeforeTheRequest(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{textTurn("done")}}
	sc := &stubCompactor{view: []llm.Message{llm.UserText("summary")}}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: m.call, Compactor: sc}, nil, Options{})
	sess := session.NewMemory(nil)
	if _, err := collectEvents(l.Run(context.Background(), sess, "go")); err != nil {
		t.Fatal(err)
	}
	if len(sc.calls) != 1 || sc.calls[0].Trigger != session.TriggerAuto {
		t.Fatalf("the loop asks for a compaction once per request, got %+v", sc.calls)
	}
	if _, ok := sess.LastCompaction(); !ok {
		t.Error("the compaction is recorded in the session")
	}
}

type recordingModel struct {
	errs   []error
	inputs []llm.CompletionRequest
}

func (r *recordingModel) call(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	r.inputs = append(r.inputs, req)
	return func(yield func(llm.StreamEvent, error) bool) {
		var err error
		if len(r.errs) > 0 {
			err, r.errs = r.errs[0], r.errs[1:]
		}
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		for _, ev := range textTurn("done") {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func TestLoopCompactsAndRetriesOnOverflow(t *testing.T) {
	overflow := errors.New(`POST "https://x/v1/messages": 400 Bad Request {"error":{"message":"prompt is too long: 300000 tokens > 200000 maximum"}}`)
	m := &recordingModel{errs: []error{overflow}}
	sc := &stubCompactor{view: []llm.Message{llm.UserText("summary")}}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: m.call, Compactor: sc}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.inputs) != 2 {
		t.Fatalf("overflow retries the request once, got %d calls", len(m.inputs))
	}
	if len(sc.calls) != 2 || sc.calls[1].Trigger != session.TriggerOverflow {
		t.Fatalf("the second compaction is reactive, got %+v", sc.calls)
	}
	if got := m.inputs[1].Messages[0].Text(); got != "summary" {
		t.Errorf("the retry sends the compacted view, got %q", got)
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("the run completes after the retry, got %+v", r)
	}
}

func TestLoopOverflowRetryOnlyOnce(t *testing.T) {
	overflow := errors.New(`400 Bad Request: prompt is too long: 1 tokens > 0 maximum`)
	m := &recordingModel{errs: []error{overflow, overflow, overflow}}
	sc := &stubCompactor{view: []llm.Message{llm.UserText("summary")}}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: m.call, Compactor: sc}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.inputs) != 2 {
		t.Errorf("a second overflow is not retried again, got %d calls", len(m.inputs))
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonError {
		t.Fatalf("the second overflow ends the run, got %+v", r)
	}
	if !llm.IsContextOverflow(errors.New(lastResult(evs).Err)) {
		t.Errorf("the error is reported, got %q", lastResult(evs).Err)
	}
}

func TestLoopOverflowWithoutACompactionEnds(t *testing.T) {
	overflow := errors.New(`400 Bad Request: prompt is too long: 1 tokens > 0 maximum`)
	m := &recordingModel{errs: []error{overflow, overflow}}
	sc := &stubCompactor{skipOn: session.TriggerOverflow}
	l := NewLoop(NewRegistry(), RunDeps{CallModel: m.call, Compactor: sc}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.inputs) != 1 {
		t.Errorf("nothing was compacted, so nothing is retried, got %d calls", len(m.inputs))
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonError {
		t.Fatalf("the overflow is reported, got %+v", r)
	}
}

func TestLoopCompactionFailureDisablesFurtherAttempts(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{toolTurn("t1", "echo", "{}", llm.StopReasonToolUse), textTurn("done")}}
	sc := &stubCompactor{err: errors.New("no summary today")}
	reg := NewRegistry()
	echo := &echoTool{readOnly: true, concSafe: true}
	reg.Register(echo)
	l := NewLoop(reg, RunDeps{CallModel: m.call, Compactor: sc}, nil, Options{})
	evs, err := collectEvents(l.Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.calls) != 1 {
		t.Errorf("after a failure the loop stops asking, got %d calls", len(sc.calls))
	}
	if echo.called != 1 {
		t.Error("the turn still runs")
	}
	var told bool
	for _, ev := range evs {
		if ev.Kind == EventCompaction && strings.Contains(ev.Message, "no summary today") {
			told = true
		}
	}
	if !told {
		t.Error("the shell is told that compaction failed")
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("the run continues, got %+v", r)
	}
}

func TestLoopManualCompaction(t *testing.T) {
	sc := &stubCompactor{}
	l := NewLoop(NewRegistry(), RunDeps{Compactor: sc}, nil, Options{})
	sess := bigSession(4)
	sess.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("ok")}})
	evs, err := collectEvents(l.Compact(context.Background(), sess, "keep the failing test"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sc.calls) != 1 || sc.calls[0].Trigger != session.TriggerManual || sc.calls[0].Instructions != "keep the failing test" {
		t.Fatalf("manual compaction carries the trigger and the focus, got %+v", sc.calls)
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("manual compaction completes, got %+v", r)
	}
	if _, ok := sess.LastCompaction(); !ok {
		t.Error("the checkpoint is recorded")
	}
	var announced bool
	for _, ev := range evs {
		if ev.Kind == EventCompaction && ev.Compaction != nil {
			announced = true
		}
	}
	if !announced {
		t.Error("the shell hears about the checkpoint")
	}
}

func TestLoopManualCompactionWithNothingToDo(t *testing.T) {
	sc := &stubCompactor{skipOn: session.TriggerManual}
	l := NewLoop(NewRegistry(), RunDeps{Compactor: sc}, nil, Options{})
	evs, err := collectEvents(l.Compact(context.Background(), session.NewMemory(nil), ""))
	if err != nil {
		t.Fatal(err)
	}
	var said bool
	for _, ev := range evs {
		if ev.Kind == EventCompaction && ev.Message == nothingToCompact {
			said = true
		}
	}
	if !said {
		t.Error("an idle /compact says why it did nothing")
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonCompleted {
		t.Fatalf("it is not an error, got %+v", r)
	}
}

func TestLoopManualCompactionWithoutACompactor(t *testing.T) {
	l := NewLoop(NewRegistry(), RunDeps{}, nil, Options{})
	evs, err := collectEvents(l.Compact(context.Background(), session.NewMemory(nil), ""))
	if err != nil {
		t.Fatal(err)
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonError || !strings.Contains(r.Err, "not available") {
		t.Fatalf("the shell is told compaction is off, got %+v", r)
	}
}

func TestLoopManualCompactionFailureIsAnError(t *testing.T) {
	sc := &stubCompactor{err: errors.New("summary call failed")}
	l := NewLoop(NewRegistry(), RunDeps{Compactor: sc}, nil, Options{})
	evs, _ := collectEvents(l.Compact(context.Background(), session.NewMemory(nil), ""))
	if r := lastResult(evs); r == nil || r.Reason != ReasonError || !strings.Contains(r.Err, "summary call failed") {
		t.Fatalf("a failed /compact reports the cause, got %+v", r)
	}
}
