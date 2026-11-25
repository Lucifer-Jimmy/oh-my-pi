package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

var keyCtrlO = tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}

func (h *harness) ask(host *Host, req agent.AskRequest) chan agent.AskResponse {
	h.t.Helper()
	res := make(chan agent.AskResponse, 1)
	go func() {
		r, _ := host.Ask(context.Background(), req)
		res <- r
	}()
	h.send(waitAsk(host)())
	return res
}

func (h *harness) typeText(s string) {
	for _, r := range s {
		h.send(keyRune(r))
	}
}

func TestEditApprovalShowsTheLineDiff(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	input, _ := json.Marshal(map[string]string{
		"path":       "calc.go",
		"old_string": "func Add(a, b int) int {\n\treturn a - b\n}",
		"new_string": "func Add(a, b int) int {\n\treturn a + b\n}",
	})
	res := h.ask(host, agent.AskRequest{
		Method: agent.AskSelect, Title: "Approve edit", Message: "approval required for write tool",
		Options: []string{"Yes", "No"}, Tool: "edit", Input: input, Detail: "path: calc.go",
	})
	v := h.view()
	for _, want := range []string{"Approve edit", "calc.go", "-     return a - b", "+     return a + b", "  func Add(a, b int) int {", "press 1-2", "ctrl+o full view"} {
		if !strings.Contains(v, want) {
			t.Errorf("dock missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "- func Add") || strings.Contains(v, "path: calc.go") {
		t.Errorf("unchanged lines must not be marked and the raw preview must not show:\n%s", v)
	}
	h.send(keyEnter)
	if len(h.m.asks) != 1 {
		t.Fatal("Enter must never answer an approval")
	}
	h.send(keyRune('1'))
	if r := <-res; r.Value != "Yes" {
		t.Errorf("1 must approve: %+v", r)
	}
}

func TestWriteApprovalDiffsAgainstTheFileOnDisk(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("\ufeffkeep\nold line\nkeep too\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	h.m.opts.CWD = dir
	approve := func(path, content string) (chan agent.AskResponse, string) {
		input, _ := json.Marshal(map[string]string{"path": path, "content": content})
		res := h.ask(host, agent.AskRequest{Method: agent.AskSelect, Title: "Approve write", Options: []string{"Yes", "No"}, Tool: "write", Input: input})
		return res, h.view()
	}

	res, v := approve("notes.txt", "keep\nnew line\nkeep too\n")
	for _, want := range []string{"notes.txt · overwrites the file", "- old line", "+ new line"} {
		if !strings.Contains(v, want) {
			t.Errorf("overwrite dock missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "- keep") || strings.Contains(v, "+ keep") {
		t.Errorf("a BOM or a line ending must not show as a change:\n%s", v)
	}
	h.send(keyEsc)
	if r := <-res; !r.Cancelled {
		t.Errorf("esc must cancel: %+v", r)
	}

	res, v = approve("fresh.txt", "a\nb\n")
	for _, want := range []string{"fresh.txt · new file", "+ a", "+ b"} {
		if !strings.Contains(v, want) {
			t.Errorf("new-file dock missing %q:\n%s", want, v)
		}
	}
	h.send(keyRune('2'))
	if r := <-res; r.Value != "No" {
		t.Errorf("2 must pick No: %+v", r)
	}
}

func TestFreeFormQuestionTakesATypedAnswer(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	h.m.input.SetValue("draft")
	res := h.ask(host, agent.AskRequest{Method: agent.AskInput, Title: "The model is asking", Message: "Which database should I target?"})
	v := h.view()
	for _, want := range []string{"Which database should I target?", "Type your answer", "enter answers"} {
		if !strings.Contains(v, want) {
			t.Errorf("dock missing %q:\n%s", want, v)
		}
	}
	h.send(keyEnter)
	if len(h.m.asks) != 1 {
		t.Fatal("an empty answer must not be sent")
	}
	h.typeText("postgres")
	h.send(tea.PasteMsg{Content: " 16"})
	view := h.m.View()
	lines := strings.Split(ansi.Strip(view.Content), "\n")
	if view.Cursor == nil || view.Cursor.Y >= len(lines) || !strings.Contains(lines[view.Cursor.Y], "postgres 16") {
		t.Fatalf("the cursor must sit on the answer row, got %+v in:\n%s", view.Cursor, ansi.Strip(view.Content))
	}
	if want := ansi.StringWidth("▌ › postgres 16"); view.Cursor.X != want {
		t.Errorf("cursor column = %d, want %d", view.Cursor.X, want)
	}
	h.send(keyEnter)
	if r := <-res; r.Value != "postgres 16" {
		t.Errorf("answer = %+v", r)
	}
	if h.m.input.Value() != "draft" {
		t.Errorf("the composer draft changed: %q", h.m.input.Value())
	}

	res = h.ask(host, agent.AskRequest{Method: agent.AskInput, Title: "The model is asking", Message: "用哪个数据库？"})
	h.send(tea.PasteMsg{Content: "数据库"})
	view = h.m.View()
	if want := ansi.StringWidth("▌ › 数据库"); view.Cursor == nil || view.Cursor.X != want {
		t.Errorf("a wide-character answer must put the cursor at column %d, got %+v", want, view.Cursor)
	}
	h.send(keyEsc)
	if r := <-res; !r.Cancelled {
		t.Errorf("esc must dismiss: %+v", r)
	}
}

func TestLongChoiceListScrollsAndNeedsADeliberatePick(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	var opts []string
	for i := 1; i <= 12; i++ {
		opts = append(opts, fmt.Sprintf("option %d", i))
	}
	res := h.ask(host, agent.AskRequest{Method: agent.AskSelect, Title: "The model is asking", Message: "Pick one", Options: opts})
	v := h.view()
	if !strings.Contains(v, "option 9") || strings.Contains(v, "option 10") || !strings.Contains(v, "↓ 3 more") || !strings.Contains(v, "press 1-9 or ↑↓ enter") {
		t.Fatalf("first page:\n%s", v)
	}
	h.send(keyEnter)
	if len(h.m.asks) != 1 {
		t.Fatal("Enter without a highlighted option must do nothing")
	}
	for range 11 {
		h.send(keyDown)
	}
	v = h.view()
	if !strings.Contains(v, "›  option 11") || !strings.Contains(v, "↑ 2 more") {
		t.Fatalf("scrolled page:\n%s", v)
	}
	h.send(keyEnter)
	if r := <-res; r.Value != "option 11" {
		t.Errorf("enter must pick the highlighted option: %+v", r)
	}

	res = h.ask(host, agent.AskRequest{Method: agent.AskSelect, Title: "The model is asking", Message: "Pick one", Options: opts})
	h.send(keyRune('3'))
	if r := <-res; r.Value != "option 3" {
		t.Errorf("3 must pick the third option: %+v", r)
	}
}

func TestWriteCardShowsWhatWasWritten(t *testing.T) {
	var lines []string
	for i := 1; i <= 12; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	input, _ := json.Marshal(map[string]string{"path": "notes.txt", "content": strings.Join(lines, "\n")})
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "w1", ToolName: "write", ToolInput: input})
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "w1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock("Created notes.txt (12 lines)")}}})
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("write it")
	h.pump()
	out := h.out()
	assertOrder(t, out, "● Write(notes.txt)", "⎿ Created notes.txt (12 lines)", "+ line 01", "+ line 08", "… +4 lines · ctrl+o shows all")
	if strings.Contains(out, "+ line 09") {
		t.Errorf("the card must stop at eight lines:\n%s", out)
	}
}

func bashRunner(lines int) *scriptRunner {
	var out []string
	for i := 1; i <= lines; i++ {
		out = append(out, fmt.Sprintf("result line %02d", i))
	}
	return &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		emit(agent.Event{Kind: agent.EventToolUse, ToolUseID: "b1", ToolName: "bash", ToolInput: json.RawMessage(`{"command":"go test ./..."}`)})
		emit(agent.Event{Kind: agent.EventToolResult, ToolUseID: "b1", ToolResult: &agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(strings.Join(out, "\n"))}}})
		emit(done(agent.ReasonCompleted))
	}}
}

