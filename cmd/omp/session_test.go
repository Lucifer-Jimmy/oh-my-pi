package main

import (
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func TestResolveSession(t *testing.T) {
	dir := t.TempDir()
	s, err := session.Create(dir, "20261117-102030-abcd", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("hi"))
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20261117-102030-abcd"+session.FileSuffix)

	for _, arg := range []string{"20261117-102030-abcd", "20261117-102030-abcd" + session.FileSuffix, "20261117", "20261117-1020", path} {
		got, err := resolveSession(dir, arg)
		if err != nil {
			t.Errorf("resolve(%q): %v", arg, err)
			continue
		}
		if got != path {
			t.Errorf("resolve(%q) = %s, want %s", arg, got, path)
		}
	}
	if _, err := resolveSession(dir, "nope"); err == nil {
		t.Error("an unknown id is an error")
	}
}

func TestResolveSessionRejectsAmbiguity(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"20261117-102030-aaaa", "20261117-102030-bbbb"} {
		s, err := session.Create(dir, id, "/work", "m")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	_, err := resolveSession(dir, "20261117")
	if err == nil || !strings.Contains(err.Error(), "2 sessions") {
		t.Fatalf("an ambiguous prefix must name the count, got %v", err)
	}
}

func TestLatestSkipsEmptySessions(t *testing.T) {
	dir := t.TempDir()
	full, err := session.Create(dir, "20261117-100000-full", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	full.Append(llm.UserText("real work"))
	if err := full.Close(); err != nil {
		t.Fatal(err)
	}
	empty, err := session.Create(dir, "20261117-110000-empt", "/work", "m")
	if err != nil {
		t.Fatal(err)
	}
	if err := empty.Close(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := session.Latest(dir)
	if err != nil || !ok {
		t.Fatal(err, ok)
	}
	if filepath.Base(got) != "20261117-100000-full"+session.FileSuffix {
		t.Errorf("--continue must skip a session with no messages, got %s", got)
	}
}
