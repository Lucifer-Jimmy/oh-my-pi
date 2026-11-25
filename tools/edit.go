package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

const editMaxListed = 8

type editArgs struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type Edit struct {
	CWD        string
	Workspaces []string
	State      *FileState
}

func (t *Edit) Name() string            { return "edit" }
func (t *Edit) Description() string     { return editDescription }
func (t *Edit) Schema() json.RawMessage { return json.RawMessage(editSchema) }

func (t *Edit) IsReadOnly() bool                       { return false }
func (t *Edit) IsConcurrencySafe(json.RawMessage) bool { return false }

func (t *Edit) Approval(input json.RawMessage) agent.Approval {
	return fileApproval(pathOf(input))
}

func (t *Edit) ReviewSpecs(input json.RawMessage) []permission.Spec {
	return pathSpecs(t.CWD, t.Workspaces, pathOf(input))
}

func (t *Edit) Metadata(input json.RawMessage) agent.Metadata {
	return agent.Metadata{Destructive: true, MaxResultSize: editResultCap}
}

func (t *Edit) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[editArgs](raw, "edit")
	if bad != nil {
		return *bad
	}
	if a.Path == "" {
		return fail("edit: path is required")
	}
	if t.State == nil {
		return fail("edit: " + errNoState)
	}
	_, oldText := splitBOM(toLF(a.OldString))
	_, newText := splitBOM(toLF(a.NewString))
	if oldText == "" {
		return fail("edit: old_string must not be empty; use write to create a file or replace all of it")
	}
	if oldText == newText {
		return fail("edit: old_string and new_string are identical; nothing to change")
	}

	full, err := resolve(rootsOf(t.CWD, t.Workspaces, tc), a.Path)
	if err != nil {
		return fail("edit: " + err.Error())
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fail(fmt.Sprintf("edit: file %q does not exist; use write to create it", a.Path))
		}
		return fail(fmt.Sprintf("edit: cannot stat %q: %v", a.Path, err))
	}
	if !info.Mode().IsRegular() {
		return fail(fmt.Sprintf("edit: %q is not a regular file", a.Path))
	}
	if err := t.State.verify(full, info); err != nil {
		return fail("edit: " + err.Error())
	}

	data, err := os.ReadFile(full)
	if err != nil {
		return fail(fmt.Sprintf("edit: cannot read %q: %v", a.Path, err))
	}
	bom, body := splitBOM(string(data))
	ending := lineEnding(body)
	text := toLF(body)

	at := offsets(text, oldText)
	switch {
	case len(at) == 0:
		return fail("edit: " + notFound(a.Path, text, oldText))
	case len(at) > 1 && !a.ReplaceAll:
		return fail(fmt.Sprintf("edit: old_string matches %d places in %s (lines %s); add surrounding lines to make it unique, or set replace_all",
			len(at), a.Path, lineNumbers(text, at)))
	}

	var updated string
	if a.ReplaceAll {
		updated = strings.ReplaceAll(text, oldText, newText)
	} else {
		updated = text[:at[0]] + newText + text[at[0]+len(oldText):]
	}

	if err := ctx.Err(); err != nil {
		return fail("edit: " + err.Error())
	}
	if err := writeFileAtomic(full, []byte(bom+fromLF(updated, ending)), info.Mode().Perm()); err != nil {
		return fail(fmt.Sprintf("edit: cannot write %q: %v", a.Path, err))
	}
	if after, err := os.Stat(full); err == nil {
		t.State.record(full, after)
	}
	return ok(fmt.Sprintf("Edited %s: %d replacement(s) at line %s", a.Path, len(at), lineNumbers(text, at)))
}

func offsets(s, sub string) []int {
	var out []int
	for i := 0; ; {
		j := strings.Index(s[i:], sub)
		if j < 0 {
			return out
		}
		out = append(out, i+j)
		i += j + len(sub)
	}
}

func lineNumbers(text string, at []int) string {
	var lines []int
	for _, off := range at {
		lines = append(lines, strings.Count(text[:off], "\n")+1)
		if len(lines) == editMaxListed {
			break
		}
	}
	s := joinInts(lines)
	if len(at) > len(lines) {
		s += fmt.Sprintf(" and %d more", len(at)-len(lines))
	}
	return s
}

func joinInts(ns []int) string {
	parts := make([]string, len(ns))
	for i, n := range ns {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ", ")
}

var typographic = strings.NewReplacer("“", `"`, "”", `"`, "‘", "'", "’", "'", "–", "-", "—", "-", "…", "...")

func notFound(path, text, old string) string {
	probe := firstLine(old)
	if probe != "" {
		if lines := matchingLines(text, probe, false); len(lines) > 0 {
			return fmt.Sprintf("old_string not found in %s, but its first line appears at line %s with different indentation or neighbouring lines; read those lines and copy them exactly",
				path, joinInts(lines))
		}
		if lines := matchingLines(text, probe, true); len(lines) > 0 {
			return fmt.Sprintf("old_string not found in %s; its first line matches line %s only after typographic quotes or dashes are made plain, copy the characters exactly",
				path, joinInts(lines))
		}
	}
	return fmt.Sprintf("old_string not found in %s; read the file again and copy the text exactly, including whitespace", path)
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

func matchingLines(text, probe string, plain bool) []int {
	if plain {
		probe = typographic.Replace(probe)
	}
	var out []int
	for i, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if plain {
			l = typographic.Replace(l)
		}
		if l == probe {
			out = append(out, i+1)
			if len(out) == editMaxListed {
				break
			}
		}
	}
	return out
}

const editSchema = `{
  "type": "object",
  "properties": {
    "path":        {"type": "string", "description": "File path relative to the workspace root."},
    "old_string":  {"type": "string", "description": "Exact text to replace, including indentation. Include enough surrounding lines to make it unique."},
    "new_string":  {"type": "string", "description": "Replacement text."},
    "replace_all": {"type": "boolean", "description": "Replace every occurrence instead of requiring a unique match."}
  },
  "required": ["path", "old_string", "new_string"],
  "additionalProperties": false
}`

const editDescription = `Replace an exact string in a file that has been read in this session.
old_string must match the file exactly and be unique unless replace_all is set; the error lists candidate line numbers when it is not.
The file's line endings and byte order mark are kept. Use write to create a file.`

var _ agent.Tool = (*Edit)(nil)
var _ permission.Reviewable = (*Edit)(nil)
