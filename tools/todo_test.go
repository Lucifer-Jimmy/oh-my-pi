package tools

import (
	"strings"
	"testing"
)

func runTodo(t *testing.T, items []map[string]any) (string, bool) {
	t.Helper()
	return call(t, &Todo{}, t.TempDir(), map[string]any{"items": items})
}

func TestTodoRendersTheList(t *testing.T) {
	got, isErr := runTodo(t, []map[string]any{
		{"content": "read the code", "status": "completed"},
		{"content": "fix the bug", "status": "in_progress"},
		{"content": "run the tests", "status": "pending"},
	})
	if isErr {
		t.Fatalf("a valid list must not be an error: %q", got)
	}
	for _, want := range []string{"1/3 done", "[x] read the code", "[>] fix the bug", "[ ] run the tests"} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered list missing %q:\n%s", want, got)
		}
	}
}

func TestTodoRejectsTwoInProgress(t *testing.T) {
	got, isErr := runTodo(t, []map[string]any{
		{"content": "a", "status": "in_progress"},
		{"content": "b", "status": "in_progress"},
	})
	if !isErr || !strings.Contains(got, "at most one") {
		t.Errorf("got isErr=%v %q", isErr, got)
	}
}

func TestTodoRejectsBadStatusAndEmptyContent(t *testing.T) {
	if got, isErr := runTodo(t, []map[string]any{{"content": "a", "status": "doing"}}); !isErr || !strings.Contains(got, "pending") {
		t.Errorf("bad status: isErr=%v %q", isErr, got)
	}
	if got, isErr := runTodo(t, []map[string]any{{"content": "  ", "status": "pending"}}); !isErr || !strings.Contains(got, "no content") {
		t.Errorf("empty content: isErr=%v %q", isErr, got)
	}
	if got, isErr := runTodo(t, []map[string]any{}); !isErr || !strings.Contains(got, "must not be empty") {
		t.Errorf("empty list: isErr=%v %q", isErr, got)
	}
}

func TestTodoIsReadAndExclusive(t *testing.T) {
	tool := &Todo{}
	if !tool.IsReadOnly() || tool.IsConcurrencySafe(nil) {
		t.Errorf("the list is read tier and must not run in parallel: it is shared state")
	}
}
