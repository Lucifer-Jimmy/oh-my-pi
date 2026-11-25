package tools

import "github.com/Lucifer-Jimmy/oh-my-pi/agent"

type Config struct {
	CWD        string
	Workspaces []string
	Shell      Shell
	State      *FileState
	DeniedGlob func(rel string) bool
	DeniedGrep func(rel string) bool
}

func Builtin(c Config) []agent.Tool {
	state := c.State
	if state == nil {
		state = NewFileState()
	}
	return []agent.Tool{
		&Read{CWD: c.CWD, Workspaces: c.Workspaces, State: state},
		&Glob{CWD: c.CWD, Workspaces: c.Workspaces, Denied: c.DeniedGlob},
		&Grep{CWD: c.CWD, Workspaces: c.Workspaces, Denied: c.DeniedGrep},
		&Edit{CWD: c.CWD, Workspaces: c.Workspaces, State: state},
		&Write{CWD: c.CWD, Workspaces: c.Workspaces, State: state},
		&Bash{CWD: c.CWD, Workspaces: c.Workspaces, Shell: c.Shell},
		&Ask{},
		&Todo{},
	}
}
