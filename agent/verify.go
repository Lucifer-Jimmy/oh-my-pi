package agent

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type CheckResult struct {
	Passed   bool
	Feedback string
}

type Verifier interface {
	Verify(ctx context.Context, sess session.Session) (CheckResult, error)
}

const defaultVerifyAttempts = 3

const verifyPrompt = "The task is not done yet. An external check ran after your last turn and did not pass. " +
	"Fix the underlying problem, do not change the check, and do not claim success without running it.\n\n%s"

type VerifierRunner struct {
	Runner   Runner
	Checks   []Verifier
	Attempts int
}

func (v VerifierRunner) Run(ctx context.Context, sess session.Session, input string) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		attempts := v.Attempts
		if attempts <= 0 {
			attempts = defaultVerifyAttempts
		}
		prompt := input
		var usage llm.Usage
		turns := 0
		for attempt := 1; ; attempt++ {
			var last *Result
			passed := false
			for ev, err := range v.Runner.Run(ctx, sess, prompt) {
				if err != nil {
					yield(Event{}, err)
					return
				}
				if ev.Kind == EventResult {
					if ev.Result != nil {
						last = ev.Result
						usage.Add(ev.Result.Usage)
						turns += ev.Result.Turns
						passed = ev.Result.Reason == ReasonCompleted
					}
					continue
				}
				if !yield(ev, nil) {
					return
				}
			}
			if last == nil {
				last = &Result{Reason: ReasonError, Err: "the run ended without a result"}
			}
			if !passed || ctx.Err() != nil {
				final := *last
				final.Usage, final.Turns, final.Complete = usage, turns, true
				yield(Event{Kind: EventResult, Result: &final}, nil)
				return
			}
			result, err := v.check(ctx, sess)
			if err != nil {
				yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonError, Err: err.Error(), Text: last.Text, Usage: usage, Turns: turns, Messages: sess.Messages(), Complete: true}}, nil)
				return
			}
			if result.Passed {
				yield(Event{Kind: EventResult, Result: &Result{Reason: ReasonCompleted, Text: last.Text, Usage: usage, Turns: turns, Messages: sess.Messages(), Complete: true}}, nil)
				return
			}
			if attempt >= attempts {
				yield(Event{Kind: EventResult, Result: &Result{
					Reason:   ReasonVerifier,
					Err:      fmt.Sprintf("gave up after %d verification attempts; the last check said:\n\n%s", attempt, result.Feedback),
					Text:     last.Text,
					Usage:    usage,
					Turns:    turns,
					Messages: sess.Messages(),
					Complete: true,
				}}, nil)
				return
			}
			if !yield(Event{Kind: EventNotification, Message: fmt.Sprintf("Verification failed (attempt %d of %d), continuing", attempt, attempts)}, nil) {
				return
			}
			prompt = fmt.Sprintf(verifyPrompt, result.Feedback)
		}
	}
}

func (v VerifierRunner) check(ctx context.Context, sess session.Session) (CheckResult, error) {
	var failures []string
	for _, c := range v.Checks {
		if c == nil {
			continue
		}
		result, err := c.Verify(ctx, sess)
		if err != nil {
			return CheckResult{}, err
		}
		if !result.Passed {
			failures = append(failures, result.Feedback)
		}
	}
	if len(failures) > 0 {
		return CheckResult{Feedback: strings.Join(failures, "\n\n")}, nil
	}
	return CheckResult{Passed: true}, nil
}
