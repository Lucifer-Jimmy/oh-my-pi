package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"go.yaml.in/yaml/v3"
)

const keyCommandTimeout = 10 * time.Second

type configFile struct {
	Providers            map[string]yaml.Node      `yaml:"providers"`
	Permissions          permissionsSection        `yaml:"permissions"`
	ModelRoles           map[string]any            `yaml:"modelRoles"`
	DefaultThinkingLevel llm.ThinkingLevel         `yaml:"defaultThinkingLevel"`
	Compaction           *compactionSection        `yaml:"compaction"`
	MCPServers           map[string]mcpServerEntry `yaml:"mcpServers"`
	Hooks                hooksSection              `yaml:"hooks"`
	Memory               memorySection             `yaml:"memory"`
}

type hooksSection struct {
	PreTool  []hookEntry `yaml:"preTool"`
	PostTool []hookEntry `yaml:"postTool"`
	Stop     []hookEntry `yaml:"stop"`
	Prompt   []hookEntry `yaml:"prompt"`
}

type hookEntry struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args"`
	Env     map[string]string `yaml:"env"`
	Timeout int               `yaml:"timeoutSeconds"`
}

type mcpServerEntry struct {
	Command   string            `yaml:"command"`
	Args      []string          `yaml:"args"`
	Env       map[string]string `yaml:"env"`
	URL       string            `yaml:"url"`
	Transport string            `yaml:"transport"`
}

type compactionSection struct {
	ContextWindow    int  `yaml:"contextWindow"`
	KeepRecentTokens int  `yaml:"keepRecentTokens"`
	ReserveTokens    int  `yaml:"reserveTokens"`
	MinElideTokens   int  `yaml:"minElideTokens"`
	Disabled         bool `yaml:"disabled"`
}

type permissionsSection struct {
	Allow       []string        `yaml:"allow"`
	Ask         []string        `yaml:"ask"`
	Deny        []string        `yaml:"deny"`
	DefaultMode permission.Mode `yaml:"defaultMode"`
}

type providerConfig struct {
	API     llm.Protocol `yaml:"api"`
	BaseURL string       `yaml:"baseUrl"`
	APIKey  configValue  `yaml:"apiKey"`
}

var builtinProviderAPIs = map[string]llm.Protocol{
	"anthropic": llm.ProtocolAnthropicMessages,
	"openai":    llm.ProtocolOpenAIResponses,
}

type configValue string

func (v *configValue) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return fmt.Errorf("line %d: apiKey must be a string", n.Line)
	}
	if !strings.HasPrefix(n.ShortTag(), "!!") {
		return fmt.Errorf(`line %d: quote apiKey values that start with !, like apiKey: "!command"`, n.Line)
	}
	*v = configValue(n.Value)
	return nil
}

func applyConfigFile(c config) (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	f, err := loadConfigFile(path)
	if err != nil {
		return config{}, err
	}
	return applyConfig(c, f)
}

func applyConfig(c config, f configFile) (config, error) {
	c.permRules = []permission.Rules{buildRules(f.Permissions)}
	if c.approval == "" {
		c.approval = string(f.Permissions.DefaultMode)
	}
	if f.Compaction != nil {
		c.compaction = *f.Compaction
	}
	c.memoryBackend = f.Memory.Backend
	c.mcp = f.MCPServers
	c.hooks = f.Hooks
	c, err := applyModelSelector(c, f.ModelRoles, f.DefaultThinkingLevel)
	if err != nil {
		return config{}, err
	}
	return applyProvider(c, f.Providers)
}

func applyModelSelector(c config, roles map[string]any, defaultThinking llm.ThinkingLevel) (config, error) {
	if c.model == "" {
		c.model, _ = roles["default"].(string)
		c.model = strings.TrimSpace(c.model)
	}
	if c.model == "" {
		return config{}, errNoModel
	}
	model, level := splitThinkingSuffix(c.model)
	c.model, c.modelLabel = model, model
	if c.thinking == "" {
		c.thinking = level
	}
	if c.thinking == "" && slices.Contains(thinkingLevels, defaultThinking) {
		c.thinking = defaultThinking
	}
	return c, nil
}

var errNoModel = errors.New(firstRunText)

const firstRunText = `no model is configured. Pick one of:

  omp --model <provider>/<model> "your prompt"
      built-in providers: anthropic, openai (apiKey only is enough)

  omp --model <id> --api openai-completions --base-url <url> "your prompt"
      any OpenAI-compatible endpoint

or write ~/.omp/config.yml once (modelRoles.default is the startup model):

  providers:
    anthropic:
      apiKey: "!<command that prints your API key>"
  modelRoles:
    default: anthropic/claude-sonnet-5

API keys are read only from that file, never from the environment or a flag.
The full key reference is in the README.`

