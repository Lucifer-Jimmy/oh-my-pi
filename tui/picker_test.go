package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

var pickerNow = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

func pickerInfos() []session.Info {
	return []session.Info{
		{ID: "s1aaaa", Title: "Fix the scheduler", Modified: pickerNow.Add(-2 * time.Hour), Messages: 24},
		{ID: "s2bbbb", Title: "Polish the TUI", Modified: pickerNow.Add(-50 * time.Hour), Messages: 8},
		{ID: "s3cccc", Modified: pickerNow.Add(-9 * 24 * time.Hour), Messages: 2},
	}
}

func openSessionPicker(t *testing.T, opts Options, infos []session.Info) *harness {
	t.Helper()
	if opts.ListSessions == nil {
		opts.ListSessions = func() ([]session.Info, error) { return infos, nil }
	}
	h := startWith(t, opts)
	h.m.now = func() time.Time { return pickerNow }
	h.submit("/resume")
	return h
}

func pickerText(h *harness) string {
	rows := h.m.pickerRows()
	for i, r := range rows {
		rows[i] = plain(r)
	}
	return strings.Join(rows, "\n")
}

func TestResumeOpensAPickerOfSavedSessions(t *testing.T) {
	h := openSessionPicker(t, Options{}, pickerInfos())
	if h.m.picker == nil {
		t.Fatal("/resume must open the picker")
	}
	got := pickerText(h)
	for _, want := range []string{
		"Resume session", "3 saved", "› Fix the scheduler", "2h ago · 24 msg",
		"Polish the TUI", "2d ago · 8 msg", "s3cccc", "· 2 msg",
		"enter resumes", "esc cancels",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the picker is missing %q:\n%s", want, got)
		}
	}
	if h.m.input.Placeholder != pickerPlaceholder {
		t.Errorf("the prompt line is the filter, placeholder %q", h.m.input.Placeholder)
	}
	if h.m.paletteRows() != nil {
		t.Error("the command palette must stay closed behind the picker")
	}
}

