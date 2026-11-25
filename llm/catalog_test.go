package llm

import "testing"

func TestContextWindowKnownFamilies(t *testing.T) {
	cases := map[string]int{
		"claude-opus-5-5":                          1_000_000,
		"anthropic/claude-haiku-5-5":               1_000_000,
		"mify/volcengine_maas/deepseek-v4.1-flash": 128_000,
		"deepseek-chat":                            128_000,
		"qwen3-coder":                              131_072,
		"kimi-k2-instruct":                         256_000,
		"gpt-4o":                                   128_000,
	}
	for model, want := range cases {
		got, ok := ContextWindow(model)
		if !ok || got != want {
			t.Errorf("%s: got (%d,%v), want (%d,true)", model, got, ok, want)
		}
	}
}

func TestContextWindowIsCaseInsensitive(t *testing.T) {
	if w, ok := ContextWindow("Claude-Sonnet-5"); !ok || w != 1_000_000 {
		t.Errorf("got (%d,%v)", w, ok)
	}
}

func TestContextWindowUnknownModelIsNotGuessed(t *testing.T) {
	if _, ok := ContextWindow("some-local-model"); ok {
		t.Errorf("an unknown model must not be guessed: a wrong window silently breaks compaction")
	}
}

func TestContextWindowPrefersSpecificFamily(t *testing.T) {
	if w, _ := ContextWindow("claude-opus-5-5"); w != 1_000_000 {
		t.Errorf("a claude-5 id must match the claude-5 entry, got %d", w)
	}
	if w, _ := ContextWindow("claude-3-5-sonnet"); w != 200_000 {
		t.Errorf("a claude-3 id must fall through to claude-3, got %d", w)
	}
}
