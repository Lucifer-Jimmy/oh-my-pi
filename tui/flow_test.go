package tui

import (
	"strings"
	"testing"
)

func TestEscClearsTheDraftWhenNoTurnRuns(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.input.SetValue("half a question")
	h.send(keyEsc)
	if got := h.m.input.Value(); got != "" {
		t.Errorf("esc must clear an idle draft, got %q", got)
	}
}

func TestEscLeavesHistoryBrowsing(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.history = []string{"first prompt"}
	h.m.histPos = 1
	h.send(keyUp)
	if got := h.m.input.Value(); got != "first prompt" {
		t.Fatalf("up must reach the last prompt, got %q", got)
	}
	h.send(keyEsc)
	if got := h.m.input.Value(); got != "" || h.m.browsing {
		t.Errorf("esc must leave browsing: value %q browsing %v", got, h.m.browsing)
	}
}

func TestQueueShowsAtMostThreePrompts(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.queue = []string{"one", "two", "three", "four", "five"}
	rows := h.m.queueRows()
	if len(rows) != 4 {
		t.Fatalf("three prompts and a count, got %d rows: %v", len(rows), rows)
	}
	if !strings.Contains(plain(rows[3]), "2 more queued") {
		t.Errorf("the overflow must be counted, got %q", plain(rows[3]))
	}
	if view := h.view(); !strings.Contains(view, "one") || strings.Contains(view, "four") {
		t.Errorf("the dock must show the oldest prompts:\n%s", view)
	}
}
