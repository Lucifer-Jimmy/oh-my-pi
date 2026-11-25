package llm

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"
	"time"
)

type flaky struct {
	failures []error
	calls    int
}

func (f *flaky) Stream(ctx context.Context, req CompletionRequest) iter.Seq2[StreamEvent, error] {
	return func(yield func(StreamEvent, error) bool) {
		n := f.calls
		f.calls++
		if n < len(f.failures) && f.failures[n] != nil {
			yield(StreamEvent{}, f.failures[n])
			return
		}
		yield(StreamEvent{Type: SETextDelta, Text: "ok"}, nil)
		yield(StreamEvent{Type: SEMessageDelta, StopReason: StopReasonStop}, nil)
	}
}

func (f *flaky) Complete(ctx context.Context, req CompletionRequest) (Message, StopReason, Usage, error) {
	acc := NewAccumulator()
	for ev, err := range f.Stream(ctx, req) {
		if err != nil {
			return Message{}, StopReasonError, Usage{}, err
		}
		acc.Add(ev)
	}
	return acc.Message(), acc.StopReason, acc.Usage, nil
}

type halfStream struct{}

func (halfStream) Stream(ctx context.Context, req CompletionRequest) iter.Seq2[StreamEvent, error] {
	return func(yield func(StreamEvent, error) bool) {
		yield(StreamEvent{Type: SETextDelta, Text: "partial"}, nil)
		yield(StreamEvent{}, &HTTPError{Status: 503, Err: errors.New("overloaded")})
	}
}

func (h halfStream) Complete(ctx context.Context, req CompletionRequest) (Message, StopReason, Usage, error) {
	return Message{}, StopReasonError, Usage{}, errors.New("unused")
}

func noWait() (func(context.Context, time.Duration) error, *[]time.Duration) {
	var delays []time.Duration
	return func(ctx context.Context, d time.Duration) error {
		delays = append(delays, d)
		return nil
	}, &delays
}

func collect(t *testing.T, p Provider) (string, error) {
	t.Helper()
	var text string
	var last error
	for ev, err := range p.Stream(context.Background(), CompletionRequest{}) {
		if err != nil {
			last = err
			continue
		}
		text += ev.Text
	}
	return text, last
}

func TestClassifyStatusCodes(t *testing.T) {
	cases := []struct {
		status int
		want   Class
	}{
		{429, ClassTransient},
		{500, ClassTransient},
		{502, ClassTransient},
		{503, ClassTransient},
		{529, ClassTransient},
		{408, ClassTransient},
		{401, ClassFatal},
		{403, ClassFatal},
		{400, ClassFatal},
		{422, ClassFatal},
	}
	for _, c := range cases {
		err := &HTTPError{Status: c.status, Err: errors.New("x")}
		if got := Classify(err); got != c.want {
			t.Errorf("status %d classified %s, want %s", c.status, got, c.want)
		}
	}
}

func TestClassifyOverflowBeforeStatus(t *testing.T) {
	err := &HTTPError{Status: 400, Err: errors.New("prompt is too long: 250000 tokens")}
	if got := Classify(err); got != ClassOverflow {
		t.Fatalf("overflow must win over the status code, got %s", got)
	}
	if got := Classify(context.Canceled); got != ClassFatal {
		t.Fatalf("cancellation is not retryable, got %s", got)
	}
	if got := Classify(errors.New("dial tcp: connection reset by peer")); got != ClassTransient {
		t.Fatalf("a reset connection is transient, got %s", got)
	}
	if got := Classify(nil); got != ClassUnknown {
		t.Fatalf("nil is unknown, got %s", got)
	}
}

