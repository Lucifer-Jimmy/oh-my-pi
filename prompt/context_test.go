package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkfile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContextFileNamesAnchor(t *testing.T) {
	want := []string{"AGENTS.md", "CLAUDE.md"}
	if len(contextFileNames) != len(want) {
		t.Fatalf("context file names changed: %v", contextFileNames)
	}
	for i := range want {
		if contextFileNames[i] != want[i] {
			t.Errorf("precedence order changed: %v", contextFileNames)
		}
	}
}

func TestDiscoverCollectsOnePerDepth(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "pkg")
	mkfile(t, root, ".git", "")
	mkfile(t, root, "AGENTS.md", "root rules")
	mkfile(t, pkg, "CLAUDE.md", "pkg rules")

	files := DiscoverContextFiles(pkg)
	if len(files) != 2 {
		t.Fatalf("want root and package files, got %d: %+v", len(files), files)
	}
	if filepath.Dir(files[0].Path) != root || files[0].Content != "root rules" {
		t.Errorf("root file must come first, got %+v", files[0])
	}
	if filepath.Dir(files[1].Path) != pkg || files[1].Content != "pkg rules" {
		t.Errorf("package file must come second, got %+v", files[1])
	}
}

func TestDiscoverAGENTSShadowsCLAUDEAtSameDepth(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "AGENTS.md", "standard")
	mkfile(t, dir, "CLAUDE.md", "claude")

	files := DiscoverContextFiles(dir)
	if len(files) != 1 || files[0].Content != "standard" {
		t.Errorf("AGENTS.md must win the depth: %+v", files)
	}
}

func TestDiscoverSkipsEmptyAndFallsThrough(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, "AGENTS.md", "   \n")
	mkfile(t, dir, "CLAUDE.md", "claude")

	files := DiscoverContextFiles(dir)
	if len(files) != 1 || files[0].Content != "claude" {
		t.Errorf("an empty file must not claim the depth: %+v", files)
	}
}

func TestDiscoverStopsAtRepoRoot(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	sub := filepath.Join(repo, "sub")
	mkfile(t, repo, ".git", "")
	mkfile(t, repo, "AGENTS.md", "in repo")
	mkfile(t, tmp, "AGENTS.md", "above repo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	files := DiscoverContextFiles(sub)
	if len(files) != 1 || files[0].Content != "in repo" {
		t.Fatalf("walk must stop at the repository root: %+v", files)
	}
}

func TestDiscoverWithoutRepoChecksCWDOnly(t *testing.T) {
	tmp := t.TempDir()
	leaf := filepath.Join(tmp, "leaf")
	mkfile(t, leaf, "CLAUDE.md", "leaf rules")
	mkfile(t, tmp, "AGENTS.md", "parent rules")

	files := DiscoverContextFiles(leaf)
	if len(files) != 1 || files[0].Content != "leaf rules" {
		t.Errorf("without a repository the walk must not leave cwd: %+v", files)
	}
}

func TestDiscoverNothing(t *testing.T) {
	dir := t.TempDir()
	mkfile(t, dir, ".git", "")
	if files := DiscoverContextFiles(dir); len(files) != 0 {
		t.Errorf("no context files expected, got %+v", files)
	}
}

func TestContextBlockRendersRepoRules(t *testing.T) {
	path := filepath.Join("repo", "AGENTS.md")
	got := Build(Context{Files: []ContextFile{{Path: path, Content: "root rules\r\n"}}}, shipped)[1]
	for _, want := range []string{"<repo-rules>", `<file path="` + path + `">`, "root rules\n</file>", "</repo-rules>\n</project-context>"} {
		if !strings.Contains(got, want) {
			t.Errorf("context block missing %q:\n%q", want, got)
		}
	}
}

func TestContextBlockWithoutFilesHasNoRepoRules(t *testing.T) {
	if got := Build(Context{}, shipped)[1]; strings.Contains(got, "<repo-rules>") {
		t.Errorf("no files must mean no repo-rules block:\n%s", got)
	}
}

func TestStaticBlockIgnoresContextFiles(t *testing.T) {
	a := Build(Context{}, shipped)[0]
	b := Build(Context{Files: []ContextFile{{Path: "AGENTS.md", Content: "x"}}}, shipped)[0]
	if a != b {
		t.Error("context files must stay out of the cached static block")
	}
}
