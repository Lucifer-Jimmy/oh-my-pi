package prompt

import (
	"os"
	"path/filepath"
	"strings"
)

type ContextFile struct {
	Path    string
	Content string
}

var contextFileNames = []string{"AGENTS.md", "CLAUDE.md"}

func DiscoverContextFiles(cwd string) []ContextFile {
	root := repoRoot(cwd)
	if root == "" {
		root = cwd
	}
	var dirs []string
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if dir == root || filepath.Dir(dir) == dir {
			break
		}
	}
	var files []ContextFile
	for i := len(dirs) - 1; i >= 0; i-- {
		if f, ok := contextFileAt(dirs[i]); ok {
			files = append(files, f)
		}
	}
	return files
}

func repoRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func contextFileAt(dir string) (ContextFile, bool) {
	for _, name := range contextFileNames {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil || strings.TrimSpace(string(data)) == "" {
			continue
		}
		return ContextFile{Path: path, Content: string(data)}, true
	}
	return ContextFile{}, false
}
