package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

const (
	readMaxLines   = 2000
	readMaxLineLen = 2000
	readMaxBytes   = 50 * 1024

	readScanBufInit = 64 * 1024
	readScanBufMax  = 16 * 1024 * 1024

	readMaxDirEntries = 500

	readLineCutMarker = "... (line truncated)"
	readNoticePrefix  = "... (output truncated: showed lines "

	utf8BOM = string(rune(0xFEFF))

	errNoState = "the tool has no FileState; give read, write and edit the same tools.NewFileState()"
)

type readArgs struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type Read struct {
	CWD        string
	Workspaces []string
	State      *FileState
}

func (t *Read) Name() string            { return "read" }
func (t *Read) Description() string     { return readDescription }
func (t *Read) Schema() json.RawMessage { return json.RawMessage(readSchema) }

func (t *Read) IsReadOnly() bool                       { return true }
func (t *Read) IsConcurrencySafe(json.RawMessage) bool { return true }
func (t *Read) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Read) ReviewSpecs(input json.RawMessage) []permission.Spec {
	return pathSpecs(t.CWD, t.Workspaces, pathOf(input))
}

func (t *Read) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: readResultCap}
}

func (t *Read) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[readArgs](raw, "read")
	if bad != nil {
		return *bad
	}
	if a.Path == "" {
		return fail("read: path is required")
	}

	full, err := resolve(rootsOf(t.CWD, t.Workspaces, tc), a.Path)
	if err != nil {
		return fail("read: " + err.Error())
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fail(fmt.Sprintf("read: file %q does not exist", a.Path))
		}
		return fail(fmt.Sprintf("read: cannot stat %q: %v", a.Path, err))
	}
	if info.IsDir() {
		return t.listDir(full)
	}
	if err := ctx.Err(); err != nil {
		return fail("read: " + err.Error())
	}

	f, err := os.Open(full)
	if err != nil {
		return fail(fmt.Sprintf("read: cannot open %q: %v", a.Path, err))
	}
	defer f.Close()

	text, notice, err := numbered(f, a.Offset, a.Limit)
	if err != nil {
		return fail(fmt.Sprintf("read: %v", err))
	}
	t.State.record(full, info)
	if notice != "" {
		text += notice
	}
	return ok(text)
}

func rootsOf(cwd string, workspaces []string, tc agent.ToolContext) []string {
	if len(workspaces) > 0 {
		return workspaces
	}
	if cwd != "" {
		return []string{cwd}
	}
	if tc != nil && tc.CWD() != "" {
		return []string{tc.CWD()}
	}
	wd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return []string{wd}
}

func runRoots(cwd string, workspaces []string, tc agent.ToolContext) []string {
	if tc != nil {
		if w := tc.Workspaces(); len(w) > 0 {
			return w
		}
		if d := tc.CWD(); d != "" {
			return []string{d}
		}
	}
	return rootsOf(cwd, workspaces, tc)
}

func (t *Read) listDir(full string) agent.ToolResult {
	entries, err := os.ReadDir(full)
	if err != nil {
		return fail("read: " + err.Error())
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var b strings.Builder
	n := 0
	for _, e := range entries {
		if n >= readMaxDirEntries {
			fmt.Fprintf(&b, "... (%d more entries)\n", len(entries)-n)
			break
		}
		name := e.Name()
		if e.IsDir() {
			name += string(filepath.Separator)
		}
		b.WriteString(name)
		b.WriteByte('\n')
		n++
	}
	if b.Len() == 0 {
		return ok("(empty directory)")
	}
	return ok(b.String())
}

func numbered(r io.Reader, offset, limit int) (string, string, error) {
	if offset < 1 {
		offset = 1
	}
	maxLines := limit
	if maxLines <= 0 || maxLines > readMaxLines {
		maxLines = readMaxLines
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, readScanBufInit), readScanBufMax)

	var b strings.Builder
	lineNo := 0
	shown := 0
	size := 0
	cut := false

	for sc.Scan() {
		lineNo++
		if lineNo < offset {
			continue
		}
		if cut || shown >= maxLines || size >= readMaxBytes {
			cut = true
			continue
		}
		line := sc.Text()
		if lineNo == 1 {
			line = strings.TrimPrefix(line, utf8BOM)
		}
		if len(line) > readMaxLineLen {
			line = line[:readMaxLineLen] + readLineCutMarker
		}
		seg := fmt.Sprintf("%6d\t%s\n", lineNo, line)
		b.WriteString(seg)
		size += len(seg)
		shown++
	}
	if err := sc.Err(); err != nil {
		return "", "", err
	}
	if !cut {
		return b.String(), "", nil
	}
	last := offset + shown - 1
	return b.String(), fmt.Sprintf("\n%s%d-%d of %d; use offset=%d to continue)\n",
		readNoticePrefix, offset, last, lineNo, last+1), nil
}

