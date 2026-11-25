package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

type writeArgs struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type Write struct {
	CWD        string
	Workspaces []string
	State      *FileState
}

func (t *Write) Name() string            { return "write" }
func (t *Write) Description() string     { return writeDescription }
func (t *Write) Schema() json.RawMessage { return json.RawMessage(writeSchema) }

func (t *Write) IsReadOnly() bool                       { return false }
func (t *Write) IsConcurrencySafe(json.RawMessage) bool { return false }

func (t *Write) Approval(input json.RawMessage) agent.Approval {
	return fileApproval(pathOf(input))
}

func (t *Write) ReviewSpecs(input json.RawMessage) []permission.Spec {
	return pathSpecs(t.CWD, t.Workspaces, pathOf(input))
}

func (t *Write) Metadata(input json.RawMessage) agent.Metadata {
	return agent.Metadata{Destructive: true, MaxResultSize: writeResultCap}
}

func (t *Write) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[writeArgs](raw, "write")
	if bad != nil {
		return *bad
	}
	if a.Path == "" {
		return fail("write: path is required")
	}
	if t.State == nil {
		return fail("write: " + errNoState)
	}
	if problem := partialContent(a.Content); problem != "" {
		return fail("write: " + problem)
	}

	full, err := resolve(rootsOf(t.CWD, t.Workspaces, tc), a.Path)
	if err != nil {
		return fail("write: " + err.Error())
	}

	data := []byte(a.Content)
	perm := os.FileMode(0o644)
	info, err := os.Stat(full)
	existed := err == nil
	switch {
	case existed && !info.Mode().IsRegular():
		return fail(fmt.Sprintf("write: %q is not a regular file", a.Path))
	case existed:
		if err := t.State.verify(full, info); err != nil {
			return fail("write: " + err.Error())
		}
		old, err := os.ReadFile(full)
		if err != nil {
			return fail(fmt.Sprintf("write: cannot read %q: %v", a.Path, err))
		}
		data = []byte(matchFormat(string(old), a.Content))
		perm = info.Mode().Perm()
	case !errors.Is(err, fs.ErrNotExist):
		return fail(fmt.Sprintf("write: cannot stat %q: %v", a.Path, err))
	}

	if err := ctx.Err(); err != nil {
		return fail("write: " + err.Error())
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fail("write: " + err.Error())
	}
	if err := writeFileAtomic(full, data, perm); err != nil {
		return fail(fmt.Sprintf("write: cannot write %q: %v", a.Path, err))
	}
	if after, err := os.Stat(full); err == nil {
		t.State.record(full, after)
	}

	verb := "Created"
	if existed {
		verb = "Overwrote"
	}
	return ok(fmt.Sprintf("%s %s (%d lines)", verb, a.Path, countLines(a.Content)))
}

func fileApproval(p string) agent.Approval {
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.EqualFold(seg, ".git") {
			return agent.Approval{
				Tier:        agent.TierWrite,
				ForcePrompt: true,
				Reason:      "writing inside .git can run code through hooks or config",
			}
		}
	}
	return agent.Approval{Tier: agent.TierWrite}
}

func partialContent(content string) string {
	text := toLF(content)
	if strings.HasPrefix(strings.TrimSpace(lastLine(text)), readNoticePrefix) {
		return "content ends with read's truncation notice, so it is only part of the file; read the rest with offset before writing"
	}
	if strings.Contains(text, readLineCutMarker) {
		return "content contains a line that read cut short (" + readLineCutMarker + "); read the full line before writing"
	}
	if numberedLines(text) {
		return "content still carries read's line-number prefixes; remove them before writing"
	}
	return ""
}

func lastLine(text string) string {
	trimmed := strings.TrimRight(text, " \t\n")
	if i := strings.LastIndexByte(trimmed, '\n'); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

func numberedLines(text string) bool {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 2 {
		return false
	}
	prev := 0
	for i, l := range lines {
		tab := strings.IndexByte(l, '\t')
		if tab < 6 {
			return false
		}
		n, err := strconv.Atoi(strings.TrimLeft(l[:tab], " "))
		if err != nil || (i > 0 && n != prev+1) {
			return false
		}
		prev = n
	}
	return true
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

func matchFormat(old, content string) string {
	bom, body := splitBOM(old)
	_, text := splitBOM(content)
	return bom + fromLF(toLF(text), lineEnding(body))
}

func splitBOM(s string) (string, string) {
	if strings.HasPrefix(s, utf8BOM) {
		return utf8BOM, s[len(utf8BOM):]
	}
	return "", s
}

func lineEnding(s string) string {
	if i := strings.IndexByte(s, '\n'); i > 0 && s[i-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func toLF(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

func fromLF(s, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".omp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		if direct := os.WriteFile(path, data, perm); direct != nil {
			return fmt.Errorf("%w (direct write also failed: %v)", err, direct)
		}
	}
	return nil
}

const writeSchema = `{
  "type": "object",
  "properties": {
    "path":    {"type": "string", "description": "File path relative to the workspace root. Missing parent directories are created."},
    "content": {"type": "string", "description": "The complete new content of the file."}
  },
  "required": ["path", "content"],
  "additionalProperties": false
}`

const writeDescription = `Create a file, or replace the whole content of an existing one.
An existing file must have been read in this session and not changed since; its line endings and byte order mark are kept.
Prefer edit for changing part of a file.`

var _ agent.Tool = (*Write)(nil)
var _ permission.Reviewable = (*Write)(nil)
