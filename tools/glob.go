package tools

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

const (
	globDefaultLimit   = 200
	globMaxLimit       = 200
	globMaxCollect     = 2000
	globDefaultTimeout = 5 * time.Second
)

type globArgs struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

type Glob struct {
	CWD        string
	Workspaces []string
	Timeout    time.Duration
	Denied     func(rel string) bool
}

func (t *Glob) Name() string            { return "glob" }
func (t *Glob) Description() string     { return globDescription }
func (t *Glob) Schema() json.RawMessage { return json.RawMessage(globSchema) }

func (t *Glob) IsReadOnly() bool                       { return true }
func (t *Glob) IsConcurrencySafe(json.RawMessage) bool { return true }
func (t *Glob) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Glob) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: globResultCap}
}

func (t *Glob) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	a, bad := decode[globArgs](raw, "glob")
	if bad != nil {
		return *bad
	}
	if a.Pattern == "" {
		return fail("glob: pattern is required")
	}

	roots := rootsOf(t.CWD, t.Workspaces, tc)
	root, err := resolve(roots, "")
	if err != nil {
		return fail("glob: " + err.Error())
	}
	base := root
	if a.Path != "" {
		if base, err = resolve(roots, a.Path); err != nil {
			return fail("glob: " + err.Error())
		}
	}
	info, err := os.Stat(base)
	if err != nil {
		return fail("glob: " + err.Error())
	}
	if !info.IsDir() {
		return fail("glob: " + a.Path + " is not a directory")
	}

	limit := a.Limit
	if limit <= 0 {
		limit = globDefaultLimit
	}
	if limit > globMaxLimit {
		limit = globMaxLimit
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = globDefaultTimeout
	}

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var found []globMatch
	capped := false

	walkErr := walkFiles(cctx, root, base, t.denies, func(rel string, d os.DirEntry) bool {
		if !matchPath(a.Pattern, rel) {
			return true
		}
		mi, err := d.Info()
		if err != nil {
			return true
		}
		found = append(found, globMatch{rel: rel, mtime: mi.ModTime()})
		if len(found) >= globMaxCollect {
			capped = true
			return false
		}
		return true
	})

	if err := ctx.Err(); err != nil {
		return fail("glob: " + err.Error())
	}
	timedOut := cctx.Err() != nil
	if walkErr != nil && !timedOut {
		return fail("glob: " + walkErr.Error())
	}

	sort.Slice(found, func(i, j int) bool {
		if found[i].mtime.Equal(found[j].mtime) {
			return found[i].rel < found[j].rel
		}
		return found[i].mtime.After(found[j].mtime)
	})

	shown := found
	overLimit := 0
	if len(shown) > limit {
		overLimit = len(shown) - limit
		shown = shown[:limit]
	}

	var b strings.Builder
	for _, m := range shown {
		b.WriteString(m.rel)
		b.WriteByte('\n')
	}

	switch {
	case timedOut:
		b.WriteString("\n... (search timed out after ")
		b.WriteString(timeout.String())
		b.WriteString("; results are partial and are NOT proof of absence — narrow path or pattern)\n")
	case overLimit > 0 || capped:
		b.WriteString("\n... (truncated; narrow the pattern, raise limit, or scope with path)\n")
	}

	if len(shown) == 0 && !timedOut {
		return ok("(no files matched)")
	}
	return ok(b.String())
}

type globMatch struct {
	rel   string
	mtime time.Time
}

func (t *Glob) denies(rel string) bool {
	return t.Denied != nil && t.Denied(rel)
}

func walkFiles(ctx context.Context, root, base string, denied func(string) bool, fn func(rel string, d os.DirEntry) bool) error {
	gi := loadGitignore(root)
	return filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if denied != nil && denied(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if gi.ignored(rel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !fn(rel, d) {
			return filepath.SkipAll
		}
		return nil
	})
}

func matchPath(pattern, rel string) bool {
	p := strings.TrimPrefix(pattern, "./")
	if !strings.ContainsAny(p, `/\`) {
		ok, _ := filepath.Match(p, pathBase(rel))
		return ok
	}
	return matchSegments(strings.Split(filepath.ToSlash(p), "/"), strings.Split(rel, "/"))
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, _ := filepath.Match(pat[0], name[0])
	return ok && matchSegments(pat[1:], name[1:])
}

func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

type ignoreRule struct {
	pattern   string
	negate    bool
	dirOnly   bool
	anchored  bool
	matchFull bool
}

type gitignore struct {
	rules          []ignoreRule
	hasSegmentRule bool
}

func loadGitignore(root string) *gitignore {
	gi := &gitignore{}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return gi
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), " ")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		if strings.HasPrefix(line, "/") {
			r.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if line == "" {
			continue
		}
		r.pattern = line
		r.matchFull = r.anchored || strings.Contains(line, "/")
		if !r.matchFull {
			gi.hasSegmentRule = true
		}
		gi.rules = append(gi.rules, r)
	}
	return gi
}

func (g *gitignore) ignored(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	base := relPath
	if i := strings.LastIndex(relPath, "/"); i >= 0 {
		base = relPath[i+1:]
	}
	var segs []string
	if g.hasSegmentRule {
		segs = strings.Split(relPath, "/")
	}
	result := false
	for _, r := range g.rules {
		if r.dirOnly && !isDir {
			continue
		}
		var match bool
		if r.matchFull {
			match, _ = filepath.Match(r.pattern, relPath)
		} else {
			match, _ = filepath.Match(r.pattern, base)
			if !match {
				for _, seg := range segs {
					if ok, _ := filepath.Match(r.pattern, seg); ok {
						match = true
						break
					}
				}
			}
		}
		if match {
			result = !r.negate
		}
	}
	return result
}

const globSchema = `{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Glob pattern. Without a slash it matches file names (e.g. *.go); with a slash it matches relative paths and ** crosses directories (e.g. src/**/*.ts)."},
    "path":    {"type": "string", "description": "Optional subdirectory to scope the search, relative to the workspace root."},
    "limit":   {"type": "integer", "description": "Maximum results. Defaults to 200, capped at 200."}
  },
  "required": ["pattern"],
  "additionalProperties": false
}`

const globDescription = `Find files by glob pattern under the workspace, newest first.
Directories are never returned. Skips .git and .gitignore'd paths. Results may be truncated or partial on timeout; the notice says which.`

var _ agent.Tool = (*Glob)(nil)
