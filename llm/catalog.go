package llm

import "strings"

type WindowEntry struct {
	Match  string
	Window int
}

var windows = []WindowEntry{
	{"claude-opus-5", 1_000_000},
	{"claude-sonnet-5", 1_000_000},
	{"claude-haiku-5", 1_000_000},
	{"claude-fable-5", 1_000_000},
	{"claude-opus-4", 200_000},
	{"claude-sonnet-4", 200_000},
	{"claude-3", 200_000},
	{"gpt-4.1", 1_047_576},
	{"gpt-4o", 128_000},
	{"deepseek", 128_000},
	{"glm-4", 128_000},
	{"kimi-k2", 256_000},
	{"qwen3", 131_072},
}

func ContextWindow(model string) (int, bool) {
	id := strings.ToLower(model)
	for _, e := range windows {
		if strings.Contains(id, e.Match) {
			return e.Window, true
		}
	}
	return 0, false
}
