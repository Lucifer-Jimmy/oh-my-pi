package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	FileVersion = 1
	FileSuffix  = ".jsonl"
)

type EntryType string

const (
	EntryMessage    EntryType = "message"
	EntryCompaction EntryType = "compaction"
	EntryTitle      EntryType = "title"
	EntryGoal       EntryType = "goal"
)

type header struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp int64  `json:"timestamp"`
	CWD       string `json:"cwd,omitempty"`
	Model     string `json:"model,omitempty"`
}

type Entry struct {
	Type       EntryType       `json:"type"`
	ID         string          `json:"id"`
	ParentID   string          `json:"parentId"`
	Timestamp  int64           `json:"timestamp"`
	Message    *llm.Message    `json:"message,omitempty"`
	Compaction *Compaction     `json:"compaction,omitempty"`
	Title      *string         `json:"title,omitempty"`
	Goal       json.RawMessage `json:"goal,omitempty"`
}

type Report struct {
	Messages    int
	Compactions int
	Dropped     int
	Synthetic   int
	Repaired    bool
}

func (r Report) String() string {
	var parts []string
	if r.Repaired {
		parts = append(parts, "session repaired")
	}
	if r.Dropped > 0 {
		parts = append(parts, fmt.Sprintf("%d damaged entries dropped", r.Dropped))
	}
	if r.Synthetic > 0 {
		parts = append(parts, fmt.Sprintf("%d tool calls marked outcome unknown", r.Synthetic))
	}
	return strings.Join(parts, ", ")
}

type Info struct {
	ID          string    `json:"id"`
	Path        string    `json:"path"`
	Modified    time.Time `json:"modified"`
	Messages    int       `json:"messages"`
	Compactions int       `json:"compactions"`
	Title       string    `json:"title,omitempty"`
}

type Store struct {
	*Memory

	mu     sync.Mutex
	file   *os.File
	id     string
	path   string
	parent string
	count  int
	err    error
	now    func() time.Time
	goal   json.RawMessage
}

var _ Session = (*Store)(nil)

func Create(dir, id, cwd, model string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, filepath.Base(id)+FileSuffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	s := &Store{Memory: NewMemory(nil), file: f, id: id, path: path, now: time.Now}
	h := header{Type: "header", Version: FileVersion, ID: id, Timestamp: s.now().UnixMilli(), CWD: cwd, Model: model}
	if err := s.write(h); err != nil {
		f.Close()
		os.Remove(path)
		return nil, err
	}
	return s, nil
}

func Load(path string) (*Store, Report, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, Report{}, err
	}
	s := &Store{Memory: NewMemory(nil), path: path, now: time.Now}
	r, err := s.replay(b)
	if err != nil {
		return nil, Report{}, fmt.Errorf("%s: %w", path, err)
	}
	s.id = strings.TrimSuffix(filepath.Base(path), FileSuffix)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, Report{}, fmt.Errorf("%s: %w", path, err)
	}
	s.file = f
	return s, r, nil
}

func Read(path string) ([]llm.Message, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s := &Store{Memory: NewMemory(nil), path: path, now: time.Now}
	if _, err := s.replay(b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s.Messages(), nil
}

func (s *Store) replay(b []byte) (Report, error) {
	var r Report
	version := 0
	var msgs []llm.Message
	var comps []Compaction
	lineNo := 0
	for len(b) > 0 {
		var line []byte
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line, b = b[:i], b[i+1:]
		} else {
			line, b = b, nil
		}
		lineNo++
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		last := len(b) == 0
		if version == 0 {
			var h header
			if err := json.Unmarshal(line, &h); err != nil || h.Type != "header" {
				return r, fmt.Errorf("line %d: the file does not start with a session header", lineNo)
			}
			if h.Version > FileVersion {
				return r, fmt.Errorf("line %d: session version %d was written by a newer omp", lineNo, h.Version)
			}
			version = h.Version
			s.id = h.ID
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			if last {
				r.Dropped++
				r.Repaired = true
				continue
			}
			return r, fmt.Errorf("line %d: %w", lineNo, err)
		}
		switch e.Type {
		case EntryMessage:
			if e.Message == nil {
				r.Dropped++
				r.Repaired = true
				s.parent, s.count = e.ID, s.count+1
				continue
			}
			msgs = append(msgs, *e.Message)
		case EntryCompaction:
			if e.Compaction == nil {
				r.Dropped++
				r.Repaired = true
				s.parent, s.count = e.ID, s.count+1
				continue
			}
			comps = append(comps, *e.Compaction)
		case EntryTitle:
		case EntryGoal:
			if len(e.Goal) > 0 {
				s.goal = append(json.RawMessage(nil), e.Goal...)
			}
		default:
			r.Dropped++
			r.Repaired = true
		}
		s.parent = e.ID
		s.count++
	}
	if version == 0 {
		return r, errors.New("the file is empty or has no session header")
	}
	msgs, dropped, synthetic := repair(msgs)
	r.Dropped += dropped
	r.Synthetic += synthetic
	r.Messages = len(msgs)
	r.Compactions = len(comps)
	if dropped > 0 || synthetic > 0 {
		r.Repaired = true
	}
	s.Memory.messages = msgs
	s.Memory.compactions = comps
	return r, nil
}