func TestPickerFiltersAsYouType(t *testing.T) {
	h := openSessionPicker(t, Options{}, pickerInfos())
	typeRunes(h, "pol")
	if got := h.m.pickerMatches(); len(got) != 1 || got[0].ID != "s2bbbb" {
		t.Fatalf("typing must filter by title, got %+v", got)
	}
	text := pickerText(h)
	if !strings.Contains(text, "1 of 3") || strings.Contains(text, "Fix the scheduler") {
		t.Errorf("the count and the rows must follow the query:\n%s", text)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if h.m.picker == nil || h.m.input.Value() != "pol" {
		t.Error("shift+enter must not pick or add a newline in the filter")
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	h.send(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := h.m.pickerMatches(); len(got) != 3 {
		t.Fatalf("erasing the query must restore every session, got %d", len(got))
	}
	typeRunes(h, "zzz")
	if got := h.m.pickerMatches(); len(got) != 0 {
		t.Fatalf("no title matches zzz, got %+v", got)
	}
	if text := pickerText(h); !strings.Contains(text, "no session matches") {
		t.Errorf("an empty result must say so:\n%s", text)
	}
	h.send(keyEnter)
	if h.m.picker == nil {
		t.Error("enter without a match must leave the picker open")
	}
}

func TestPickerEnterResumesTheSelectedSession(t *testing.T) {
	var got string
	stored := storedSession()
	h := openSessionPicker(t, Options{Resume: func(arg string) (session.Session, string, error) {
		got = arg
		return stored, arg, nil
	}}, pickerInfos())
	h.send(keyDown)
	h.send(keyEnter)

	if got != "s2bbbb" {
		t.Fatalf("down then enter must resume the second session, resumed %q", got)
	}
	if h.m.picker != nil {
		t.Error("the picker must close after resuming")
	}
	if h.m.input.Value() != "" || h.m.input.Placeholder != composerPlaceholder {
		t.Errorf("the prompt line must go back to normal, value %q placeholder %q", h.m.input.Value(), h.m.input.Placeholder)
	}
	assertOrder(t, h.out(), "resumed s2bbbb", "It is in agent/loop.go.")
}

func TestPickerEscClosesWithoutResuming(t *testing.T) {
	resumed := 0
	h := openSessionPicker(t, Options{Resume: func(string) (session.Session, string, error) {
		resumed++
		return storedSession(), "x", nil
	}}, pickerInfos())
	typeRunes(h, "pol")
	h.send(keyEsc)

	if resumed != 0 {
		t.Error("esc must not resume anything")
	}
	if h.m.picker != nil || h.m.input.Value() != "" || h.m.input.Placeholder != composerPlaceholder {
		t.Errorf("esc must close the picker and restore the prompt, value %q placeholder %q", h.m.input.Value(), h.m.input.Placeholder)
	}
	typeRunes(h, "/")
	if h.m.paletteRows() == nil {
		t.Error("the command palette must work again after the picker closes")
	}
}

func TestPickerMarksTheCurrentSessionAndDoesNotReloadIt(t *testing.T) {
	store, err := session.Create(t.TempDir(), "s1aaaa", "/work", "m1")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resumed := 0
	h := openSessionPicker(t, Options{
		Session: store,
		Resume: func(string) (session.Session, string, error) {
			resumed++
			return storedSession(), "x", nil
		},
	}, pickerInfos())
	if !strings.Contains(pickerText(h), "current") {
		t.Error("the session in the prompt must be marked current")
	}
	h.send(keyEnter)
	if resumed != 0 {
		t.Error("picking the current session must not reload it")
	}
	if h.m.picker != nil {
		t.Error("the picker must close")
	}
	if !strings.Contains(h.out(), "Already in session s1aaaa") {
		t.Errorf("the notice must say why nothing happened:\n%s", h.out())
	}
}

func TestResumeWithoutSavedSessionsOnlyNotices(t *testing.T) {
	h := openSessionPicker(t, Options{}, nil)
	if h.m.picker != nil {
		t.Error("there is nothing to pick, so no picker")
	}
	if !strings.Contains(h.out(), "No earlier sessions in this directory") {
		t.Errorf("missing the empty notice:\n%s", h.out())
	}
}

func TestResumeDoesNotOpenWhileATurnRuns(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.opts.ListSessions = func() ([]session.Info, error) { return pickerInfos(), nil }
	h.m.run = &activeRun{cancel: func() {}}
	h.submit("/resume")
	if h.m.picker != nil {
		t.Error("the picker must wait for the running turn")
	}
	if !strings.Contains(h.out(), "A turn is running") {
		t.Errorf("missing the warning:\n%s", h.out())
	}
}

func TestPickerPasteFiltersAndResetsTheSelection(t *testing.T) {
	h := openSessionPicker(t, Options{}, pickerInfos())
	h.send(keyDown)
	h.send(tea.PasteMsg{Content: "tui"})
	if h.m.pickerSel != 0 {
		t.Errorf("a paste changes the query, so the selection starts over, got %d", h.m.pickerSel)
	}
	if got := h.m.pickerMatches(); len(got) != 1 || got[0].ID != "s2bbbb" {
		t.Errorf("the pasted text must filter like typing, got %+v", got)
	}
}

func TestPickerFitsANarrowTerminal(t *testing.T) {
	h := openSessionPicker(t, Options{}, pickerInfos())
	h.send(tea.WindowSizeMsg{Width: 20, Height: 20})
	for _, row := range h.m.pickerRows() {
		if w := ansi.StringWidth(row); w > h.m.cols() {
			t.Errorf("row is %d cells on a %d-column terminal: %q", w, h.m.cols(), plain(row))
		}
	}
}

func TestPickerWindowFollowsTheSelection(t *testing.T) {
	infos := make([]session.Info, 20)
	for i := range infos {
		infos[i] = session.Info{
			ID: fmt.Sprintf("s%02d", i), Title: fmt.Sprintf("Session %02d", i),
			Modified: pickerNow.Add(-time.Duration(i) * time.Hour), Messages: 1,
		}
	}
	h := openSessionPicker(t, Options{}, infos)
	for i := 0; i < 9; i++ {
		h.send(keyDown)
	}
	text := pickerText(h)
	if !strings.Contains(text, "› Session 09") {
		t.Errorf("the selection must stay on screen:\n%s", text)
	}
	if strings.Contains(text, "Session 01") {
		t.Errorf("rows above the window must scroll away:\n%s", text)
	}
	if !strings.Contains(text, "↑ 2 more") || !strings.Contains(text, "↓ 10 more") {
		t.Errorf("both ends must be counted:\n%s", text)
	}
	h.send(tea.KeyPressMsg{Code: tea.KeyPgDown})
	if h.m.pickerSel != 17 {
		t.Errorf("pgdown must jump a window, selected %d", h.m.pickerSel)
	}
}
