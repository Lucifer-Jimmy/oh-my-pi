package goal

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type Status string

const (
	StatusActive        Status = "active"
	StatusPaused        Status = "paused"
	StatusBudgetLimited Status = "budget-limited"
	StatusComplete      Status = "complete"
	StatusDropped       Status = "dropped"
)

type Goal struct {
	ID              string `json:"id"`
	Objective       string `json:"objective"`
	Status          Status `json:"status"`
	TokenBudget     int    `json:"token_budget,omitempty"`
	TokensUsed      int    `json:"tokens_used"`
	TimeUsedSeconds int64  `json:"time_used_seconds"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (g Goal) Live() bool {
	return g.Status == StatusActive || g.Status == StatusPaused || g.Status == StatusBudgetLimited
}

func (g Goal) Remaining() (int, bool) {
	if g.TokenBudget <= 0 {
		return 0, false
	}
	return max(0, g.TokenBudget-g.TokensUsed), true
}

func (g Goal) accounting() bool {
	return g.Status == StatusActive || g.Status == StatusBudgetLimited
}

var (
	ErrNoGoal     = errors.New("no goal is set")
	ErrGoalExists = errors.New("this session already has a goal; complete or drop it before creating another")
)

type Tracker struct {
	OnChange func(Goal)
	Now      func() time.Time

	mu      sync.Mutex
	goal    *Goal
	notice  bool
	partial time.Duration
}

func (t *Tracker) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

func (t *Tracker) changed(g Goal) {
	if t.OnChange != nil {
		t.OnChange(g)
	}
}

func (t *Tracker) Current() (Goal, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.goal == nil {
		return Goal{}, false
	}
	return *t.goal, true
}

func (t *Tracker) Create(objective string, budget int) (Goal, error) {
	return t.start(objective, budget, false)
}

func (t *Tracker) Set(objective string, budget int) (Goal, error) {
	return t.start(objective, budget, true)
}

func (t *Tracker) start(objective string, budget int, replace bool) (Goal, error) {
	objective = strings.TrimSpace(objective)
	if objective == "" {
		return Goal{}, errors.New("the objective is empty")
	}
	if budget < 0 {
		return Goal{}, errors.New("the token budget must be a positive number of tokens")
	}
	now := t.now().UnixMilli()
	g := Goal{ID: newID(), Objective: objective, Status: StatusActive, TokenBudget: budget, CreatedAt: now, UpdatedAt: now}
	t.mu.Lock()
	if !replace && t.goal != nil && t.goal.Live() {
		t.mu.Unlock()
		return Goal{}, ErrGoalExists
	}
	t.goal, t.notice, t.partial = &g, false, 0
	t.mu.Unlock()
	t.changed(g)
	return g, nil
}

func (t *Tracker) update(fn func(g *Goal) error) (Goal, error) {
	t.mu.Lock()
	if t.goal == nil {
		t.mu.Unlock()
		return Goal{}, ErrNoGoal
	}
	g := *t.goal
	if err := fn(&g); err != nil {
		t.mu.Unlock()
		return g, err
	}
	g.UpdatedAt = t.now().UnixMilli()
	*t.goal = g
	if g.Status != StatusBudgetLimited {
		t.notice = false
	}
	t.mu.Unlock()
	t.changed(g)
	return g, nil
}

func (t *Tracker) Pause() (Goal, error) {
	return t.update(func(g *Goal) error {
		switch g.Status {
		case StatusActive, StatusBudgetLimited:
			g.Status = StatusPaused
			return nil
		case StatusPaused:
			return errors.New("the goal is already paused")
		}
		return fmt.Errorf("the goal is %s", g.Status)
	})
}

func (t *Tracker) Resume() (Goal, error) {
	return t.update(func(g *Goal) error {
		switch g.Status {
		case StatusActive:
			return errors.New("the goal is already active")
		case StatusComplete, StatusDropped:
			return fmt.Errorf("the goal is %s", g.Status)
		}
		if left, ok := g.Remaining(); ok && left == 0 {
			return errors.New("the token budget is used up; raise it or remove it before resuming")
		}
		g.Status = StatusActive
		return nil
	})
}

func (t *Tracker) Complete() (Goal, error) {
	return t.update(func(g *Goal) error {
		if !g.Live() {
			return fmt.Errorf("the goal is already %s", g.Status)
		}
		g.Status = StatusComplete
		return nil
	})
}

func (t *Tracker) SetBudget(tokens int) (Goal, error) {
	if tokens < 0 {
		return Goal{}, errors.New("the token budget must be a positive number of tokens")
	}
	return t.update(func(g *Goal) error {
		if !g.Live() {
			return fmt.Errorf("the goal is %s", g.Status)
		}
		g.TokenBudget = tokens
		over := tokens > 0 && g.TokensUsed >= tokens
		switch {
		case over && g.Status == StatusActive:
			g.Status = StatusBudgetLimited
			t.notice = true
		case !over && g.Status == StatusBudgetLimited:
			g.Status = StatusActive
		}
		return nil
	})
}

func (t *Tracker) Drop() (Goal, error) {
	t.mu.Lock()
	if t.goal == nil {
		t.mu.Unlock()
		return Goal{}, ErrNoGoal
	}
	g := *t.goal
	g.Status, g.UpdatedAt = StatusDropped, t.now().UnixMilli()
	t.goal, t.notice, t.partial = nil, false, 0
	t.mu.Unlock()
	t.changed(g)
	return g, nil
}

func (t *Tracker) Reset() {
	t.mu.Lock()
	t.goal, t.notice, t.partial = nil, false, 0
	t.mu.Unlock()
}

func (t *Tracker) Restore(g Goal) {
	t.mu.Lock()
	t.goal, t.notice, t.partial = nil, false, 0
	if g.ID == "" || strings.TrimSpace(g.Objective) == "" || g.Status == StatusDropped {
		t.mu.Unlock()
		return
	}
	paused := g.Status == StatusActive
	if paused {
		g.Status, g.UpdatedAt = StatusPaused, t.now().UnixMilli()
	}
	t.goal = &g
	t.mu.Unlock()
	if paused {
		t.changed(g)
	}
}

func (t *Tracker) Account(u llm.Usage) {
	spent := u.InputTokens + u.OutputTokens + u.CacheWriteTokens
	if spent <= 0 {
		return
	}
	t.mu.Lock()
	g := t.goal
	if g == nil || !g.accounting() {
		t.mu.Unlock()
		return
	}
	g.TokensUsed += spent
	g.UpdatedAt = t.now().UnixMilli()
	flipped := g.Status == StatusActive && g.TokenBudget > 0 && g.TokensUsed >= g.TokenBudget
	if flipped {
		g.Status = StatusBudgetLimited
		t.notice = true
	}
	snapshot := *g
	t.mu.Unlock()
	if flipped {
		t.changed(snapshot)
	}
}

func (t *Tracker) AddTime(id string, d time.Duration) {
	t.mu.Lock()
	g := t.goal
	if g == nil || g.ID != id || d <= 0 {
		t.mu.Unlock()
		return
	}
	t.partial += d
	whole := t.partial / time.Second
	t.partial -= whole * time.Second
	g.TimeUsedSeconds += int64(whole)
	g.UpdatedAt = t.now().UnixMilli()
	snapshot := *g
	t.mu.Unlock()
	t.changed(snapshot)
}

func (t *Tracker) OnPrompt(ctx context.Context) ([]llm.Message, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.notice {
		return nil, nil
	}
	t.notice = false
	if t.goal == nil || t.goal.Status != StatusBudgetLimited {
		return nil, nil
	}
	return []llm.Message{llm.UserText(BudgetLimitPrompt(*t.goal))}, nil
}

func (t *Tracker) Summary() string {
	g, ok := t.Current()
	if !ok || !g.Live() {
		return ""
	}
	s := "goal " + string(g.Status)
	switch {
	case g.TokenBudget > 0:
		s += " " + formatTokens(g.TokensUsed) + "/" + formatTokens(g.TokenBudget)
	case g.TokensUsed > 0:
		s += " " + formatTokens(g.TokensUsed)
	}
	return s
}

const resumeInput = "Continue working toward the goal."

func (t *Tracker) Command(args string) (prompt, notice string, err error) {
	args = strings.TrimSpace(args)
	sub, rest, _ := strings.Cut(args, " ")
	rest = strings.TrimSpace(rest)
	switch strings.ToLower(sub) {
	case "", "show":
		g, ok := t.Current()
		if !ok {
			return "", "No goal. /goal <objective> sets one; the agent then works until it can prove the objective is met.", nil
		}
		return "", Describe(g), nil
	case "set":
		return t.setCommand(rest)
	case "pause":
		g, err := t.Pause()
		if err != nil {
			return "", "", err
		}
		return "", "Goal paused. /goal resume continues it.\n" + Describe(g), nil
	case "resume":
		g, err := t.Resume()
		if err != nil {
			return "", "", err
		}
		return resumeInput, "Goal resumed.\n" + Describe(g), nil
	case "drop":
		if _, err := t.Drop(); err != nil {
			return "", "", err
		}
		return "", "Goal dropped.", nil
	case "budget":
		tokens, err := parseBudget(rest)
		if err != nil {
			return "", "", err
		}
		g, err := t.SetBudget(tokens)
		if err != nil {
			return "", "", err
		}
		return "", "Goal budget updated.\n" + Describe(g), nil
	}
	return t.setCommand(args)
}

func (t *Tracker) setCommand(objective string) (string, string, error) {
	if strings.TrimSpace(objective) == "" {
		return "", "", errors.New("usage: /goal <objective>")
	}
	g, err := t.Set(objective, 0)
	if err != nil {
		return "", "", err
	}
	return g.Objective, "Goal set. The agent keeps working until it can show the objective is met; Esc pauses it, /goal budget <tokens> caps it.", nil
}

func parseBudget(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "":
		return 0, errors.New("usage: /goal budget <tokens|off>")
	case "off", "none", "0":
		return 0, nil
	}
	scale := 1
	switch {
	case strings.HasSuffix(s, "k"):
		scale, s = 1000, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		scale, s = 1000000, strings.TrimSuffix(s, "m")
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("the budget must be a positive number of tokens such as 200000 or 200k, or off")
	}
	return int(n * float64(scale)), nil
}

func Describe(g Goal) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal (%s): %s\n", g.Status, g.Objective)
	fmt.Fprintf(&b, "Tokens: %s used", formatTokens(g.TokensUsed))
	if left, ok := g.Remaining(); ok {
		fmt.Fprintf(&b, " of %s (%s left)", formatTokens(g.TokenBudget), formatTokens(left))
	}
	if g.TimeUsedSeconds > 0 {
		fmt.Fprintf(&b, " · time %s", (time.Duration(g.TimeUsedSeconds) * time.Second).String())
	}
	return b.String()
}

func formatTokens(n int) string {
	switch {
	case n >= 1000000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000), ".0") + "M"
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	}
	return strconv.Itoa(n)
}

func newID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
