package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type conversation struct {
	sess  session.Session
	store *session.Store
	dir   string
	cwd   string
	model string
	note  string
}

func openConversation(cfg config, cwd string) (*conversation, error) {
	c := &conversation{cwd: cwd, model: cfg.modelLabel, sess: session.NewMemory(nil)}
	if cfg.noSession {
		return c, nil
	}
	root, err := session.Root()
	if cfg.sessionsDir != "" {
		root, err = cfg.sessionsDir, nil
	}
	if err != nil {
		c.note = "sessions are off: " + err.Error()
		return c, nil
	}
	c.dir = session.ProjectDir(root, cwd)
	switch {
	case cfg.resume != "":
		path, err := resolveSession(c.dir, cfg.resume)
		if err != nil {
			return nil, err
		}
		if err := c.load(path); err != nil {
			return nil, err
		}
	case cfg.cont:
		path, ok, err := session.Latest(c.dir)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("no earlier session in %s", c.dir)
		}
		if err := c.load(path); err != nil {
			return nil, err
		}
	default:
		if err := c.fresh(); err != nil {
			c.note = "sessions are off: " + err.Error()
		}
	}
	return c, nil
}

func (c *conversation) load(path string) error {
	s, r, err := session.Load(path)
	if err != nil {
		return err
	}
	c.sess, c.store = s, s
	if note := r.String(); note != "" {
		c.note = "Session repaired on load: " + note
	}
	return nil
}

func (c *conversation) fresh() error {
	s, err := session.Create(c.dir, session.NewID(time.Now()), c.cwd, c.model)
	if err != nil {
		return err
	}
	if c.store != nil {
		_ = c.store.Close()
	}
	c.sess, c.store = s, s
	return nil
}

func (c *conversation) sessionID() string {
	if c.store == nil {
		return "session"
	}
	return c.store.ID()
}

func (c *conversation) sessionPath() string {
	if c.store == nil {
		return ""
	}
	return c.store.Path()
}

func (c *conversation) Close() error {
	if c.store == nil {
		return nil
	}
	return c.store.Close()
}

func resolveSession(dir, arg string) (string, error) {
	if st, err := os.Stat(arg); err == nil && !st.IsDir() {
		return arg, nil
	}
	name := strings.TrimSuffix(filepath.Base(arg), session.FileSuffix)
	exact := filepath.Join(dir, name+session.FileSuffix)
	if st, err := os.Stat(exact); err == nil && !st.IsDir() {
		return exact, nil
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("no session matches %q", arg)
	}
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), name) {
			continue
		}
		found = append(found, filepath.Join(dir, e.Name()))
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("no session matches %q", arg)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("%q matches %d sessions, use more of the id", arg, len(found))
	}
}

func (c *conversation) saveGoal(g goal.Goal) {
	if c.store == nil {
		return
	}
	b, err := json.Marshal(g)
	if err != nil {
		return
	}
	c.store.SetGoal(b)
}

func (c *conversation) restoreGoal(t *goal.Tracker) {
	t.Reset()
	if c.store == nil {
		return
	}
	raw := c.store.Goal()
	if len(raw) == 0 {
		return
	}
	var g goal.Goal
	if json.Unmarshal(raw, &g) != nil {
		return
	}
	t.Restore(g)
}

func (c *conversation) attachGoal(t *goal.Tracker) {
	t.OnChange = c.saveGoal
	c.restoreGoal(t)
	g, ok := t.Current()
	if !ok || !g.Live() {
		return
	}
	hint := "/goal resume continues it"
	if g.Status == goal.StatusBudgetLimited {
		hint = "/goal budget <tokens> raises its budget"
	}
	note := fmt.Sprintf("This session has a %s goal: %s (%s)", g.Status, firstLineOf(g.Objective), hint)
	if c.note != "" {
		note = c.note + "\n" + note
	}
	c.note = note
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(line); len(r) > 120 {
		line = string(r[:120]) + "…"
	}
	return line
}
