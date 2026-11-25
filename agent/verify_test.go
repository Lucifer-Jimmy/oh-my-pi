package agent

import (
	"context"
	"iter"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type fakeRunner struct {
	prompts []string
	answer  func(prompt string) string
}

func (r *fakeRunner) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
	r.prompts = append(r.prompts, input)
	return func(yield func(Event, error) bool) {
		sess.Append(llm.UserText(input))
		text := r.answer(input)
		sess.Append(llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock(text)}})
		yield(Event{Kind: EventTextDelta, Text: text}, nil)
		yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonCompleted, Text: text, Messages: sess.Messages()}}, nil)
	}
}

func collect(t *testing.T, seq iter.Seq2[Event, error]) (events []Event, result *Result) {
	t.Helper()
	for ev, err := range seq {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ev.Kind == EventResult {
			result = ev.Result
			continue
		}
		events = append(events, ev)
	}
	return events, result
}

type checkFunc func(context.Context, session.Session) (CheckResult, error)

func (f checkFunc) Verify(ctx context.Context, sess session.Session) (CheckResult, error) {
	return f(ctx, sess)
}

func failing(times int) Verifier {
	return checkFunc(func(context.Context, session.Session) (CheckResult, error) {
		if times > 0 {
			times--
			return CheckResult{Feedback: "test failed: expected 2 got 3"}, nil
		}
		return CheckResult{Passed: true}, nil
	})
}

func TestVerifierRemindsUntilTheCheckPasses(t *testing.T) {
	run := &fakeRunner{answer: func(string) string { return "done" }}
	v := VerifierRunner{Runner: run, Checks: []Verifier{failing(2)}, Attempts: 3}
	events, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "fix the test"))

	if len(run.prompts) != 3 {
		t.Fatalf("want three attempts, got %d", len(run.prompts))
	}
	if run.prompts[0] != "fix the test" {
		t.Errorf("the first prompt is the user's: %q", run.prompts[0])
	}
	if !strings.Contains(run.prompts[1], "expected 2 got 3") || !strings.Contains(run.prompts[1], "do not claim success") {
		t.Errorf("the retry must carry the check's output and the rule: %q", run.prompts[1])
	}
	var notes int
	for _, ev := range events {
		if ev.Kind == EventNotification {
			notes++
		}
	}
	if notes != 2 {
		t.Errorf("each failed attempt is announced: %d", notes)
	}
	if result == nil || result.Reason != ReasonCompleted || !result.Complete {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Messages) != 6 {
		t.Errorf("the session holds every attempt: %d messages", len(result.Messages))
	}
}

func TestVerifierGivesUpAtTheCap(t *testing.T) {
	run := &fakeRunner{answer: func(string) string { return "done" }}
	v := VerifierRunner{Runner: run, Checks: []Verifier{failing(99)}, Attempts: 2}
	_, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "fix the test"))

	if len(run.prompts) != 2 {
		t.Fatalf("want two attempts, got %d", len(run.prompts))
	}
	if result.Reason != ReasonVerifier || !result.Complete {
		t.Fatalf("result = %+v", result)
	}
	if !strings.Contains(result.Err, "expected 2 got 3") || !strings.Contains(result.Err, "2 verification attempts") {
		t.Errorf("the failure must be reported, not claimed as success: %q", result.Err)
	}
}

func TestVerifierSkipsChecksWhenTheRunFailed(t *testing.T) {
	calls := 0
	check := checkFunc(func(context.Context, session.Session) (CheckResult, error) {
		calls++
		return CheckResult{Passed: true}, nil
	})
	inner := runFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
		return func(yield func(Event, error) bool) {
			yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonMaxTurns, Err: "turn limit"}}, nil)
		}
	})
	v := VerifierRunner{Runner: inner, Checks: []Verifier{check}, Attempts: 3}
	_, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "go"))

	if calls != 0 {
		t.Errorf("the check must not run after a failed turn: %d calls", calls)
	}
	if result == nil || result.Reason != ReasonMaxTurns || !result.Complete {
		t.Errorf("result = %+v", result)
	}
}

func TestVerifierPassesThroughUsage(t *testing.T) {
	inner := runFunc(func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
		return func(yield func(Event, error) bool) {
			yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonCompleted, Text: "ok", Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}}}, nil)
		}
	})
	v := VerifierRunner{Runner: inner, Checks: []Verifier{failing(0)}, Attempts: 2}
	_, result := collect(t, v.Run(context.Background(), session.NewMemory(nil), "go"))
	if result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 2 {
		t.Errorf("usage must survive the wrapper: %+v", result.Usage)
	}
}

type runFunc func(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error]

func (f runFunc) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
	return f(ctx, sess, input)
}
