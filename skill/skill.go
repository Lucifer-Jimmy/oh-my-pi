package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	fileName   = "SKILL.md"
	maxBody    = 64 * 1024
	resultCap  = 64 * 1024
	projectDir = ".omp/skills"
	userDir    = ".omp/skills"
)

type Skill struct {
	Name        string
	Description string
	Body        string
	Path        string
}

func Discover(cwd string) []Skill {
	byName := map[string]Skill{}
	for _, dir := range searchDirs(cwd) {
		for _, s := range loadDir(dir) {
			if _, seen := byName[s.Name]; seen {
				continue
			}
			byName[s.Name] = s
		}
	}
	out := make([]Skill, 0, len(byName))
	for _, s := range byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func searchDirs(cwd string) []string {
	var dirs []string
	if cwd != "" {
		dirs = append(dirs, filepath.Join(cwd, filepath.FromSlash(projectDir)))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, filepath.FromSlash(userDir)))
	}
	return dirs
}

func loadDir(dir string) []Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		s, ok := loadFile(filepath.Join(dir, e.Name(), fileName))
		if !ok {
			continue
		}
		if s.Name == "" {
			s.Name = e.Name()
		}
		out = append(out, s)
	}
	return out
}

func loadFile(path string) (Skill, bool) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) > maxBody {
		return Skill{}, false
	}
	meta, body := splitFrontmatter(string(b))
	if meta.Name == "" || strings.TrimSpace(meta.Description) == "" {
		return Skill{}, false
	}
	if strings.TrimSpace(body) == "" {
		return Skill{}, false
	}
	return Skill{Name: meta.Name, Description: meta.Description, Body: body, Path: path}, true
}

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func splitFrontmatter(text string) (frontmatter, string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return frontmatter{}, text
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return frontmatter{}, text
	}
	var meta frontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &meta); err != nil {
		return frontmatter{}, text
	}
	body := rest[end+len("\n---"):]
	body = strings.TrimPrefix(body, "\n")
	return meta, body
}

type Tool struct {
	Skills []Skill
}

type toolArgs struct {
	Name string `json:"name"`
}

func (t *Tool) Name() string            { return "skill" }
func (t *Tool) Description() string     { return toolDescription }
func (t *Tool) Schema() json.RawMessage { return json.RawMessage(toolSchema) }

func (t *Tool) IsReadOnly() bool                       { return true }
func (t *Tool) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *Tool) Approval(json.RawMessage) agent.Approval {
	return agent.Approval{Tier: agent.TierRead}
}

func (t *Tool) Metadata(json.RawMessage) agent.Metadata {
	return agent.Metadata{MaxResultSize: resultCap}
}

func (t *Tool) Execute(ctx context.Context, tc agent.ToolContext, raw json.RawMessage) agent.ToolResult {
	var a toolArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return result("skill: invalid arguments: "+err.Error(), true)
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return result("skill: name is required; available: "+t.names(), true)
	}
	for _, s := range t.Skills {
		if strings.EqualFold(s.Name, name) {
			return result(s.Body, false)
		}
	}
	return result(fmt.Sprintf("skill: no skill named %q; available: %s", name, t.names()), true)
}

func (t *Tool) names() string {
	if len(t.Skills) == 0 {
		return "(none)"
	}
	out := make([]string, len(t.Skills))
	for i, s := range t.Skills {
		out[i] = s.Name
	}
	return strings.Join(out, ", ")
}

type Summary struct {
	Name        string
	Description string
}

func Summaries(skills []Skill) []Summary {
	out := make([]Summary, len(skills))
	for i, s := range skills {
		out[i] = Summary{Name: s.Name, Description: s.Description}
	}
	return out
}

func result(text string, isErr bool) agent.ToolResult {
	return agent.ToolResult{Content: []llm.ContentBlock{{Type: llm.BlockText, Text: text}}, IsError: isErr}
}

const toolDescription = `Load a skill's full instructions by name. Skills are listed in the project context with a one-line description; load one when its description matches the job, then follow its procedure. Loading costs context, so load only what you will use.`

const toolSchema = `{
  "type": "object",
  "properties": {
    "name": {"type": "string", "description": "The skill name exactly as listed."}
  },
  "required": ["name"],
  "additionalProperties": false
}`