type FileState struct {
	mu    sync.Mutex
	reads map[string]fileStamp
}

type fileStamp struct {
	modTime time.Time
	size    int64
}

func NewFileState() *FileState {
	return &FileState{reads: make(map[string]fileStamp)}
}

func (s *FileState) record(full string, info os.FileInfo) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads[stateKey(full)] = fileStamp{modTime: info.ModTime(), size: info.Size()}
}

func (s *FileState) verify(full string, info os.FileInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stamp, ok := s.reads[stateKey(full)]
	if !ok {
		return errors.New("the file has not been read in this session; read it before changing it")
	}
	if !stamp.modTime.Equal(info.ModTime()) || stamp.size != info.Size() {
		return errors.New("the file changed on disk since it was last read; read it again before changing it")
	}
	return nil
}

func stateKey(full string) string {
	k := realPath(filepath.Clean(full))
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	return k
}

func resolve(roots []string, p string) (string, error) {
	expanded, err := expandHome(p)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("cannot determine working directory: %w", err)
		}
		roots = []string{wd}
	}

	var full string
	if filepath.IsAbs(expanded) {
		full = filepath.Clean(expanded)
	} else {
		full = filepath.Join(roots[0], expanded)
	}
	real := realPath(full)

	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(realPath(absRoot), real)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return full, nil
		}
	}
	return "", fmt.Errorf("path %q is outside the workspace", p)
}

func realPath(full string) string {
	if r, err := filepath.EvalSymlinks(full); err == nil {
		return r
	}
	dir, rest := full, ""
	for {
		parent := filepath.Dir(dir)
		rest = filepath.Join(filepath.Base(dir), rest)
		if parent == dir {
			return full
		}
		if r, err := filepath.EvalSymlinks(parent); err == nil {
			return filepath.Join(r, rest)
		}
		dir = parent
	}
}

func expandHome(p string) (string, error) {
	if p != "~" && !strings.HasPrefix(p, "~/") && !strings.HasPrefix(p, `~\`) {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve ~: %w", err)
	}
	if p == "~" {
		return home, nil
	}
	return filepath.Join(home, p[2:]), nil
}

func pathOf(input json.RawMessage) string {
	var a struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(input, &a) != nil {
		return ""
	}
	return a.Path
}

func pathSpecs(cwd string, workspaces []string, p string) []permission.Spec {
	if p == "" {
		return []permission.Spec{{Kind: permission.KindPath}}
	}
	roots := rootsOf(cwd, workspaces, nil)
	full, err := resolve(roots, p)
	if err != nil {
		return []permission.Spec{{Kind: permission.KindPath}}
	}
	for _, root := range roots {
		rel, err := filepath.Rel(root, full)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return []permission.Spec{{Kind: permission.KindPath, Value: filepath.ToSlash(rel)}}
		}
	}
	return []permission.Spec{{Kind: permission.KindPath, Value: filepath.ToSlash(full)}}
}

func decode[T any](raw json.RawMessage, tool string) (T, *agent.ToolResult) {
	var v T
	if len(strings.TrimSpace(string(raw))) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		r := fail(fmt.Sprintf("%s: invalid arguments: %v", tool, err))
		return v, &r
	}
	return v, nil
}

func ok(text string) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}}
}

func fail(text string) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{llm.TextBlock(text)}, IsError: true}
}

const readSchema = `{
  "type": "object",
  "properties": {
    "path":   {"type": "string", "description": "File or directory path, relative to the workspace root."},
    "offset": {"type": "integer", "description": "1-based line to start reading from. Defaults to 1."},
    "limit":  {"type": "integer", "description": "Maximum number of lines to return. Defaults to 2000."}
  },
  "required": ["path"],
  "additionalProperties": false
}`

const readDescription = `Read a text file's contents, or list a directory.
Output is line-numbered. Large files are truncated; the notice tells you how to continue with offset.`
