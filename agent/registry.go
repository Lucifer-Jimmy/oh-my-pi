package agent

import (
	"slices"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

const (
	minSuggestSegment = 3
	maxSuggestions    = 3
)

// 工具按注册顺序保存，模型每次看到的 schema 顺序因此稳定
type Registry struct {
	tools map[string]Tool
	raw   map[string]Tool
	order []string
	wrap  func(Tool) Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]Tool), raw: make(map[string]Tool)}
}

func (r *Registry) SetWrapper(fn func(Tool) Tool) {
	r.wrap = fn
	for _, name := range r.order {
		r.tools[name] = r.apply(r.raw[name])
	}
}

func (r *Registry) apply(t Tool) Tool {
	if r.wrap == nil {
		return t
	}
	return r.wrap(t)
}

func (r *Registry) Register(t Tool) {
	name := t.Name()
	if _, ok := r.raw[name]; !ok {
		r.order = append(r.order, name)
	}
	r.raw[name] = t
	r.tools[name] = r.apply(t)
}

func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

func (r *Registry) Schemas() []llm.ToolSchema {
	out := make([]llm.ToolSchema, 0, len(r.order))
	for _, name := range r.order {
		t := r.tools[name]
		out = append(out, llm.ToolSchema{Name: name, Description: t.Description(), InputSchema: t.Schema()})
	}
	return out
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// 按注册顺序返回已注册的工具名 供提示词等需要知道真实工具集的地方使用
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// 未知工具的错误文本，顺带给出按名字猜出的近似工具
func (r *Registry) NotFound(name string) string {
	return toolNotFound(name, r.order)
}

func toolNotFound(name string, names []string) string {
	s := suggestTools(name, names)
	switch len(s) {
	case 0:
		return "Tool " + name + " not found"
	case 1:
		return "Tool " + name + " not found. Did you mean " + s[0] + "?"
	default:
		return "Tool " + name + " not found. Closest available: " + strings.Join(s[:min(len(s), maxSuggestions)], ", ")
	}
}

func suggestTools(name string, names []string) []string {
	var out []string
	add := func(c string) {
		if c != name && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	for _, c := range names {
		if strings.EqualFold(c, name) {
			add(c)
		}
	}
	type near struct {
		name string
		dist int
	}
	var byDist []near
	for _, c := range names {
		if d := editDistance(strings.ToLower(c), strings.ToLower(name)); d <= 2 {
			byDist = append(byDist, near{c, d})
		}
	}
	slices.SortStableFunc(byDist, func(a, b near) int { return a.dist - b.dist })
	for _, n := range byDist {
		add(n.name)
	}
	// 拿名字按下划线切开后的最后一段，找同名或以它结尾的工具，段太短就跳过免得乱猜
	for _, sep := range []string{"__", "_"} {
		i := strings.LastIndex(name, sep)
		if i < 0 || len(name)-i-len(sep) < minSuggestSegment {
			continue
		}
		seg := name[i+len(sep):]
		for _, c := range names {
			if c == seg || strings.HasSuffix(c, "_"+seg) {
				add(c)
			}
		}
	}
	return out
}
