package eval

import (
	"context"
	"testing"
)

func TestGolden(t *testing.T) {
	for _, c := range golden() {
		t.Run(c.Name, func(t *testing.T) {
			out, err := run(context.Background(), c)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if !out.Passed {
				t.Fatalf("case failed: %v\ntext=%q\ncalls=%v\nreason=%s", out.Failures, out.Text, out.Calls, out.Reason)
			}
		})
	}
}

func TestDenyRuleViaAllowListIsNotEnough(t *testing.T) {
	c := evalCase{
		Name:   "probe",
		Files:  map[string]string{"a.txt": "hello\n"},
		Prompt: "noop",
		Script: []turn{{Text: "hello there"}},
		Checks: []check{finalTextContains("absent")},
	}
	out, err := run(context.Background(), c)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out.Passed {
		t.Fatalf("a check that cannot pass must fail the case; the harness is not measuring anything")
	}
	if len(out.Failures) == 0 {
		t.Fatalf("a failed case must explain itself")
	}
}