func TestCtrlOOpensTheFullTranscript(t *testing.T) {
	h := newHarness(t, bashRunner(40), nil)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 20})
	h.submit("run the tests")
	h.pump()
	card := h.out()
	if !strings.Contains(card, "result line 01") || strings.Contains(card, "result line 40") || !strings.Contains(card, "ctrl+o shows all") {
		t.Fatalf("the card must be cut and say how to see the rest:\n%s", card)
	}

	h.send(keyCtrlO)
	view := h.m.View()
	if h.m.transcript == nil || !view.AltScreen {
		t.Fatal("ctrl+o must open the viewer on the alternate screen")
	}
	if v := h.view(); !strings.Contains(v, "result line 40") || !strings.Contains(v, "transcript · bottom") {
		t.Fatalf("the viewer must open at the end:\n%s", v)
	}
	h.send(keyRune('g'))
	if v := h.view(); !strings.Contains(v, "run the tests") || !strings.Contains(v, "transcript · top") {
		t.Fatalf("g must go to the top:\n%s", v)
	}
	h.send(keyRune('j'))
	if v := h.view(); strings.Contains(v, "transcript · top") {
		t.Fatalf("j must scroll down:\n%s", v)
	}
	h.send(keyEsc)
	if h.m.transcript != nil || h.m.View().AltScreen {
		t.Fatal("esc must close the viewer")
	}
	for _, row := range strings.Split(card, "\n") {
		if w := ansi.StringWidth(row); w > 79 {
			t.Errorf("row is %d cells wide on an 80-column terminal: %q", w, row)
		}
	}
}

