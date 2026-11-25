package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadTestConfig(t *testing.T, content string) configFile {
	t.Helper()
	f, err := loadConfigFile(writeConfig(t, content))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadConfigFile(t *testing.T) {
	f, err := loadConfigFile(writeConfig(t, `
providers:
  cacheRetention: long
  maxInFlightRequests: 8
  broken:
    baseUrl: [x]
    apiKey: !op read x
  mygw:
    api: openai-completions
    baseUrl: https://gw.example/v1
    apiKey: "!op read op://vault/gw"
    headers:
      X-Team: platform
    models:
      - id: ignored
`))
	if err != nil {
		t.Fatalf("entries other than the selected provider must not fail the load: %v", err)
	}
	got, err := applyProvider(config{model: "mygw/m1"}, f.Providers)
	want := config{model: "m1", api: llm.ProtocolOpenAICompletions, baseURL: "https://gw.example/v1", apiKeyConfig: "!op read op://vault/gw"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, %v, want %+v", got, err, want)
	}
	if f, err := loadConfigFile(filepath.Join(t.TempDir(), "missing.yml")); err != nil || f.Providers != nil {
		t.Errorf("missing file should mean no config, got %v, %v", f, err)
	}
}

func TestLoadConfigPermissions(t *testing.T) {
	f, err := loadConfigFile(writeConfig(t, `
permissions:
  allow:
    - Read
    - Bash(git status)
  ask:
    - Bash(git push *)
  deny:
    - Bash(rm -rf *)
`))
	if err != nil {
		t.Fatal(err)
	}
	rules := buildRules(f.Permissions)
	if len(rules.Allow) != 2 || len(rules.Ask) != 1 || len(rules.Deny) != 1 {
		t.Fatalf("parsed rules wrong: %+v", rules)
	}
	if rules.Allow[0].Tool != "Read" || rules.Allow[0].Spec != "" {
		t.Errorf("bare tool rule must have empty spec: %+v", rules.Allow[0])
	}
	if rules.Allow[1].Tool != "Bash" || rules.Allow[1].Spec != "git status" {
		t.Errorf("spec rule parsed wrong: %+v", rules.Allow[1])
	}
	if rules.Deny[0].Spec != "rm -rf *" {
		t.Errorf("deny spec parsed wrong: %+v", rules.Deny[0])
	}
}

func TestLoadConfigFileErrors(t *testing.T) {
	cases := []struct{ content, want string }{
		{"providers: [", "config.yml"},
		{"permissions:\n  deny: Bash(rm -rf *)\n", "config.yml"},
		{"permissions:\n  defaultMode: yolo\n", "defaultMode"},
	}
	for _, c := range cases {
		if _, err := loadConfigFile(writeConfig(t, c.content)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: got %v, want error containing %q", c.content, err, c.want)
		}
	}
}

func TestApplyProvider(t *testing.T) {
	providers := loadTestConfig(t, `
providers:
  mygw:
    api: openai-completions
    baseUrl: https://gw.example/v1
    apiKey: "!op read x"
  or:
    api: openai-completions
    baseUrl: https://or.example/v1
  anthropic:
    apiKey: sk-ant
  openai:
    apiKey: sk-oai
`).Providers
	cases := []struct{ in, want config }{
		{config{model: "claude-x"}, config{model: "claude-x", api: llm.ProtocolAnthropicMessages}},
		{config{model: "mygw/m1"}, config{model: "m1", api: llm.ProtocolOpenAICompletions, baseURL: "https://gw.example/v1", apiKeyConfig: "!op read x"}},
		{config{model: "mygw/m1", api: llm.ProtocolOpenAIResponses, baseURL: "https://other/v1"}, config{model: "m1", api: llm.ProtocolOpenAIResponses, baseURL: "https://other/v1", apiKeyConfig: "!op read x"}},
		{config{model: "or/anthropic/claude"}, config{model: "anthropic/claude", api: llm.ProtocolOpenAICompletions, baseURL: "https://or.example/v1"}},
		{config{model: "unknown/m"}, config{model: "unknown/m", api: llm.ProtocolAnthropicMessages}},
		{config{model: "anthropic/claude-x"}, config{model: "claude-x", api: llm.ProtocolAnthropicMessages, apiKeyConfig: "sk-ant"}},
		{config{model: "openai/gpt-x"}, config{model: "gpt-x", api: llm.ProtocolOpenAIResponses, apiKeyConfig: "sk-oai"}},
	}
	for _, c := range cases {
		if got, err := applyProvider(c.in, providers); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("applyProvider(%+v) = %+v, %v, want %+v", c.in, got, err, c.want)
		}
	}
}