func TestRetrySucceedsAfterTransientFailures(t *testing.T) {
	sleep, delays := noWait()
	p := NewRetrying(&flaky{failures: []error{
		&HTTPError{Status: 429, Err: errors.New("rate limited")},
		&HTTPError{Status: 503, Err: errors.New("overloaded")},
	}}, RetryPolicy{Sleep: sleep, Jitter: 0})
	text, err := collect(t, p)
	if err != nil {
		t.Fatalf("two transient failures must be retried away: %v", err)
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
	if len(*delays) != 2 {
		t.Fatalf("expected two waits, got %v", *delays)
	}
	if (*delays)[0] >= (*delays)[1] {
		t.Fatalf("the wait must grow between attempts, got %v", *delays)
	}
}

func TestRetryStopsAtFatalErrors(t *testing.T) {
	sleep, delays := noWait()
	f := &flaky{failures: []error{&HTTPError{Status: 401, Err: errors.New("bad key")}}}
	p := NewRetrying(f, RetryPolicy{Sleep: sleep})
	_, err := collect(t, p)
	if err == nil || StatusOf(err) != 401 {
		t.Fatalf("the error must survive retrying, got %v", err)
	}
	if f.calls != 1 || len(*delays) != 0 {
		t.Fatalf("a fatal error must not be retried: calls=%d waits=%v", f.calls, *delays)
	}
}

func TestRetryGivesUpAfterTheAttemptBudget(t *testing.T) {
	sleep, _ := noWait()
	f := &flaky{failures: []error{
		&HTTPError{Status: 500}, &HTTPError{Status: 500},
		&HTTPError{Status: 500}, &HTTPError{Status: 500}, &HTTPError{Status: 500},
	}}
	p := NewRetrying(f, RetryPolicy{Attempts: 3, Sleep: sleep})
	_, err := collect(t, p)
	if err == nil || StatusOf(err) != 500 {
		t.Fatalf("the last failure must surface, got %v", err)
	}
	if f.calls != 3 {
		t.Fatalf("attempts = %d, want 3", f.calls)
	}
}

func TestRetryNeverRetriesAPartiallyDeliveredStream(t *testing.T) {
	sleep, delays := noWait()
	p := NewRetrying(halfStream{}, RetryPolicy{Sleep: sleep})
	text, err := collect(t, p)
	if err == nil {
		t.Fatal("the failure must surface")
	}
	if text != "partial" {
		t.Fatalf("what already streamed must not be dropped, got %q", text)
	}
	if len(*delays) != 0 {
		t.Fatalf("a stream that already answered must not restart: %v", *delays)
	}
}

func TestRetryHonoursRetryAfter(t *testing.T) {
	sleep, delays := noWait()
	p := NewRetrying(&flaky{failures: []error{
		&HTTPError{Status: 429, RetryAfter: 7 * time.Second},
	}}, RetryPolicy{Sleep: sleep, BaseDelay: time.Millisecond})
	if _, err := collect(t, p); err != nil {
		t.Fatal(err)
	}
	if len(*delays) != 1 || (*delays)[0] != 7*time.Second {
		t.Fatalf("the server's Retry-After must win over the backoff, got %v", *delays)
	}
}

func TestRetryStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := NewRetrying(&flaky{failures: []error{&HTTPError{Status: 503}}}, RetryPolicy{
		Sleep: func(context.Context, time.Duration) error { cancel(); return context.Canceled },
	})
	var last error
	for _, err := range p.Stream(ctx, CompletionRequest{}) {
		if err != nil {
			last = err
		}
	}
	if !errors.Is(last, context.Canceled) {
		t.Fatalf("cancellation during the wait must end the retries, got %v", last)
	}
}

func TestRetryNotesNameTheClassAndDelay(t *testing.T) {
	note := RetryNote(2, 1500*time.Millisecond, &HTTPError{Status: 503, Err: errors.New("overloaded\nsecond line")})
	for _, want := range []string{"transient", "1.5s", "attempt 3", "overloaded"} {
		if !strings.Contains(note, want) {
			t.Fatalf("note %q missing %q", note, want)
		}
	}
	if strings.Contains(note, "second line") {
		t.Fatalf("the note must stay one line: %q", note)
	}
}
