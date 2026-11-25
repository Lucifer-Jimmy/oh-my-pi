package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/background"
	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/mcp"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/skill"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
	"github.com/Lucifer-Jimmy/oh-my-pi/trace"
)

func dispatch(cfg config, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signalCode := watchSignals(cancel)

	cfg, err := applyConfigFile(cfg)
	if err != nil {
		if cfg.interactive && errors.Is(err, errNoModel) {
			if ok, werr := runFirstRun(os.Stdin, stdout); werr == nil && ok {
				if again, rerr := applyConfigFile(config{}); rerr == nil {
					cfg, err = again, nil
				}
			}
		}
		if err != nil {
			fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
			return 2
		}
	}
	key, err := resolveAPIKey(cfg.apiKeyConfig)
	if err != nil {
		fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
		return 1
	}
	cfg.apiKey = key

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
		return 1
	}

	shell := tools.ResolveShell("")
	policy := permission.NewPolicy(cfg.approvalMode(), cfg.rules())
	retryPolicy := llm.DefaultRetryPolicy()
	if !cfg.interactive {
		retryPolicy.OnRetry = func(attempt int, delay time.Duration, err error) {
			fmt.Fprintln(stderr, "omp: "+sanitize(llm.RetryNote(attempt, delay, err)))
		}
	}
	provider := newEffort(llm.NewRetrying(newProvider(cfg), retryPolicy), cfg.thinking)
	window := new(atomic.Int64)
	window.Store(int64(resolveWindow(cfg)))
	steers := new(steerer)
	deps := runDepsWithWindow(cfg, provider, window)
	skills := skill.Discover(cwd)
	bg, err := background.NewDefault(shell)
	if err != nil {
		fmt.Fprintln(stderr, "omp: background commands are off:", sanitize(err.Error()))
		bg = nil
	}
	if bg != nil {
		deps.Notify = bg.Take
	}
	pool := new(mcp.Pool)
	defer pool.Close()
	ext, err := loadExternalTools(cfg.extTools)
	if err != nil {
		fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
		return 2
	}
	goals := new(goal.Tracker)
	mem, memNote := openMemory(cfg, cwd)
	reg := assemble(cwd, shell, policy, deps, cfg, skills, ext, bg, goals, func(ctx context.Context) []agent.Tool {
		return connectMCP(ctx, cfg, pool, stderr)
	})
	opts := options(cfg, cwd, reg.Names(), shell.Name, skills)
	mem.attach(reg, &opts)
	opts.Hooks = withGoalPrompt(hookSet(cfg, cwd).Hooks(), goals)
	deps.Steer = steers.Take
	conv, err := openConversation(cfg, cwd)
	if err != nil {
		fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
		return 2
	}
	defer conv.Close()
	conv.attachGoal(goals)
	if memNote != "" {
		conv.note = strings.TrimPrefix(conv.note+"\n"+memNote, "\n")
	}
	if conv.note != "" && !cfg.interactive {
		fmt.Fprintln(stderr, "omp:", conv.note)
	}
	rec, err := openTrace(cfg, cwd, conv, window, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "omp:", sanitize(err.Error()))
		return 2
	}
	a := &app{
		cfg: cfg, conv: conv, reg: reg, opts: opts, deps: deps, policy: policy,
		provider: provider, window: window, steers: steers, goals: goals, mem: mem,
		shell: shell, cwd: cwd, rec: rec, stderr: stderr,
	}
	var code int
	switch {
	case cfg.mode == "json":
		code = runJSON(ctx, a.runner(nil), conv.sess, cfg.prompts, cwd, stdout, stderr)
	case cfg.mode == "rpc":
		host := newRPCHost()
		code = runRPC(ctx, host, a.runner(host), conv.sess, cfg.prompts, os.Stdin, stdout, stderr)
	case cfg.interactive:
		code = runInteractive(ctx, a)
	default:
		code = runPrint(ctx, a.runner(nil), conv.sess, cfg.prompts, stdout, stderr)
	}
	if rec != nil {
		if err := rec.Close(); err != nil {
			fmt.Fprintln(stderr, "omp: trace:", sanitize(err.Error()))
		}
	}
	if c := signalCode.Load(); c != 0 {
		return int(c)
	}
	return code
}

type app struct {
	cfg      config
	conv     *conversation
	reg      *agent.Registry
	opts     agent.Options
	deps     agent.RunDeps
	policy   *permission.Policy
	provider *effort
	window   *atomic.Int64
	steers   *steerer
	goals    *goal.Tracker
	mem      *memorySetup
	shell    tools.Shell
	cwd      string
	rec      *trace.Recorder
	stderr   io.Writer
}

func (a *app) runner(host agent.Host) agent.Runner {
	return a.stack(agent.NewLoop(a.reg, a.deps, host, a.opts))
}

func (a *app) stack(loop *agent.Loop) agent.Runner {
	var run agent.Runner = withPlanReminder(loop, a.policy)
	run = verifyRunner(a.cfg, a.cwd, a.shell, run)
	run = goalRunner(a.goals, a.policy, run)
	return traceRunner(a.rec, run)
}

func watchSignals(cancel context.CancelFunc) *atomic.Int32 {
	code := new(atomic.Int32)
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-ch
		signal.Stop(ch)
		c := int32(130)
		if s == syscall.SIGTERM {
			c = 143
		}
		code.Store(c)
		cancel()
	}()
	return code
}