func splitThinkingSuffix(selector string) (string, llm.ThinkingLevel) {
	i := strings.LastIndexByte(selector, ':')
	if i <= 0 {
		return selector, ""
	}
	level := llm.ThinkingLevel(selector[i+1:])
	if !slices.Contains(thinkingLevels, level) {
		return selector, ""
	}
	return selector[:i], level
}

func buildRules(s permissionsSection) permission.Rules {
	return permission.Rules{
		Allow: toRules(s.Allow),
		Ask:   toRules(s.Ask),
		Deny:  toRules(s.Deny),
	}
}

func toRules(entries []string) []permission.Rule {
	out := make([]permission.Rule, 0, len(entries))
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		tool, spec := e, ""
		if open := strings.IndexByte(e, '('); open > 0 && strings.HasSuffix(e, ")") {
			tool, spec = strings.TrimSpace(e[:open]), strings.TrimSpace(e[open+1:len(e)-1])
		}
		out = append(out, permission.Rule{Tool: tool, Spec: spec})
	}
	return out
}

func loadConfigFile(path string) (configFile, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return configFile{}, nil
	}
	if err != nil {
		return configFile{}, err
	}
	var f configFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return configFile{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := f.validate(); err != nil {
		return configFile{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}

func (f configFile) validate() error {
	if _, err := permission.ParseMode(string(f.Permissions.DefaultMode)); err != nil {
		return fmt.Errorf("permissions.defaultMode: %w", err)
	}
	if c := f.Compaction; c != nil {
		switch {
		case c.ContextWindow < 0:
			return errors.New("compaction.contextWindow must not be negative")
		case c.KeepRecentTokens < 0:
			return errors.New("compaction.keepRecentTokens must not be negative")
		case c.ReserveTokens < 0:
			return errors.New("compaction.reserveTokens must not be negative")
		case c.MinElideTokens < 0:
			return errors.New("compaction.minElideTokens must not be negative")
		}
	}
	return nil
}

func applyProvider(c config, providers map[string]yaml.Node) (config, error) {
	name, id, hasSlash := strings.Cut(c.model, "/")
	if node, found := providers[name]; hasSlash && found {
		var p providerConfig
		if err := node.Decode(&p); err != nil {
			return config{}, fmt.Errorf("provider %s in config.yml: %w", name, err)
		}
		if p.API == "" {
			p.API = builtinProviderAPIs[name]
		}
		switch {
		case id == "":
			return config{}, fmt.Errorf("--model %s has no model id after the provider name", c.model)
		case c.api == "" && p.API == "":
			return config{}, fmt.Errorf("provider %s in config.yml has no api", name)
		case c.api == "" && !slices.Contains(knownAPIs, p.API):
			return config{}, fmt.Errorf("provider %s in config.yml has unsupported api %q", name, p.API)
		}
		c.model, c.apiKeyConfig = id, string(p.APIKey)
		if c.api == "" {
			c.api = p.API
		}
		if c.baseURL == "" {
			c.baseURL = p.BaseURL
		}
	}
	if c.api == "" {
		c.api = llm.ProtocolAnthropicMessages
	}
	return c, nil
}

func resolveAPIKey(value string) (string, error) {
	command, ok := strings.CutPrefix(value, "!")
	if !ok {
		return value, nil
	}
	argv, err := splitArgs(command)
	if err != nil {
		return "", fmt.Errorf("apiKey command %q: %w", command, err)
	}
	return runKeyCommand(argv, keyCommandTimeout)
}

func splitArgs(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	var quote rune
	inArg := false
	for _, r := range s {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inArg = r, true
		case r == ' ' || r == '\t':
			if inArg {
				args = append(args, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote", quote)
	}
	if inArg {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}

func runKeyCommand(argv []string, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = time.Second
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	key := strings.TrimSpace(string(out))
	var problem string
	switch {
	case ctx.Err() != nil:
		problem = "timed out after " + timeout.String()
	case err != nil:
		problem = "failed: " + err.Error()
	case key == "":
		problem = "printed nothing"
	default:
		return key, nil
	}
	if detail := strings.TrimSpace(stderr.String()); detail != "" {
		problem += ": " + detail
	}
	return "", fmt.Errorf("apiKey command %q %s", strings.Join(argv, " "), problem)
}
