package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/spf13/pflag"
	"golang.org/x/term"
)

type config struct {
	print         bool
	interactive   bool
	api           llm.Protocol
	model         string
	modelLabel    string
	baseURL       string
	apiKey        string
	apiKeyConfig  string
	thinking      llm.ThinkingLevel
	maxTokens     int
	maxTurns      int
	budgetTokens  int
	approval      string
	permRules     []permission.Rules
	prompts       []string
	compaction    compactionSection
	cont          bool
	resume        string
	noSession     bool
	sessionsDir   string
	mode          string
	extTools      string
	verify        string
	verifyTries   int
	tracePath     string
	mcp           map[string]mcpServerEntry
	hooks         hooksSection
	memoryBackend string
}

var (
	knownAPIs      = []llm.Protocol{llm.ProtocolAnthropicMessages, llm.ProtocolOpenAICompletions, llm.ProtocolOpenAIResponses}
	thinkingLevels = []llm.ThinkingLevel{llm.ThinkingOff, llm.ThinkingMinimal, llm.ThinkingLow, llm.ThinkingMedium, llm.ThinkingHigh, llm.ThinkingXHigh, llm.ThinkingMax}
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) int {
	cfg, code := parse(args, stdin, stderr)
	if code != 0 {
		return code
	}
	return dispatch(cfg, stdout, stderr)
}

func parse(args []string, stdin *os.File, stderr io.Writer) (config, int) {
	cfg, err := parseArgs(args, stderr)
	if errors.Is(err, pflag.ErrHelp) {
		return config{}, -1
	}
	if err != nil {
		return config{}, 2
	}
	if cfg.mode == "json" || cfg.mode == "rpc" {
		cfg.print = true
		if len(cfg.prompts) == 0 && !term.IsTerminal(int(stdin.Fd())) {
			piped, err := readPiped(stdin, stderr)
			if err != nil {
				fmt.Fprintln(stderr, "omp: reading stdin:", err)
				return config{}, 1
			}
			cfg.prompts = buildPrompts(piped, cfg.prompts)
		}
		if cfg.mode == "json" && len(cfg.prompts) == 0 {
			fmt.Fprintln(stderr, "omp: no prompt given")
			return config{}, 2
		}
		return cfg, 0
	}
	if term.IsTerminal(int(stdin.Fd())) {
		if cfg.print {
			if len(cfg.prompts) == 0 {
				fmt.Fprintln(stderr, "omp: no prompt given")
				return config{}, 2
			}
			return cfg, 0
		}
		if !term.IsTerminal(int(os.Stdout.Fd())) {
			fmt.Fprintln(stderr, "omp: output is not a terminal, use -p")
			return config{}, 2
		}
		cfg.interactive = true
		return cfg, 0
	}
	piped, err := readPiped(stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "omp: reading stdin:", err)
		return config{}, 1
	}
	cfg.print = true
	cfg.prompts = buildPrompts(piped, cfg.prompts)
	if len(cfg.prompts) == 0 {
		fmt.Fprintln(stderr, "omp: no prompt given")
		return config{}, 2
	}
	return cfg, 0
}

func parseArgs(args []string, stderr io.Writer) (config, error) {
	var c config
	var api, thinking string
	fs := pflag.NewFlagSet("omp", pflag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: omp [flags] [prompt]...")
		fmt.Fprintln(stderr, "Starts the interactive terminal UI; -p answers once and exits.")
		fmt.Fprintln(stderr, "API keys are read only from ~/.omp/config.yml, never from flags or environment variables.")
		fs.PrintDefaults()
	}
	fs.BoolVarP(&c.print, "print", "p", false, "print mode: answer once and exit")
	fs.StringVar(&api, "api", "", "anthropic-messages | openai-completions | openai-responses (default anthropic-messages)")
	fs.StringVar(&c.model, "model", "", "model id, or <provider>/<model> for a provider in ~/.omp/config.yml (default: modelRoles.default)")
	fs.StringVar(&c.baseURL, "base-url", "", "override the API base URL")
	fs.StringVar(&thinking, "thinking", "", "off | minimal | low | medium | high | xhigh | max")
	fs.IntVar(&c.maxTokens, "max-tokens", 0, "output token cap (default: provider fallback)")
	fs.IntVar(&c.maxTurns, "max-turns", 0, "maximum agent turns before stopping (default: unlimited)")
	fs.IntVar(&c.budgetTokens, "budget-tokens", 0, "stop when total tokens spent reach this (default: unlimited)")
	fs.StringVar(&c.approval, "approval", "", "default | acceptEdits | plan | bypass (default acceptEdits)")
	fs.BoolVarP(&c.cont, "continue", "c", false, "resume the most recent session in this directory")
	fs.StringVar(&c.resume, "resume", "", "resume a session by id, id prefix or file path")
	fs.BoolVar(&c.noSession, "no-session", false, "keep this conversation in memory only")
	fs.StringVar(&c.sessionsDir, "sessions-dir", "", "where sessions are stored (default: ~/.omp/sessions)")
	fs.StringVar(&c.mode, "mode", "text", "output mode: text | json | rpc")
	fs.StringVar(&c.extTools, "external-tools", "", "JSON file declaring tools the host executes (rpc mode)")
	fs.StringVar(&c.verify, "verify", "", "command that must exit 0 before the run is reported done")
	fs.IntVar(&c.verifyTries, "verify-attempts", 3, "how many times to re-prompt after a failed check")
	fs.StringVar(&c.tracePath, "trace", "", "write a JSONL trace of every event to this path")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	c.api, c.thinking, c.prompts = llm.Protocol(api), llm.ThinkingLevel(thinking), fs.Args()
	if err := c.validate(); err != nil {
		fmt.Fprintln(stderr, "omp:", err)
		return config{}, err
	}
	return c, nil
}

func (c config) validate() error {
	switch {
	case c.api != "" && !slices.Contains(knownAPIs, c.api):
		return fmt.Errorf("unknown --api %q", c.api)
	case c.thinking != "" && !slices.Contains(thinkingLevels, c.thinking):
		return fmt.Errorf("unknown --thinking %q", c.thinking)
	case c.maxTokens < 0:
		return errors.New("--max-tokens must not be negative")
	case c.maxTurns < 0:
		return errors.New("--max-turns must not be negative")
	case c.budgetTokens < 0:
		return errors.New("--budget-tokens must not be negative")
	case c.cont && c.resume != "":
		return errors.New("--continue and --resume cannot be combined")
	case c.noSession && (c.cont || c.resume != ""):
		return errors.New("--no-session cannot be combined with --continue or --resume")
	case c.verifyTries < 1:
		return errors.New("--verify-attempts must be at least 1")
	case c.mode != "text" && c.mode != "json" && c.mode != "rpc":
		return fmt.Errorf("unknown --mode %q", c.mode)
	}
	if _, err := permission.ParseMode(c.approval); err != nil {
		return err
	}
	return nil
}

func (c config) approvalMode() permission.Mode {
	if c.approval == "" {
		return permission.ModeAcceptEdits
	}
	m, err := permission.ParseMode(c.approval)
	if err != nil {
		return permission.ModeDefault
	}
	return m
}

func (c config) rules() permission.Rules {
	if len(c.permRules) > 0 {
		return c.permRules[0]
	}
	return permission.Rules{}
}
