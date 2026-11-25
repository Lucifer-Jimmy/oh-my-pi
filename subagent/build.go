package subagent

import (
	"context"
	"errors"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

var notInherited = map[string]bool{"task": true, "exit_plan_mode": true, "background": true, "goal": true}

func Inherits(name string) bool { return !notInherited[name] }

type Config struct {
	Registry *agent.Registry
	Deps     agent.RunDeps
	Options  func(dir string) agent.Options
	CWD      string
	RepoRoot string
	Wrap     func(agent.Tool) agent.Tool
}

func childRegistry(parent *agent.Registry, wrap func(agent.Tool) agent.Tool) *agent.Registry {
	child := agent.NewRegistry()
	for _, name := range parent.Names() {
		if !Inherits(name) {
			continue
		}
		if t, ok := parent.Get(name); ok {
			if wrap != nil {
				t = wrap(t)
			}
			child.Register(t)
		}
	}
	return child
}

func NewTaskTool(cfg Config) *Task {
	root := cfg.RepoRoot
	return &Task{
		Run: func(ctx context.Context, prompt string, progress func(agent.Event)) (Result, error) {
			return cfg.run(ctx, prompt, cfg.CWD, progress)
		},
		Isolated: func(ctx context.Context, prompt string, progress func(agent.Event)) (Result, error) {
			wt, err := createWorktree(root)
			if err != nil {
				return Result{}, err
			}
			defer wt.remove()
			res, err := cfg.run(ctx, prompt, wt.dir, progress)
			if err != nil {
				return Result{}, err
			}
			note := "ran in a temporary git worktree; "
			sha, changed, err := wt.commit("omp subagent " + prompt)
			switch {
			case err != nil:
				res.Err = err.Error()
				res.Reason = agent.ReasonError
				return res, nil
			case !changed:
				res.Note = note + "nothing changed, no files to merge"
				return res, nil
			}
			p, err := wt.patch(sha)
			if err != nil {
				res.Err = err.Error()
				res.Reason = agent.ReasonError
				return res, nil
			}
			if err := wt.apply(p); err != nil {
				path, serr := savePatch(wt.name, p)
				res.Note = note + "its changes do not apply onto your working tree: " + err.Error()
				if serr == nil {
					res.Note += "; the patch is saved at " + path
				}
				res.Err = "merge conflict"
				res.Reason = agent.ReasonError
				return res, nil
			}
			res.Note = note + "its changes were applied to your working tree"
			return res, nil
		},
	}
}

func (cfg Config) run(ctx context.Context, prompt, dir string, progress func(agent.Event)) (Result, error) {
	if cfg.Options == nil {
		return Result{}, errors.New("subagent: no options")
	}
	opts := cfg.Options(dir)
	if dir != "" {
		opts.Workspaces = []string{dir}
	}
	loop := agent.NewLoop(childRegistry(cfg.Registry, cfg.Wrap), cfg.Deps, nil, opts)
	sess := session.NewMemory(nil)
	res := Result{Reason: agent.ReasonError}
	for ev, err := range loop.Run(ctx, sess, prompt) {
		if err != nil {
			return Result{}, err
		}
		if progress != nil {
			progress(ev)
		}
		if ev.Kind == agent.EventResult && ev.Result != nil {
			res = Result{
				Text:   ev.Result.Text,
				Turns:  ev.Result.Turns,
				Usage:  ev.Result.Usage,
				Reason: ev.Result.Reason,
				Err:    ev.Result.Err,
			}
		}
	}
	return res, nil
}
