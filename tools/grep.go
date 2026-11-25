package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

const (
	grepMaxMatches     = 1000
	grepMaxFiles       = 50000
	grepScanBufMax     = 1024 * 1024
	grepBinaryProbe    = 8192
	grepDefaultTimeout = 10 * time.Second
)

type grepArgs struct {
	Pattern    string `json:"pattern"`
	Path       string `json:"path,omitempty"`
	Glob       string `json:"glob,omitempty"`
	IgnoreCase bool   `json:"ignore_case,omitempty"`
	Fixed      bool   `json:"fixed_string,omitempty"`
	OutputMode string `json:"output_mode,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type Grep struct {
	CWD        string
	Workspaces []string
	Timeout    time.Duration
	Denied     func(rel string) bool
}

func (t *Grep) Name() string            { return "grep" }
func (t *Grep) Description() string     { return grepDescription }
func (t *Grep) Schema() json.RawMessage { return json.RawMessage(grepSchema) }

func (t *Grep) IsReadOnly() bool                       { return true }
func (t *Grep) IsConcurrencySafe(json.RawMessage) bool { return true }
func (t *Grep) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Grep) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: grepResultCap}
}

type fileMatch struct {
	path  string
	lines []matchedLine
	err   string
}

type matchedLine struct {
	n    int
	text string
}

type grepEngine struct {
	re         *regexp.Regexp
	fixed      []byte
	ignoreCase bool
}

func (e *grepEngine) match(line []byte) bool {
	switch {
	case e.re != nil:
		return e.re.Match(line)
	case e.ignoreCase:
		return bytes.Contains(bytes.ToLower(line), e.fixed)
	default:
		return bytes.Contains(line, e.fixed)
	}
}

func (t *Grep) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[grepArgs](raw, "grep")
	if bad != nil {
		return *bad
	}
	if a.Pattern == "" {
		return fail("grep: pattern is required")
	}

	mode := a.OutputMode
	if mode == "" {
		mode = "content"
	}
	switch mode {
	case "content", "files_with_matches", "count":
	default:
		return fail("grep: unknown output_mode " + mode)
	}

	eng, err := newGrepEngine(a)
	if err != nil {
		return fail("grep: " + err.Error())
	}

	roots := rootsOf(t.CWD, t.Workspaces, tc)
	root, err := resolve(roots, "")
	if err != nil {
		return fail("grep: " + err.Error())
	}
	base := root
	if a.Path != "" {
		if base, err = resolve(roots, a.Path); err != nil {
			return fail("grep: " + err.Error())
		}
	}
	info, err := os.Stat(base)
	if err != nil {
		return fail("grep: " + err.Error())
	}

	limit := a.Limit
	if limit <= 0 || limit > grepMaxMatches {
		limit = grepMaxMatches
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = grepDefaultTimeout
	}

	files, filesCapped := t.collect(ctx, root, base, info.IsDir(), a.Glob)
	if len(files) == 0 {
		return ok("(no files searched)")
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		results = make([]fileMatch, len(files))
		matched int64
		skipped int64
		stop    atomic.Bool
		next    int64 = -1
	)
	workers := min(runtime.NumCPU(), len(files))

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Go(func() {
			for {
				i := int(atomic.AddInt64(&next, 1))
				if i >= len(files) || stop.Load() || cctx.Err() != nil {
					return
				}
				fm, binary := scanFile(files[i], root, eng, limit)
				if binary {
					atomic.AddInt64(&skipped, 1)
					continue
				}
				if len(fm.lines) == 0 {
					continue
				}
				results[i] = fm
				if atomic.AddInt64(&matched, int64(len(fm.lines))) >= int64(limit) {
					stop.Store(true)
					return
				}
			}
		})
	}
	wg.Wait()

	if err := ctx.Err(); err != nil {
		return fail("grep: " + err.Error())
	}
	timedOut := cctx.Err() != nil

	var hits []fileMatch
	var failed int
	for _, fm := range results {
		if fm.err != "" {
			failed++
		}
		if fm.path != "" && len(fm.lines) > 0 {
			hits = append(hits, fm)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].path < hits[j].path })

	out, shown := formatGrep(mode, hits, limit)

	var notes []string
	if timedOut {
		notes = append(notes, fmt.Sprintf("search timed out after %s; results are partial and are NOT proof of absence", timeout))
	}
	if matched >= int64(limit) {
		notes = append(notes, fmt.Sprintf("truncated at %d matches; narrow the pattern or scope with path", limit))
	}
	if filesCapped {
		notes = append(notes, fmt.Sprintf("file list capped at %d; results may be incomplete", grepMaxFiles))
	}
	if skipped > 0 {
		notes = append(notes, fmt.Sprintf("%d binary files skipped", skipped))
	}
	if failed > 0 {
		notes = append(notes, fmt.Sprintf("%d files could not be read fully (unreadable or oversized lines)", failed))
	}

	if shown == 0 && !timedOut {
		if len(notes) == 0 {
			return ok("(no matches)")
		}
		return ok("(no matches; " + strings.Join(notes, "; ") + ")")
	}
	for _, n := range notes {
		out += "\n... (" + n + ")\n"
	}
	return ok(out)
}

func formatGrep(mode string, hits []fileMatch, limit int) (string, int) {
	var b strings.Builder
	shown := 0
	switch mode {
	case "files_with_matches":
		for _, fm := range hits {
			if shown >= limit {
				break
			}
			b.WriteString(fm.path)
			b.WriteByte('\n')
			shown++
		}
	case "count":
		for _, fm := range hits {
			if shown >= limit {
				break
			}
			fmt.Fprintf(&b, "%s:%d\n", fm.path, len(fm.lines))
			shown++
		}
	default:
	Content:
		for _, fm := range hits {
			for _, l := range fm.lines {
				if shown >= limit {
					break Content
				}
				fmt.Fprintf(&b, "%s:%d:%s\n", fm.path, l.n, l.text)
				shown++
			}
		}
	}
	return b.String(), shown
}

func newGrepEngine(a grepArgs) (*grepEngine, error) {
	if a.Fixed {
		s := a.Pattern
		if a.IgnoreCase {
			s = strings.ToLower(s)
		}
		return &grepEngine{fixed: []byte(s), ignoreCase: a.IgnoreCase}, nil
	}
	p := a.Pattern
	if a.IgnoreCase {
		p = "(?i)" + p
	}
	re, err := regexp.Compile(p)
	if err != nil {
		return nil, err
	}
	return &grepEngine{re: re}, nil
}

func (t *Grep) collect(ctx context.Context, root, base string, isDir bool, glob string) ([]string, bool) {
	if !isDir {
		rel, err := filepath.Rel(root, base)
		if err != nil {
			rel = base
		}
		rel = filepath.ToSlash(rel)
		if t.denies(rel) {
			return nil, false
		}
		return []string{rel}, false
	}
	var files []string
	capped := false
	walkFiles(ctx, root, base, t.denies, func(rel string, d os.DirEntry) bool {
		if glob != "" && !matchPath(glob, rel) {
			return true
		}
		files = append(files, rel)
		if len(files) >= grepMaxFiles {
			capped = true
			return false
		}
		return true
	})
	sort.Strings(files)
	return files, capped
}

func (t *Grep) denies(rel string) bool {
	return t.Denied != nil && t.Denied(rel)
}

func scanFile(rel, root string, eng *grepEngine, limit int) (fileMatch, bool) {
	fm := fileMatch{path: rel}
	full := filepath.Join(root, filepath.FromSlash(rel))

	f, err := os.Open(full)
	if err != nil {
		return fm, false
	}
	defer f.Close()

	head := make([]byte, grepBinaryProbe)
	n, _ := f.Read(head)
	if bytes.IndexByte(head[:n], 0) >= 0 {
		return fm, true
	}
	if _, err := f.Seek(0, 0); err != nil {
		return fm, false
	}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), grepScanBufMax)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		if !eng.match(sc.Bytes()) {
			continue
		}
		text := sc.Text()
		if len(text) > readMaxLineLen {
			text = text[:readMaxLineLen] + readLineCutMarker
		}
		fm.lines = append(fm.lines, matchedLine{n: lineNo, text: text})
		if len(fm.lines) >= limit {
			break
		}
	}
	if err := sc.Err(); err != nil {
		fm.err = err.Error()
	}
	return fm, false
}

const grepSchema = `{
  "type": "object",
  "properties": {
    "pattern":      {"type": "string", "description": "Regular expression (RE2 syntax) to search for."},
    "path":         {"type": "string", "description": "Optional file or directory to search, relative to the workspace root. Defaults to the whole workspace."},
    "glob":         {"type": "string", "description": "Only search files matching this glob, e.g. *.go."},
    "ignore_case":  {"type": "boolean", "description": "Case-insensitive match."},
    "fixed_string": {"type": "boolean", "description": "Treat pattern as a literal string instead of a regular expression."},
    "output_mode":  {"type": "string", "enum": ["content", "files_with_matches", "count"], "description": "content (default, path:line:text), files_with_matches, or count."},
    "limit":        {"type": "integer", "description": "Maximum matches to return. Defaults to 1000."}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`

const grepDescription = `Search file contents by regular expression under the workspace.
Skips .git, .gitignore'd paths and binary files. Results are grouped by path, sorted, and capped at 1000 matches by default.`

var _ agent.Tool = (*Grep)(nil)