func TestApplyProviderErrors(t *testing.T) {
	providers := loadTestConfig(t, `
providers:
  mygw:
    api: openai-completions
  noapi:
    baseUrl: https://x.example/v1
  google:
    api: google-generative-ai
  cacheRetention: long
  badurl:
    api: openai-completions
    baseUrl: [x]
  unquoted:
    api: openai-completions
    apiKey: !op read x
  listkey:
    api: openai-completions
    apiKey: [a, b]
`).Providers
	cases := []struct{ model, want string }{
		{"mygw/", "no model id"},
		{"noapi/m", "no api"},
		{"google/gemini", "unsupported api"},
		{"cacheRetention/m", "cacheRetention"},
		{"badurl/m", "badurl"},
		{"unquoted/m", "quote apiKey"},
		{"listkey/m", "must be a string"},
	}
	for _, c := range cases {
		if _, err := applyProvider(config{model: c.model}, providers); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want error containing %q", c.model, err, c.want)
		}
	}
}

func TestApplyModelSelector(t *testing.T) {
	roles := map[string]any{"default": " mygw/m1:high ", "smol": "x"}
	cases := []struct {
		in       config
		fallback llm.ThinkingLevel
		model    string
		thinking llm.ThinkingLevel
	}{
		{config{}, "", "mygw/m1", llm.ThinkingHigh},
		{config{thinking: llm.ThinkingLow}, "", "mygw/m1", llm.ThinkingLow},
		{config{model: "other/m2"}, "", "other/m2", ""},
		{config{model: "other/m2"}, llm.ThinkingMedium, "other/m2", llm.ThinkingMedium},
		{config{model: "other/m2:max"}, llm.ThinkingMedium, "other/m2", llm.ThinkingMax},
		{config{model: "ollama/qwen3:8b"}, "", "ollama/qwen3:8b", ""},
	}
	for _, c := range cases {
		got, err := applyModelSelector(c.in, roles, c.fallback)
		if err != nil || got.model != c.model || got.modelLabel != c.model || got.thinking != c.thinking {
			t.Errorf("applyModelSelector(%+v, %q) = model %q, label %q, thinking %q, %v; want model %q, thinking %q",
				c.in, c.fallback, got.model, got.modelLabel, got.thinking, err, c.model, c.thinking)
		}
	}
	for _, r := range []map[string]any{nil, {"default": 42}, {"default": "  "}} {
		if _, err := applyModelSelector(config{}, r, ""); err == nil || !strings.Contains(err.Error(), "modelRoles.default") {
			t.Errorf("%v: want an error naming modelRoles.default, got %v", r, err)
		}
	}
}

func TestApplyConfigIgnoresUnknownThinkingLevel(t *testing.T) {
	for _, level := range []string{"auto", "inherit", "huge"} {
		f := loadTestConfig(t, "defaultThinkingLevel: "+level+"\nmodelRoles:\n  default: m\n")
		got, err := applyConfig(config{}, f)
		if err != nil || got.thinking != "" {
			t.Errorf("defaultThinkingLevel %s: got thinking %q, %v; want it ignored", level, got.thinking, err)
		}
	}
}

func TestApplyConfig(t *testing.T) {
	f := loadTestConfig(t, `
defaultThinkingLevel: medium
modelRoles:
  default: mygw/m1
permissions:
  defaultMode: plan
  allow: [Read]
providers:
  mygw:
    api: openai-completions
    baseUrl: https://gw.example/v1
    apiKey: "!op read x"
`)
	got, err := applyConfig(config{}, f)
	if err != nil {
		t.Fatal(err)
	}
	if got.model != "m1" || got.modelLabel != "mygw/m1" || got.thinking != llm.ThinkingMedium ||
		got.api != llm.ProtocolOpenAICompletions || got.baseURL != "https://gw.example/v1" || got.apiKeyConfig != "!op read x" {
		t.Errorf("model selection wrong: %+v", got)
	}
	if got.approvalMode() != permission.ModePlan || len(got.rules().Allow) != 1 {
		t.Errorf("permissions wrong: mode %q, rules %+v", got.approvalMode(), got.rules())
	}
}

func TestApplyConfigApprovalMode(t *testing.T) {
	plan := configFile{Permissions: permissionsSection{DefaultMode: permission.ModePlan}}
	cases := []struct {
		flag string
		file configFile
		want permission.Mode
	}{
		{"", configFile{}, permission.ModeAcceptEdits},
		{"", plan, permission.ModePlan},
		{"bypass", plan, permission.ModeBypass},
	}
	for _, c := range cases {
		got, err := applyConfig(config{model: "m", approval: c.flag}, c.file)
		if err != nil || got.approvalMode() != c.want {
			t.Errorf("--approval %q, defaultMode %q: got %q, %v, want %q", c.flag, c.file.Permissions.DefaultMode, got.approvalMode(), err, c.want)
		}
	}
}

