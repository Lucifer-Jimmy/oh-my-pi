package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestModelCommandShowsCurrent(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/model")
	if out := h.out(); !strings.Contains(out, "gw/m1") {
		t.Errorf("/model must show the current model label:\n%s", out)
	}
}

func TestModelCommandSwitches(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	var got string
	h.m.opts.SwitchModel = func(selector string) (string, error) {
		got = selector
		return "other/gw-m2", nil
	}
	h.submit("/model other/gw-m2")
	if got != "other/gw-m2" {
		t.Fatalf("selector = %q", got)
	}
	if h.m.opts.Model != "other/gw-m2" {
		t.Errorf("status bar model = %q", h.m.opts.Model)
	}
	if out := h.out(); !strings.Contains(out, "other/gw-m2") {
		t.Errorf("the switch must be confirmed:\n%s", out)
	}
}

func TestModelSwitchFailureIsReported(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.m.opts.SwitchModel = func(string) (string, error) { return "", errors.New("no such provider") }
	h.submit("/model nope/x")
	if h.m.opts.Model != "gw/m1" {
		t.Errorf("a failed switch must keep the old model, got %q", h.m.opts.Model)
	}
	if out := h.out(); !strings.Contains(out, "no such provider") {
		t.Errorf("the failure must be shown:\n%s", out)
	}
}

func TestModelSwitchWithoutControlIsReported(t *testing.T) {
	h := newHarness(t, &scriptRunner{}, nil)
	h.submit("/model other/x")
	if out := h.out(); !strings.Contains(out, "cannot switch models") {
		t.Errorf("a session without switching must say so:\n%s", out)
	}
}
