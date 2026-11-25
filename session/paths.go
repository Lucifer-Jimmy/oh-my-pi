package session

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".omp", "sessions"), nil
}

func ProjectDir(root, cwd string) string {
	return filepath.Join(root, encodeDir(cwd))
}

func encodeDir(cwd string) string {
	abs, err := filepath.Abs(cwd)
	if err != nil {
		abs = cwd
	}
	abs = filepath.Clean(abs)
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(filepath.Clean(home), abs); err == nil && !strings.HasPrefix(rel, "..") {
			if rel == "." {
				return "-home"
			}
			return fold("-" + flatten(rel))
		}
	}
	return fold("--" + flatten(abs) + "--")
}

func fold(name string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(name)
	}
	return name
}

func flatten(p string) string {
	p = strings.ReplaceAll(p, ":", "-")
	p = strings.ReplaceAll(p, string(filepath.Separator), "-")
	p = strings.ReplaceAll(p, "/", "-")
	return strings.Trim(p, "-")
}

func NewID(now time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return now.Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}