func TestLoadConfigModelRoles(t *testing.T) {
	f, err := loadConfigFile(writeConfig(t, "modelRoles:\n  default: mygw/m1\n  slow:\n    nested: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := f.ModelRoles["default"].(string); got != "mygw/m1" {
		t.Errorf("modelRoles.default not read: %+v", f.ModelRoles)
	}
}

// 测试进程把自己当成取 key 的命令再运行一次，按环境变量决定表现
func TestHelperProcess(t *testing.T) {
	switch os.Getenv("PI_KEY_HELPER") {
	case "":
		t.Skip("only runs as a child process of the key command tests")
	case "ok":
		fmt.Println("  secret-123  ")
	case "fail":
		fmt.Fprint(os.Stderr, "vault locked")
		os.Exit(3)
	case "hang":
		time.Sleep(time.Minute)
	}
	os.Exit(0)
}

func TestRunKeyCommand(t *testing.T) {
	helper := []string{os.Args[0], "-test.run=^TestHelperProcess$"}
	cases := []struct {
		mode    string
		timeout time.Duration
		want    string
		wantErr string
	}{
		{"ok", 5 * time.Second, "secret-123", ""},
		{"fail", 5 * time.Second, "", "vault locked"},
		{"empty", 5 * time.Second, "", "printed nothing"},
		{"hang", 200 * time.Millisecond, "", "timed out"},
	}
	for _, c := range cases {
		t.Setenv("PI_KEY_HELPER", c.mode)
		got, err := runKeyCommand(helper, c.timeout)
		if got != c.want || (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: got %q, %v", c.mode, got, err)
		}
	}
	if _, err := runKeyCommand([]string{"pi-no-such-program"}, time.Second); err == nil {
		t.Error("missing program: want error")
	}
}

func TestResolveAPIKey(t *testing.T) {
	t.Setenv("PI_KEY_HELPER", "ok")
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Setenv("MY_GW_KEY", "from-env")
	cases := []struct{ value, want string }{
		{"", ""},
		{"sk-literal", "sk-literal"},
		{"MY_GW_KEY", "MY_GW_KEY"},
		{`!"` + os.Args[0] + `" -test.run=^TestHelperProcess$`, "secret-123"},
	}
	for _, c := range cases {
		if got, err := resolveAPIKey(c.value); err != nil || got != c.want {
			t.Errorf("resolveAPIKey(%q) = %q, %v, want %q", c.value, got, err, c.want)
		}
	}
	for _, value := range []string{"!pi-no-such-program", `!"unterminated`} {
		if _, err := resolveAPIKey(value); err == nil {
			t.Errorf("%s: want error", value)
		}
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{`powershell -NoProfile -File C:\Users\Mi\.omp\gw-key.ps1`, []string{"powershell", "-NoProfile", "-File", `C:\Users\Mi\.omp\gw-key.ps1`}},
		{`op read "op://dev/my key"`, []string{"op", "read", "op://dev/my key"}},
		{`  a  'b c'  ""  `, []string{"a", "b c", ""}},
	}
	for _, c := range cases {
		if got, err := splitArgs(c.in); err != nil || !slices.Equal(got, c.want) {
			t.Errorf("splitArgs(%q) = %q, %v, want %q", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "   ", `op "unterminated`} {
		if _, err := splitArgs(bad); err == nil {
			t.Errorf("splitArgs(%q): want error", bad)
		}
	}
}

func TestFirstRunTextWhenNoModelIsConfigured(t *testing.T) {
	f := loadTestConfig(t, "")
	_, err := applyConfig(config{}, f)
	if err == nil {
		t.Fatal("no model must be an error")
	}
	msg := err.Error()
	for _, want := range []string{"--model", "providers:", "modelRoles:", "default:", "~/.omp/config.yml", "never from the environment"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the first-run message must mention %q:\n%s", want, msg)
		}
	}
}

func TestMCPServersAreParsedFromTheConfigFile(t *testing.T) {
	f := loadTestConfig(t, `
modelRoles:
  default: mygw/m1
mcpServers:
  browser:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-browser"]
    env:
      TOKEN: abc
`)
	got, err := applyConfig(config{}, f)
	if err != nil {
		t.Fatal(err)
	}
	e, ok := got.mcp["browser"]
	if !ok || e.Command != "npx" || len(e.Args) != 2 || e.Env["TOKEN"] != "abc" {
		t.Fatalf("mcpServers not parsed: %+v", got.mcp)
	}
}

func TestHooksAreParsedFromTheConfigFile(t *testing.T) {
	f := loadTestConfig(t, `
modelRoles:
  default: mygw/m1
hooks:
  preTool:
    - command: omp-guard
      args: ["--strict"]
      timeoutSeconds: 5
  stop:
    - command: omp-check
`)
	got, err := applyConfig(config{}, f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.hooks.PreTool) != 1 || got.hooks.PreTool[0].Command != "omp-guard" || got.hooks.PreTool[0].Timeout != 5 {
		t.Fatalf("preTool not parsed: %+v", got.hooks.PreTool)
	}
	if len(got.hooks.Stop) != 1 || got.hooks.Stop[0].Command != "omp-check" {
		t.Fatalf("stop not parsed: %+v", got.hooks.Stop)
	}
	set := hookSet(got, "")
	if len(set.PreTool) != 1 || len(set.Stop) != 1 || len(set.PostTool) != 0 {
		t.Fatalf("hook set = %+v", set)
	}
	if set.PreTool[0].Timeout != 5*time.Second {
		t.Errorf("timeout = %s", set.PreTool[0].Timeout)
	}
}
