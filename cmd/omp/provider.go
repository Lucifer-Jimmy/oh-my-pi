package main

import (
	"context"
	"sync/atomic"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/compaction"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm/anthropic"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm/openai"
)

func newProvider(cfg config) llm.Provider {
	if cfg.api == llm.ProtocolAnthropicMessages {
		return anthropic.New(anthropic.Config{APIKey: cfg.apiKey, BaseURL: cfg.baseURL, Model: cfg.model})
	}
	return openai.New(openai.Config{APIKey: cfg.apiKey, BaseURL: cfg.baseURL, Model: cfg.model, Api: cfg.api})
}

func switchModel(cfg config, provider *effort, selector string, window *atomic.Int64) (config, error) {
	next := cfg
	next.model = selector
	next.modelLabel = ""
	next.apiKeyConfig = ""
	next.api = ""
	next.baseURL = ""
	path, err := configPath()
	if err != nil {
		return cfg, err
	}
	f, err := loadConfigFile(path)
	if err != nil {
		return cfg, err
	}
	model, level := splitThinkingSuffix(selector)
	next.model, next.modelLabel = model, model
	if next.thinking == "" {
		next.thinking = level
	}
	next, err = applyProvider(next, f.Providers)
	if err != nil {
		return cfg, err
	}
	key, err := resolveAPIKey(next.apiKeyConfig)
	if err != nil {
		return cfg, err
	}
	next.apiKey = key
	policy := llm.DefaultRetryPolicy()
	provider.Swap(llm.NewRetrying(newProvider(next), policy))
	window.Store(int64(resolveWindow(next)))
	return next, nil
}

func runDepsWithWindow(cfg config, p llm.Provider, window *atomic.Int64) agent.RunDeps {
	return agent.RunDeps{
		CallModel: p.Stream,
		Compactor: compactor(cfg, p, window),
	}
}

func resolveWindow(cfg config) int {
	if cfg.compaction.ContextWindow > 0 {
		return cfg.compaction.ContextWindow
	}
	if w, ok := llm.ContextWindow(cfg.model); ok {
		return w
	}
	return 0
}

func compactor(cfg config, p llm.Provider, window *atomic.Int64) agent.Compactor {
	c := cfg.compaction
	if c.Disabled {
		return nil
	}
	return compaction.New(compaction.Config{
		Window:           func() int { return int(window.Load()) },
		KeepRecentTokens: c.KeepRecentTokens,
		ReserveTokens:    c.ReserveTokens,
		MinElideTokens:   c.MinElideTokens,
	}, func(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.Usage, error) {
		msg, _, usage, err := p.Complete(ctx, req)
		return msg, usage, err
	})
}
