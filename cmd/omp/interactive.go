package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tui"
)

func (a *app) windowSize() int { return int(a.window.Load()) }

func (a *app) newSession() (session.Session, error) {
	if err := a.conv.fresh(); err != nil {
		return nil, err
	}
	a.goals.Reset()
	return a.conv.sess, nil
}

func (a *app) resumeSession(arg string) (session.Session, string, error) {
	if a.conv.dir == "" {
		return nil, "", fmt.Errorf("sessions are off")
	}
	path, err := resolveSession(a.conv.dir, arg)
	if err != nil {
		return nil, "", err
	}
	if err := a.conv.load(path); err != nil {
		return nil, "", err
	}
	a.conv.restoreGoal(a.goals)
	return a.conv.sess, strings.TrimSuffix(filepath.Base(path), session.FileSuffix), nil
}

func (a *app) listSessions() ([]session.Info, error) {
	if a.conv.dir == "" {
		return nil, nil
	}
	return session.List(a.conv.dir)
}

func (a *app) renameSession(title string) error {
	if a.conv.store == nil {
		return fmt.Errorf("sessions are off")
	}
	a.conv.store.SetTitle(title)
	return a.conv.store.Err()
}

func (a *app) exportSession(path string) (string, error) {
	if path == "" {
		path = filepath.Join(a.cwd, a.conv.sessionID()+".md")
	}
	body := session.Markdown(a.conv.sess.Messages())
	if err := session.WriteExport(path, body); err != nil {
		return "", err
	}
	return path, nil
}

func (a *app) deleteSession(id string) error {
	if a.conv.dir == "" {
		return fmt.Errorf("sessions are off")
	}
	path, err := resolveSession(a.conv.dir, id)
	if err != nil {
		return err
	}
	if path == a.conv.sessionPath() {
		return fmt.Errorf("that is the session you are in; start a new one first")
	}
	return session.DeleteByPath(path)
}

func (a *app) setModel(selector string) (string, error) {
	next, err := switchModel(a.cfg, a.provider, selector, a.window)
	if err != nil {
		return "", err
	}
	return next.modelLabel, nil
}

func runInteractive(ctx context.Context, a *app) int {
	host := tui.NewHost()
	loop := agent.NewLoop(a.reg, a.deps, host, a.opts)
	var memoryControl tui.MemoryControl
	if c := a.mem.control(ctx, a.conv, a.provider); c != nil {
		c.Start(false)
		memoryControl = c
	}
	err := tui.Run(ctx, tui.Options{
		Runner:       a.stack(loop),
		Compact:      traceCompact(a.rec, loop.Compact),
		Session:      a.conv.sess,
		NewSession:   a.newSession,
		Resume:       a.resumeSession,
		SwitchModel:  a.setModel,
		ListSessions: a.listSessions,
		Rename:       a.renameSession,
		Export:       a.exportSession,
		Delete:       a.deleteSession,
		Host:         host,
		Policy:       a.policy,
		Effort:       a.provider,
		Goal:         a.goals,
		Memory:       memoryControl,
		Steer:        a.steers.Push,
		Model:        a.cfg.modelLabel,
		Window:       a.windowSize,
		CWD:          a.opts.CWD,
		Prompts:      a.cfg.prompts,
		Projectors:   projectors(),
		InjectedTags: injectedTags,
		Note:         a.conv.note,
	})
	if err != nil {
		fmt.Fprintln(a.stderr, "omp:", sanitize(err.Error()))
		return 1
	}
	return 0
}
