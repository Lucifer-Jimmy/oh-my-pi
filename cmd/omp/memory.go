package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/memory"
)

type memorySection struct {
	Backend string `yaml:"backend"`
}

type memorySetup struct {
	store    memory.Store
	snapshot string
}

func openMemory(cfg config, cwd string) (*memorySetup, string) {
	switch strings.ToLower(strings.TrimSpace(cfg.memoryBackend)) {
	case "", "off":
		return nil, ""
	case "local":
	default:
		return nil, fmt.Sprintf("memory.backend %q is not supported, so memory is off (supported: off, local)", cfg.memoryBackend)
	}
	root, err := memory.Root()
	if err != nil {
		return nil, "memory is off: " + err.Error()
	}
	store := memory.ForProject(root, cwd)
	return &memorySetup{store: store, snapshot: memory.Guidance(store.Summary(), store.Lessons())}, ""
}

func (m *memorySetup) attach(reg *agent.Registry, opts *agent.Options) {
	if m == nil {
		return
	}
	reg.Register(&memory.LearnTool{Store: m.store})
	if m.snapshot != "" {
		opts.System = append(opts.System, m.snapshot)
	}
}

func (m *memorySetup) control(ctx context.Context, conv *conversation, provider llm.Provider) *memory.Control {
	if m == nil {
		return nil
	}
	c := &memory.Control{Store: m.store, Snapshot: m.snapshot, Context: ctx}
	if conv.store != nil && conv.dir != "" {
		c.Pipeline = &memory.Pipeline{
			Store:       m.store,
			SessionsDir: conv.dir,
			Current:     conv.sessionID(),
			Complete:    completeText(provider),
		}
	}
	return c
}

func completeText(p llm.Provider) memory.Complete {
	return func(ctx context.Context, req llm.CompletionRequest) (string, error) {
		msg, _, _, err := p.Complete(ctx, req)
		if err != nil {
			return "", err
		}
		return msg.Text(), nil
	}
}
