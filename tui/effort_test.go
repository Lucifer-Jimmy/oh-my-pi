package tui

import (
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
)

type fakeEffort struct{ level llm.ThinkingLevel }

func (f *fakeEffort) Level() llm.ThinkingLevel     { return f.level }
func (f *fakeEffort) SetLevel(l llm.ThinkingLevel) { f.level = l }

func newEffortHarness(t *testing.T) (*harness, *fakeEffort) {
	h := newHarness(t, &scriptRunner{}, nil)
	e := &fakeEffort{level: llm.ThinkingHigh}
	h.m.opts.Effort = e
	return h, e
}

func TestEffortCommandShowsAndSets(t *testing.T) {
	h, e := newEffortHarness(t)

	h.submit("/effort")
	if out := h.out(); !strings.Contains(out, "high") {
		t.Errorf("/effort must show the current level:\n%s", out)
	}

	h.submit("/effort low")
	if e.level != llm.ThinkingLow {
		t.Errorf("level = %q, want low", e.level)
	}
	if out := h.out(); !strings.Contains(out, "low") {
		t.Errorf("/effort low must confirm:\n%s", out)
	}
}

func TestEffortUnknownLevelIsRefused(t *testing.T) {
	h, e := newEffortHarness(t)
	h.submit("/effort turbo")
	if e.level != llm.ThinkingHigh {
		t.Errorf("an unknown level must not change the setting, got %q", e.level)
	}
	if out := h.out(); !strings.Contains(out, "Unknown thinking level") {
		t.Errorf("the refusal must name the problem:\n%s", out)
	}
}

func TestEffortWithoutControlIsReported(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/effort high")
	if out := h.out(); !strings.Contains(out, "no effort control") {
		t.Errorf("a session without effort control must say so:\n%s", out)
	}
}

func TestPlanCommandEntersPlanMode(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/plan")
	if got := h.m.opts.Policy.Mode(); got != permission.ModePlan {
		t.Fatalf("mode = %s, want plan", got)
	}
	if out := h.out(); !strings.Contains(out, "Plan mode") {
		t.Errorf("entering plan mode must say what it means:\n%s", out)
	}
	h.submit("/plan")
	if got := h.m.opts.Policy.Mode(); got != permission.ModePlan {
		t.Errorf("a second /plan must stay in plan mode, got %s", got)
	}
}

func TestTodoStripShowsTheCurrentStep(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	if rows := h.m.todoRows(); rows != nil {
		t.Fatalf("no todos means no strip, got %v", rows)
	}
	h.m.state.Todos = []presentation.TodoItem{
		{Content: "read the code", Status: "completed"},
		{Content: "fix the bug", Status: "in_progress"},
		{Content: "run the tests", Status: "pending"},
	}
	rows := h.m.todoRows()
	if len(rows) != 1 || !strings.Contains(plain(rows[0]), "fix the bug") || !strings.Contains(plain(rows[0]), "(1/3)") {
		t.Fatalf("strip = %v", rows)
	}
	h.m.state.Todos = []presentation.TodoItem{
		{Content: "a", Status: "completed"},
		{Content: "b", Status: "completed"},
	}
	if rows := h.m.todoRows(); len(rows) != 1 || !strings.Contains(plain(rows[0]), "all 2 todos done") {
		t.Errorf("all done must say so: %v", rows)
	}
}

func TestRenameExportDeleteCommands(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	var renamed, exported, deleted string
	h.m.opts.Rename = func(title string) error {
		renamed = title
		return nil
	}
	h.m.opts.Export = func(path string) (string, error) {
		if path == "" {
			path = "auto.md"
		}
		exported = path
		return "/tmp/" + path, nil
	}
	h.m.opts.Delete = func(id string) error {
		deleted = id
		return nil
	}

	h.submit("/rename release work")
	if renamed != "release work" {
		t.Fatalf("rename got %q", renamed)
	}
	if out := h.out(); !strings.Contains(out, "Session renamed to release work") {
		t.Errorf("rename confirmation missing:\n%s", out)
	}

	h.submit("/export out.md")
	if exported != "out.md" {
		t.Fatalf("export got %q", exported)
	}
	if out := h.out(); !strings.Contains(out, "Exported to") {
		t.Errorf("export confirmation missing:\n%s", out)
	}

	h.submit("/delete 20260101-120000-abcd")
	if deleted != "20260101-120000-abcd" {
		t.Fatalf("delete got %q", deleted)
	}
	if out := h.out(); !strings.Contains(out, "Deleted session") {
		t.Errorf("delete confirmation missing:\n%s", out)
	}
}

func TestSessionCommandsExplainMissingArguments(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.opts.Rename = func(string) error { return nil }
	h.m.opts.Delete = func(string) error { return nil }
	h.submit("/rename")
	if out := h.out(); !strings.Contains(out, "/rename <title>") {
		t.Errorf("rename without a title must explain itself:\n%s", out)
	}
	h.submit("/delete")
	if out := h.out(); !strings.Contains(out, "/delete <id>") {
		t.Errorf("delete without an id must explain itself:\n%s", out)
	}
}

func TestSessionCommandsFailClosedWithoutCallbacks(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/rename x")
	if out := h.out(); !strings.Contains(out, "cannot be renamed") {
		t.Errorf("rename without a store must say so:\n%s", out)
	}
	h.submit("/export")
	if out := h.out(); !strings.Contains(out, "cannot be exported") {
		t.Errorf("export without a store must say so:\n%s", out)
	}
	h.submit("/delete x")
	if out := h.out(); !strings.Contains(out, "cannot delete sessions") {
		t.Errorf("delete without a store must say so:\n%s", out)
	}
}