func repair(msgs []llm.Message) (out []llm.Message, dropped, synthetic int) {
	out = make([]llm.Message, 0, len(msgs)+1)
	var pending []string
	for _, m := range msgs {
		if m.Role == llm.RoleAssistant {
			pending = pending[:0]
		}
		var content []llm.ContentBlock
		removed := false
		for _, b := range m.Content {
			switch b.Type {
			case llm.BlockToolUse:
				if m.Role == llm.RoleAssistant {
					pending = append(pending, b.ID)
				}
			case llm.BlockToolResult:
				matched := false
				for i, id := range pending {
					if id == b.ToolUseID {
						pending = append(pending[:i], pending[i+1:]...)
						matched = true
						break
					}
				}
				if !matched {
					removed = true
					continue
				}
			}
			content = append(content, b)
		}
		if removed {
			dropped++
		}
		if len(m.Content) > 0 && len(content) == 0 {
			continue
		}
		m.Content = content
		out = append(out, m)
	}
	if len(pending) > 0 {
		var blocks []llm.ContentBlock
		for _, id := range pending {
			blocks = append(blocks, llm.ToolResultText(id, outcomeUnknown, true))
		}
		out = append(out, llm.Message{Role: llm.RoleUser, Content: blocks})
		synthetic = len(pending)
	}
	return out, dropped, synthetic
}

const outcomeUnknown = "outcome unknown: omp stopped before this tool call reported its result. " +
	"Re-run it only if the answer is still needed; do not repeat side-effecting commands blindly."

func (s *Store) Append(m llm.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := m.Timestamp
	if ts == 0 {
		ts = s.now().UnixMilli()
		m.Timestamp = ts
	}
	s.Memory.Append(m)
	s.record(Entry{Type: EntryMessage, Timestamp: ts, Message: &m})
}

func (s *Store) AppendCompaction(c Compaction) {
	s.Memory.AppendCompaction(c)
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := c
	s.record(Entry{Type: EntryCompaction, Timestamp: cp.Timestamp, Compaction: &cp})
}

func (s *Store) SetTitle(title string) {
	t := strings.TrimSpace(title)
	if t == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(Entry{Type: EntryTitle, Timestamp: s.now().UnixMilli(), Title: &t})
}

func (s *Store) SetGoal(raw json.RawMessage) {
	if len(raw) == 0 || !json.Valid(raw) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.goal = append(json.RawMessage(nil), raw...)
	s.record(Entry{Type: EntryGoal, Timestamp: s.now().UnixMilli(), Goal: s.goal})
}

func (s *Store) Goal() json.RawMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append(json.RawMessage(nil), s.goal...)
}

func (s *Store) record(e Entry) {
	if s.file == nil || s.err != nil {
		return
	}
	s.count++
	e.ID = fmt.Sprintf("%s-%d", s.id, s.count)
	e.ParentID = s.parent
	s.parent = e.ID
	s.err = s.write(e)
}

func (s *Store) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if _, err := s.file.Write(b); err != nil {
		return err
	}
	return nil
}

func (s *Store) ID() string { return s.id }

func (s *Store) Path() string { return s.path }

func (s *Store) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return s.err
	}
	err := s.file.Sync()
	if cerr := s.file.Close(); err == nil {
		err = cerr
	}
	s.file = nil
	if s.err != nil {
		return s.err
	}
	return err
}

func List(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), FileSuffix) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := describe(path)
		if err != nil {
			continue
		}
		if fi, err := e.Info(); err == nil {
			info.Modified = fi.ModTime()
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

func Latest(dir string) (string, bool, error) {
	list, err := List(dir)
	if err != nil {
		return "", false, err
	}
	for _, info := range list {
		if info.Messages > 0 {
			return info.Path, true, nil
		}
	}
	return "", false, nil
}

func describe(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	info := Info{Path: path, ID: strings.TrimSuffix(filepath.Base(path), FileSuffix)}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	seenHeader := false
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if !seenHeader {
			var h header
			if json.Unmarshal(line, &h) != nil || h.Type != "header" {
				return Info{}, errors.New("not a session file")
			}
			if h.ID != "" {
				info.ID = h.ID
			}
			seenHeader = true
			continue
		}
		var e struct {
			Type    EntryType `json:"type"`
			Title   *string   `json:"title"`
			Message *struct {
				Role    llm.Role `json:"role"`
				Content []struct {
					Type llm.BlockType `json:"type"`
					Text string        `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &e) != nil {
			continue
		}
		switch e.Type {
		case EntryTitle:
			if e.Title != nil && strings.TrimSpace(*e.Title) != "" {
				info.Title = firstLine(*e.Title, 80)
			}
		case EntryMessage:
			info.Messages++
			if info.Title == "" && e.Message != nil && e.Message.Role == llm.RoleUser {
				for _, b := range e.Message.Content {
					if b.Type == llm.BlockText && strings.TrimSpace(b.Text) != "" {
						info.Title = firstLine(b.Text, 80)
						break
					}
				}
			}
		case EntryCompaction:
			info.Compactions++
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return Info{}, err
	}
	return info, nil
}

func firstLine(s string, limit int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if r := []rune(s); len(r) > limit {
		s = string(r[:limit]) + "..."
	}
	return strings.ReplaceAll(s, "\t", " ")
}
