package main

import (
	"context"
	"iter"
	"sync"
	"sync/atomic"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type effort struct {
	mu    sync.RWMutex
	inner llm.Provider
	level atomic.Value
}

func newEffort(inner llm.Provider, level llm.ThinkingLevel) *effort {
	e := &effort{inner: inner}
	e.level.Store(level)
	return e
}

func (e *effort) Level() llm.ThinkingLevel {
	if v, ok := e.level.Load().(llm.ThinkingLevel); ok {
		return v
	}
	return ""
}

func (e *effort) SetLevel(l llm.ThinkingLevel) { e.level.Store(l) }

func (e *effort) Swap(inner llm.Provider) {
	e.mu.Lock()
	e.inner = inner
	e.mu.Unlock()
}

func (e *effort) provider() llm.Provider {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.inner
}

func (e *effort) with(req llm.CompletionRequest) llm.CompletionRequest {
	req.Thinking = e.Level()
	return req
}

func (e *effort) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return e.provider().Stream(ctx, e.with(req))
}

func (e *effort) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, llm.StopReason, llm.Usage, error) {
	return e.provider().Complete(ctx, e.with(req))
}
