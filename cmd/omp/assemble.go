package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/background"
	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/hook"
	"github.com/Lucifer-Jimmy/oh-my-pi/mcp"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/plan"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/prompt"
	"github.com/Lucifer-Jimmy/oh-my-pi/skill"
	"github.com/Lucifer-Jimmy/oh-my-pi/subagent"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

func assemble(cwd string, shell tools.Shell, policy *permission.Policy, deps agent.RunDeps, cfg config, skills []skill.Skill, mcpTools []agent.Tool, bg *background.Manager, goals *goal.Tracker, connect func(context.Context) []agent.Tool) *agent.Registry {
	reg := agent.NewRegistry()
	reg.SetWrapper(func(t agent.Tool) agent.Tool { return permission.Wrap(t, policy) })
	for _, tool := range tools.Builtin(tools.Config{
		Shell:      shell,
		State:      tools.NewFileState(),
		DeniedGlob: deniedPath(policy, "glob"),
		DeniedGrep: deniedPath(policy, "grep"),
	}) {
		reg.Register(tool)
	}
	reg.Register(&plan.ExitTool{Modes: policy})
	if goals != nil {
		reg.Register(&goal.Tool{Tracker: goals})
	}
	if len(skills) > 0 {
		reg.Register(&skill.Tool{Skills: skills})
	}
	for _, t := range mcpTools {
		reg.Register(t)
	}
	if connect != nil {
		for _, t := range connect(context.Background()) {
			reg.Register(t)
		}
	}
	if bg != nil {
		reg.Register(&background.Tool{Manager: bg})
	}
	names := childTools(reg.Names())
	child := permission.NewPolicy(subagentMode(policy, names), cfg.rules())
	reg.Register(newTaskTool(subagent.Config{
		Registry: reg,
		Deps:     childDeps(deps),
		Options: func(dir string) agent.Options {
			o := options(cfg, dir, names, shell.Name, skills)
			h := hookSet(cfg, dir).Hooks()
			o.Hooks = agent.Hooks{PreTool: h.PreTool, PostTool: h.PostTool}
			return o
		},
		CWD:      cwd,
		RepoRoot: repoRootOf(cwd),
		Wrap:     func(t agent.Tool) agent.Tool { return permission.Wrap(t, child) },
	}))
	return reg
}

var newTaskTool = subagent.NewTaskTool

func subagentMode(policy *permission.Policy, names []string) permission.Mode {
	mode, rules := policy.Snapshot()
	if mode == permission.ModePlan {
		return mode
	}
	d := permission.Evaluate(permission.Request{Tool: "bash", Tier: permission.TierWrite, Mode: mode, Rules: rules})
	if d.Behavior == permission.Allow {
		return permission.ModeBypass
	}
	return mode
}

func repoRootOf(cwd string) string {
	out, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func deniedPath(policy *permission.Policy, tool string) func(string) bool {
	return func(rel string) bool { return policy.DeniesPath(tool, rel) }
}

func childTools(names []string) []string {
	out := make([]string, 0, len(names))
	for _, n := range names {
		if subagent.Inherits(n) {
			out = append(out, n)
		}
	}
	return out
}

func childDeps(deps agent.RunDeps) agent.RunDeps {
	out := deps
	out.Steer = nil
	out.Compactor = nil
	out.Notify = nil
	return out
}

func options(cfg config, cwd string, toolNames []string, shell string, skills []skill.Skill) agent.Options {
	promptCtx := prompt.ContextFromEnv(cwd, shell)
	promptCtx.Files = prompt.DiscoverContextFiles(cwd)
	for _, s := range skill.Summaries(skills) {
		promptCtx.Skills = append(promptCtx.Skills, prompt.SkillSummary{Name: s.Name, Description: s.Description})
	}
	return agent.Options{
		System:        prompt.Build(promptCtx, toolNames),
		CacheBoundary: prompt.CacheBoundary,
		MaxTokens:     cfg.maxTokens,
		MaxTurns:      cfg.maxTurns,
		TokenBudget:   cfg.budgetTokens,
		Thinking:      cfg.thinking,
		CWD:           cwd,
		Workspaces:    []string{cwd},
	}
}

var injectedTags = []string{"system-reminder", "goal_context", "goal_continuation", "goal_budget_limit", "background-task", "hook"}

func projectors() map[string]presentation.ToolProjector {
	return map[string]presentation.ToolProjector{
		"todo": func(s *presentation.State, raw json.RawMessage) {
			var a struct {
				Items []presentation.TodoItem `json:"items"`
			}
			if err := json.Unmarshal(raw, &a); err != nil || len(a.Items) == 0 {
				return
			}
			s.Todos = a.Items
		},
	}
}

func hookSet(cfg config, dir string) *hook.Set {
	toSpec := func(entries []hookEntry) []hook.Spec {
		out := make([]hook.Spec, 0, len(entries))
		for _, e := range entries {
			out = append(out, hook.Spec{
				Command: e.Command,
				Args:    e.Args,
				Env:     e.Env,
				Dir:     dir,
				Timeout: time.Duration(e.Timeout) * time.Second,
			})
		}
		return out
	}
	return &hook.Set{
		PreTool:  toSpec(cfg.hooks.PreTool),
		PostTool: toSpec(cfg.hooks.PostTool),
		Stop:     toSpec(cfg.hooks.Stop),
		Prompt:   toSpec(cfg.hooks.Prompt),
	}
}

func connectMCP(ctx context.Context, cfg config, pool *mcp.Pool, stderr io.Writer) []agent.Tool {
	var out []agent.Tool
	names := make([]string, 0, len(cfg.mcp))
	for name := range cfg.mcp {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		e := cfg.mcp[name]
		connCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		srv, err := pool.Connect(connCtx, name, mcp.ServerConfig{
			Command: e.Command,
			Args:    e.Args,
			Env:     e.Env,
			URL:     e.URL,
			SSE:     strings.EqualFold(strings.TrimSpace(e.Transport), "sse"),
		})
		cancel()
		if err != nil {
			fmt.Fprintln(stderr, "omp: "+sanitize(err.Error()))
			continue
		}
		out = append(out, srv.Tools()...)
	}
	return out
}