func TestPrintingWaitsWhileTheViewerIsOpen(t *testing.T) {
	release := make(chan struct{})
	runner := &scriptRunner{script: func(_ context.Context, _ string, emit func(agent.Event)) {
		<-release
		emit(text("answer while viewing\n"))
		emit(done(agent.ReasonCompleted))
	}}
	h := newHarness(t, runner, nil)
	h.submit("go")
	h.send(keyCtrlO)
	if h.m.transcript == nil {
		t.Fatal("ctrl+o must open the viewer during a run")
	}
	h.printed = nil
	close(release)
	h.pump()
	if strings.Contains(h.out(), "answer while viewing") {
		t.Fatal("nothing may be printed while the alternate screen is up")
	}
	if !strings.Contains(h.view(), "answer while viewing") {
		t.Fatalf("the viewer must follow the run:\n%s", h.view())
	}
	if cmd := h.send(keyEsc); cmd == nil {
		t.Fatal("closing must schedule the release of held output")
	}
	if strings.Contains(h.out(), "answer while viewing") {
		t.Fatal("output must wait until the terminal has left the alternate screen")
	}
	h.send(releaseMsg{})
	if !strings.Contains(h.out(), "answer while viewing") {
		t.Fatalf("held output must print after the release:\n%s", h.out())
	}
}

func TestCtrlOWaitsForAPrintInFlight(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.printing = true
	h.m.Update(keyCtrlO)
	if h.m.transcript != nil {
		t.Fatal("the viewer must not open while a print is in flight")
	}
	h.m.Update(printedMsg{})
	if h.m.transcript == nil {
		t.Fatal("the viewer must open once the print has landed")
	}
}

func TestViewerShowsThePendingApprovalInFull(t *testing.T) {
	host := NewHost()
	h := newHarness(t, &scriptRunner{}, host)
	h.send(tea.WindowSizeMsg{Width: 80, Height: 40})
	var before, after []string
	for i := 0; i < 30; i++ {
		before = append(before, fmt.Sprintf("old %02d", i))
		after = append(after, fmt.Sprintf("new %02d", i))
	}
	input, _ := json.Marshal(map[string]string{"path": "big.go", "old_string": strings.Join(before, "\n"), "new_string": strings.Join(after, "\n")})
	res := h.ask(host, agent.AskRequest{Method: agent.AskSelect, Title: "Approve edit", Options: []string{"Yes", "No"}, Tool: "edit", Input: input})
	if v := h.view(); strings.Contains(v, "+ new 29") || !strings.Contains(v, "ctrl+o shows all") {
		t.Fatalf("the dock must cut a long diff:\n%s", v)
	}
	h.send(keyCtrlO)
	if v := h.view(); !strings.Contains(v, "+ new 29") || !strings.Contains(v, "approval waiting") {
		t.Fatalf("the viewer must show the whole diff being approved:\n%s", v)
	}
	h.send(keyRune('1'))
	if len(h.m.asks) != 1 {
		t.Fatal("keys inside the viewer must not answer the approval")
	}
	h.send(keyEsc)
	h.send(keyRune('1'))
	if r := <-res; r.Value != "Yes" {
		t.Errorf("1 must approve once the viewer is closed: %+v", r)
	}
}
