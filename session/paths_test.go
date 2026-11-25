package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectDirEncodesCWD(t *testing.T) {
	root := filepath.Join(t.TempDir(), "sessions")
	dir := ProjectDir(root, filepath.Join(t.TempDir(), "work", "repo"))
	if filepath.Dir(dir) != filepath.Clean(root) {
		t.Errorf("project dir must live directly under the root: %s", dir)
	}
	name := filepath.Base(dir)
	if strings.ContainsAny(name, `/\:`) {
		t.Errorf("the directory name must survive on every filesystem: %q", name)
	}
}

func TestProjectDirOfHomeIsNamed(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	if got := filepath.Base(ProjectDir("root", home)); got != "-home" {
		t.Errorf("home encodes as -home, got %q", got)
	}
}

func TestNewIDIsSortableAndUnique(t *testing.T) {
	at := time.Date(2026, 11, 17, 10, 20, 30, 0, time.UTC)
	seen := map[string]bool{}
	for range 100 {
		id := NewID(at)
		if !strings.HasPrefix(id, "20261117-102030-") {
			t.Fatalf("id = %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}
