package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const baselineEnv = "OMP_EVAL_BASELINE"

func TestBaseline(t *testing.T) {
	b, _, err := runAll(context.Background(), golden())
	if err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	line := fmt.Sprintf("baseline: %d/%d passed (%.0f%%), %d turns, in=%d out=%d cache_read=%d cache_write=%d",
		b.Passed, b.Cases, b.Rate*100, b.Turns,
		b.Usage.InputTokens, b.Usage.OutputTokens, b.Usage.CacheReadTokens, b.Usage.CacheWriteTokens)
	t.Log(line)
	if b.Passed != b.Cases {
		t.Fatalf("%s\nfailing: %v", line, b.FailNames)
	}
	if path := os.Getenv(baselineEnv); path != "" {
		body, _ := json.MarshalIndent(b, "", "  ")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("baseline dir: %v", err)
		}
		if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
			t.Fatalf("write baseline: %v", err)
		}
	}
}

func TestBaselineIsDeterministic(t *testing.T) {
	first, _, err := runAll(context.Background(), golden())
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	second, _, err := runAll(context.Background(), golden())
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if first.Cases != second.Cases || first.Passed != second.Passed || first.Turns != second.Turns {
		t.Fatalf("scripted runs must be deterministic: %+v vs %+v", first, second)
	}
	if first.Rate != 1 {
		t.Fatalf("the golden() set is a regression gate: every case must pass, got %+v", first)
	}
}
