package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

type ModeControl interface {
	Mode() permission.Mode
	SetMode(permission.Mode)
}

type EffortControl interface {
	Level() llm.ThinkingLevel
	SetLevel(llm.ThinkingLevel)
}

type GoalControl interface {
	Command(args string) (prompt, notice string, err error)
	Summary() string
}

type MemoryControl interface {
	Command(args string) (string, error)
}

type Options struct {
	Runner       Runner
	Compact      CompactFunc
	NewSession   func() (session.Session, error)
	Resume       func(arg string) (session.Session, string, error)
	ListSessions func() ([]session.Info, error)
	Rename       func(title string) error
	Export       func(path string) (string, error)
	Delete       func(id string) error
	Session      session.Session
	Host         *Host
	Policy       ModeControl
	Effort       EffortControl
	Goal         GoalControl
	Memory       MemoryControl
	Model        string
	Window       func() int
	SwitchModel  func(selector string) (string, error)
	Steer        func(text string)
	CWD          string
	Prompts      []string
	Projectors   map[string]presentation.ToolProjector
	InjectedTags []string
	Note         string
}

func Run(ctx context.Context, opts Options) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	p := tea.NewProgram(newModel(ctx, opts), tea.WithContext(ctx))
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}
