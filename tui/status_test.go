package tui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

func statusAt(t *testing.T, model string, width int, running bool) string {
	t.Helper()
	m := newModel(context.Background(), Options{
		Runner: &scriptRunner{}, Model: model, CWD: "/work",
		Policy: permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{}),
		Window: func() int { return 128000 },
	})
	m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
	m.state.Usage = llm.Usage{InputTokens: 31234, OutputTokens: 1890, CacheReadTokens: 120000}
	m.state.Context = 34567
	if running {
		m.run = &activeRun{cancel: func() {}}
	}
	row := ansi.Strip(m.statusRow())
	if w := ansi.StringWidth(row); w > width-1 {
		t.Errorf("status bar is %d cells on a %d-column terminal: %q", w, width, row)
	}
	return row
}

func TestStatusBarKeepsTheLiveFieldsWhenTheModelNameIsLong(t *testing.T) {
	long := "mify/volcengine_maas/deepseek-v4.1-flash"
	cases := []struct {
		width   int
		running bool
		want    []string
	}{
		{120, false, []string{long, "acceptEdits", "ctx 27.0%/128k", "↑151.2k ↓1.9k", "/help"}},
		{100, true, []string{"deepseek-v4.1-flash", "acceptEdits", "ctx 27.0%/128k", "↑151.2k ↓1.9k", "esc to interrupt"}},
		{80, false, []string{"deepseek-v4.1-flash", "acceptEdits", "ctx 27.0%/128k", "↑151.2k ↓1.9k", "/help"}},
		{80, true, []string{"deepseek-v4.1-flash", "acceptEdits", "ctx 27.0%/128k", "esc to interrupt"}},
		{60, true, []string{"deepseek-v4.1-flash", "acceptEdits", "ctx 27.0%/128k"}},
		{40, true, []string{"omp", "acceptEdits", "ctx 27.0%/128k"}},
	}
	for _, c := range cases {
		row := statusAt(t, long, c.width, c.running)
		for _, want := range c.want {
			if !strings.Contains(row, want) {
				t.Errorf("width %d running %v: missing %q in %q", c.width, c.running, want, row)
			}
		}
		if strings.Contains(row, "…") || strings.Contains(row, "...") {
			t.Errorf("width %d: nothing may be cut mid-word: %q", c.width, row)
		}
	}
	if row := statusAt(t, long, 40, true); strings.Contains(row, "deepseek") {
		t.Errorf("at 40 columns the context share outranks the model name: %q", row)
	}
}

func TestStatusBarShowsAShortModelNameInFull(t *testing.T) {
	row := statusAt(t, "anthropic/claude-opus-5-5", 100, true)
	for _, want := range []string{"anthropic/claude-opus-5-5", "ctx 27.0%/128k", "↑151.2k ↓1.9k", "esc to interrupt"} {
		if !strings.Contains(row, want) {
			t.Errorf("there is room for everything, missing %q: %q", want, row)
		}
	}
}

func TestStatusBarWithoutAModelName(t *testing.T) {
	row := statusAt(t, "", 80, false)
	if !strings.HasPrefix(row, "omp ") || !strings.Contains(row, "↑151.2k ↓1.9k") || !strings.Contains(row, "/help") {
		t.Errorf("no model label must not cost the other fields: %q", row)
	}
}

func TestModelLabelsShortenStepByStep(t *testing.T) {
	got := strings.Join(modelLabels("mify/volcengine_maas/deepseek-v4.1-flash:high"), " | ")
	if want := "mify/volcengine_maas/deepseek-v4.1-flash:high | volcengine_maas/deepseek-v4.1-flash:high | deepseek-v4.1-flash:high"; got != want {
		t.Errorf("labels = %q, want %q", got, want)
	}
	if got := modelLabels("anthropic/claude"); len(got) != 2 || got[1] != "claude" {
		t.Errorf("labels = %q", got)
	}
	if got := modelLabels(""); got != nil {
		t.Errorf("no model, no labels: %q", got)
	}
}
