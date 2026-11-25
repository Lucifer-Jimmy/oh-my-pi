package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func typeRunes(h *harness, s string) {
	for _, r := range s {
		h.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func TestPaletteAppearsOnlyForASlashPrefix(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	typeRunes(h, "/")
	rows := h.m.paletteRows()
	if len(rows) == 0 {
		t.Fatalf("a bare slash must open the palette")
	}
	if !strings.Contains(plain(rows[0]), "›") {
		t.Errorf("the first match is selected: %q", plain(rows[0]))
	}
	typeRunes(h, "he")
	if rows := h.m.paletteRows(); len(rows) != 1 || !strings.Contains(plain(rows[0]), "/help") {
		t.Fatalf("/he must narrow to /help, got %v", rows)
	}
	typeRunes(h, "lp ")
	if rows := h.m.paletteRows(); rows != nil {
		t.Errorf("a space closes the palette, got %v", rows)
	}
}

func TestPaletteArrowsMoveAndTabCompletes(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	typeRunes(h, "/")
	all := h.m.paletteMatches()
	if len(all) < 3 {
		t.Fatalf("need at least three commands to test movement, got %d", len(all))
	}
	h.m.input.SetValue("/")
	h.send(tea.KeyPressMsg{Code: tea.KeyDown})
	if h.m.paletteIndex != 1 {
		t.Fatalf("down must select the second match, index = %d", h.m.paletteIndex)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyUp})
	if h.m.paletteIndex != 0 {
		t.Fatalf("up must go back, index = %d", h.m.paletteIndex)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyTab})
	want := all[0].Name + " "
	if got := h.m.input.Value(); got != want {
		t.Fatalf("tab must complete the selected command: %q, want %q", got, want)
	}
}

func TestPaletteNoMatchesFallsThroughToHistory(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	typeRunes(h, "/zzz")
	if rows := h.m.paletteRows(); rows != nil {
		t.Fatalf("no match means no palette, got %v", rows)
	}
	if h.m.paletteOpen() != true {
		t.Errorf("the query is still a slash query even without matches")
	}
}

func TestPaletteWindowFollowsTheSelection(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.input.SetValue("/")
	if n := len(h.m.paletteMatches()); n <= paletteMax {
		t.Fatalf("need more than %d commands to test the window, got %d", paletteMax, n)
	}
	for i := 0; i < paletteMax+3; i++ {
		h.send(keyDown)
	}
	rows := h.m.paletteRows()
	sel := h.m.paletteMatches()[h.m.paletteIndex]
	var text strings.Builder
	for _, r := range rows {
		text.WriteString(plain(r) + "\n")
	}
	if !strings.Contains(text.String(), "› "+sel.Name) {
		t.Fatalf("the selection scrolled out of the window:\n%s", text.String())
	}
	if !strings.Contains(text.String(), "↑ ") {
		t.Errorf("rows above the window need a marker:\n%s", text.String())
	}
	if len(rows) > paletteMax+2 {
		t.Errorf("the window must stay bounded, got %d rows", len(rows))
	}
}

func TestPaletteSelectionResetsWhenTheQueryChanges(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	typeRunes(h, "/")
	h.send(keyDown)
	h.send(keyDown)
	if h.m.paletteIndex != 2 {
		t.Fatalf("two downs must select the third match, index = %d", h.m.paletteIndex)
	}
	typeRunes(h, "c")
	if h.m.paletteIndex != 0 {
		t.Errorf("a new query must start from its first match, index = %d", h.m.paletteIndex)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if h.m.paletteIndex != 0 {
		t.Errorf("erasing must also start from the first match, index = %d", h.m.paletteIndex)
	}
}
