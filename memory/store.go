package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const (
	LessonsFile = "learned.md"
	SummaryFile = "memory_summary.md"

	extractsDir = "sessions"
	statusFile  = "status.json"
	lockFile    = "memory.lock"

	maxLessons      = 100
	maxLessonChars  = 2000
	maxContextChars = 400
	lockStale       = 15 * time.Minute
)

var ErrBusy = errors.New("another omp process is updating this project's memory; try again when it finishes")

func Root() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".omp", "memory"), nil
}

type Store struct {
	Dir string
}

func ForProject(root, cwd string) Store {
	return Store{Dir: session.ProjectDir(root, cwd)}
}

var lessonsMu sync.Mutex

func (s Store) Learn(lesson, context string) (bool, error) {
	content := normalize(lesson, maxLessonChars)
	if content == "" {
		return false, nil
	}
	line := "- " + content
	if c := normalize(context, maxContextChars); c != "" {
		line += " _(context: " + c + ")_"
	}
	lessonsMu.Lock()
	defer lessonsMu.Unlock()
	path := filepath.Join(s.Dir, LessonsFile)
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	var out []string
	for _, l := range splitLines(string(existing)) {
		if isLesson(l) && strings.TrimSpace(l) == line {
			continue
		}
		out = append(out, l)
	}
	first := -1
	for i, l := range out {
		if isLesson(l) {
			first = i
			break
		}
	}
	if first < 0 {
		out = append(out, line)
	} else {
		out = append(out[:first], append([]string{line}, out[first:]...)...)
	}
	count := 0
	for _, l := range out {
		if isLesson(l) {
			count++
		}
	}
	for i := len(out) - 1; i >= 0 && count > maxLessons; i-- {
		if isLesson(out[i]) {
			out = append(out[:i], out[i+1:]...)
			count--
		}
	}
	return true, writeFile(path, strings.Join(out, "\n")+"\n")
}

func (s Store) Lessons() string {
	b, err := os.ReadFile(filepath.Join(s.Dir, LessonsFile))
	if err != nil {
		return ""
	}
	lines := splitLines(string(b))
	for i, l := range lines {
		lines[i] = Redact(neutralize(l))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (s Store) LessonCount() int {
	b, err := os.ReadFile(filepath.Join(s.Dir, LessonsFile))
	if err != nil {
		return 0
	}
	n := 0
	for _, l := range splitLines(string(b)) {
		if isLesson(l) {
			n++
		}
	}
	return n
}

func (s Store) Summary() string {
	b, err := os.ReadFile(filepath.Join(s.Dir, SummaryFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(Redact(string(b)))
}

func (s Store) Clear() error {
	release, err := s.lock()
	if err != nil {
		return err
	}
	release()
	return os.RemoveAll(s.Dir)
}

func (s Store) lock() (func(), error) {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(s.Dir, lockFile)
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		st, serr := os.Stat(path)
		if serr != nil || time.Since(st.ModTime()) < lockStale {
			return nil, ErrBusy
		}
		_ = os.Remove(path)
	}
	return nil, ErrBusy
}

func isLesson(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "- ")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func normalize(text string, maxChars int) string {
	return boundRunes(strings.TrimSpace(Redact(neutralize(text))), maxChars)
}

func neutralize(text string) string {
	var b strings.Builder
	tildes := 0
	for _, r := range text {
		switch {
		case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
			r = ' '
		case r == '<' || r == '>' || r == '`':
			continue
		}
		if r == '~' {
			tildes++
			if tildes > 1 {
				continue
			}
		} else {
			tildes = 0
		}
		b.WriteRune(r)
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func boundRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func writeFile(path string, data string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.WriteString(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}
