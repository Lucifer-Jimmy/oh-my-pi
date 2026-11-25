package llm

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"math/rand/v2"
	"strings"
	"time"
)

type Class int

const (
	ClassUnknown Class = iota
	ClassTransient
	ClassOverflow
	ClassFatal
)

func (c Class) String() string {
	switch c {
	case ClassTransient:
		return "transient"
	case ClassOverflow:
		return "overflow"
	case ClassFatal:
		return "fatal"
	}
	return "unknown"
}

type HTTPError struct {
	Status     int
	RequestID  string
	RetryAfter time.Duration
	Err        error
}

func (e *HTTPError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("http %d", e.Status)
	}
	return e.Err.Error()
}

func (e *HTTPError) Unwrap() error { return e.Err }

func (e *HTTPError) HTTPStatusCode() int { return e.Status }

func (e *HTTPError) RetryAfterDuration() time.Duration { return e.RetryAfter }

type statusCarrier interface {
	HTTPStatusCode() int
}

type retryAfterCarrier interface {
	RetryAfterDuration() time.Duration
}

func StatusOf(err error) int {
	if err == nil {
		return 0
	}
	var sc statusCarrier
	if errors.As(err, &sc) {
		return sc.HTTPStatusCode()
	}
	var s interface{ StatusCode() int }
	if errors.As(err, &s) {
		return s.StatusCode()
	}
	return 0
}

func RetryAfterOf(err error) time.Duration {
	var r retryAfterCarrier
	if errors.As(err, &r) {
		return r.RetryAfterDuration()
	}
	var s interface{ RetryAfter() time.Duration }
	if errors.As(err, &s) {
		return s.RetryAfter()
	}
	return 0
}

func Classify(err error) Class {
	if err == nil {
		return ClassUnknown
	}
	if IsContextOverflow(err) {
		return ClassOverflow
	}
	switch StatusOf(err) {
	case 408, 409, 425, 429:
		return ClassTransient
	case 500, 502, 503, 504, 529:
		return ClassTransient
	case 400, 401, 403, 404, 413, 422:
		return ClassFatal
	}
	if errors.Is(err, context.Canceled) {
		return ClassFatal
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{
		"connection reset", "connection refused", "broken pipe", "unexpected eof",
		"i/o timeout", "tls handshake timeout", "temporary failure", "server closed",
		"502 bad gateway", "503 service", "504 gateway", "rate limit", "overloaded",
		"try again", "timed out",
	} {
		if strings.Contains(msg, s) {
			return ClassTransient
		}
	}
	return ClassUnknown
}

type RetryPolicy struct {
	Attempts   int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
	MaxElapsed time.Duration
	Jitter     float64
	Now        func() time.Time
	Sleep      func(context.Context, time.Duration) error
	OnRetry    func(attempt int, delay time.Duration, err error)
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		Attempts:   4,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   20 * time.Second,
		MaxElapsed: 2 * time.Minute,
		Jitter:     0.25,
	}
}

func (p RetryPolicy) normalized() RetryPolicy {
	d := DefaultRetryPolicy()
	if p.Attempts <= 0 {
		p.Attempts = d.Attempts
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = d.BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = d.MaxDelay
	}
	if p.MaxElapsed <= 0 {
		p.MaxElapsed = d.MaxElapsed
	}
	if p.Now == nil {
		p.Now = time.Now
	}
	if p.Sleep == nil {
		p.Sleep = sleepContext
	}
	return p
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (p RetryPolicy) delay(attempt int, err error) time.Duration {
	if ra := RetryAfterOf(err); ra > 0 {
		if ra > p.MaxDelay {
			return p.MaxDelay
		}
		return ra
	}
	d := p.BaseDelay << (attempt - 1)
	if d > p.MaxDelay || d <= 0 {
		d = p.MaxDelay
	}
	if p.Jitter > 0 {
		spread := float64(d) * p.Jitter
		d = time.Duration(float64(d) - spread + rand.Float64()*2*spread)
	}
	if d < 0 {
		d = 0
	}
	return d
}

func (p RetryPolicy) retriable(class Class) bool {
	return class == ClassTransient || class == ClassUnknown
}

type Retrying struct {
	Inner  Provider
	Policy RetryPolicy
}

func NewRetrying(p Provider, policy RetryPolicy) *Retrying {
	return &Retrying{Inner: p, Policy: policy.normalized()}
}

func (r *Retrying) Stream(ctx context.Context, req CompletionRequest) iter.Seq2[StreamEvent, error] {
	return func(yield func(StreamEvent, error) bool) {
		policy := r.Policy
		start := policy.Now()
		for attempt := 1; ; attempt++ {
			received := false
			var last error
			cont := true
			for ev, err := range r.Inner.Stream(ctx, req) {
				if err != nil {
					last = err
					break
				}
				received = true
				if !yield(ev, nil) {
					cont = false
					break
				}
			}
			if last == nil || !cont {
				return
			}
			class := Classify(last)
			if received || attempt >= policy.Attempts || !policy.retriable(class) {
				yield(StreamEvent{}, last)
				return
			}
			if policy.Now().Sub(start) > policy.MaxElapsed {
				yield(StreamEvent{}, last)
				return
			}
			d := policy.delay(attempt, last)
			if policy.OnRetry != nil {
				policy.OnRetry(attempt, d, last)
			}
			if err := policy.Sleep(ctx, d); err != nil {
				yield(StreamEvent{}, err)
				return
			}
			if ctx.Err() != nil {
				yield(StreamEvent{}, ctx.Err())
				return
			}
		}
	}
}

func (r *Retrying) Complete(ctx context.Context, req CompletionRequest) (Message, StopReason, Usage, error) {
	acc := NewAccumulator()
	for ev, err := range r.Stream(ctx, req) {
		if err != nil {
			return Message{}, StopReasonError, Usage{}, err
		}
		if err := acc.Add(ev); err != nil {
			return Message{}, StopReasonError, Usage{}, err
		}
	}
	return acc.Message(), acc.StopReason, acc.Usage, nil
}

func RetryNote(attempt int, delay time.Duration, err error) string {
	return fmt.Sprintf("model call failed (%s), retrying in %s (attempt %d): %s",
		Classify(err), delay.Round(time.Millisecond), attempt+1, firstLineOf(err.Error()))
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return strings.TrimSpace(s)
}
