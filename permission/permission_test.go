package permission

import (
	"slices"
	"strings"
	"testing"
)

func req(tool string, tier Tier, specs []Spec, mode Mode, rules Rules) Request {
	return Request{Tool: tool, Specs: specs, Tier: tier, Mode: mode, Rules: rules}
}

func TestDenyBeatsEverything(t *testing.T) {
	rules := Rules{Deny: []Rule{{Tool: "bash", Spec: "rm *"}}, Allow: []Rule{{Tool: "*"}}}
	cases := []struct {
		name        string
		mode        Mode
		interactive bool
	}{
		{"bypass", ModeBypass, true},
		{"acceptEdits", ModeAcceptEdits, true},
		{"default", ModeDefault, true},
		{"non-interactive", ModeBypass, false},
	}
	for _, c := range cases {
		r := req("bash", TierExec, []Spec{{KindCommand, "rm -rf /"}}, c.mode, rules)
		r.Interactive = c.interactive
		if d := Evaluate(r); d.Behavior != Deny {
			t.Errorf("%s: deny rule must win, got %v (%s)", c.name, d.Behavior, d.Reason)
		}
	}
}

func TestPlanBlocksWriteAndExecEvenUnderBypass(t *testing.T) {
	for _, tier := range []Tier{TierWrite, TierExec} {
		if d := Evaluate(req("write", tier, nil, ModePlan, Rules{})); d.Behavior != Deny {
			t.Errorf("plan must deny %s, got %v", tier, d.Behavior)
		}
	}
	if d := Evaluate(req("read", TierRead, nil, ModePlan, Rules{})); d.Behavior != Allow {
		t.Errorf("plan must allow read, got %v", d.Behavior)
	}
}

func TestBypassAllowsAll(t *testing.T) {
	for _, tier := range []Tier{TierRead, TierWrite, TierExec} {
		if d := Evaluate(req("bash", tier, nil, ModeBypass, Rules{})); d.Behavior != Allow {
			t.Errorf("bypass must allow %s, got %v", tier, d.Behavior)
		}
	}
}

func TestReadOnlyAllowedInDefaultAndAcceptEdits(t *testing.T) {
	for _, mode := range []Mode{ModeDefault, ModeAcceptEdits} {
		if d := Evaluate(req("read", TierRead, nil, mode, Rules{})); d.Behavior != Allow {
			t.Errorf("%s must allow read, got %v", mode, d.Behavior)
		}
	}
}

func TestAcceptEditsAllowsWriteNotExec(t *testing.T) {
	if d := Evaluate(req("edit", TierWrite, nil, ModeAcceptEdits, Rules{})); d.Behavior != Allow {
		t.Errorf("acceptEdits must allow write, got %v", d.Behavior)
	}
	r := req("bash", TierExec, nil, ModeAcceptEdits, Rules{})
	r.Interactive = true
	if d := Evaluate(r); d.Behavior != Ask {
		t.Errorf("acceptEdits must ask for exec, got %v", d.Behavior)
	}
}

func TestDefaultAsksForWrite(t *testing.T) {
	r := req("edit", TierWrite, nil, ModeDefault, Rules{})
	r.Interactive = true
	if d := Evaluate(r); d.Behavior != Ask {
		t.Errorf("default must ask for write, got %v", d.Behavior)
	}
}

func TestNonInteractiveAskBecomesDeny(t *testing.T) {
	r := req("bash", TierExec, nil, ModeDefault, Rules{})
	r.Interactive = false
	d := Evaluate(r)
	if d.Behavior != Deny {
		t.Fatalf("no host must deny, got %v", d.Behavior)
	}
	if !strings.Contains(d.Reason, "no interactive approval") {
		t.Errorf("reason must explain how to fix it, got %q", d.Reason)
	}
}

func TestRuleOrderDenyThenAskThenAllow(t *testing.T) {
	rules := Rules{
		Deny:  []Rule{{Tool: "bash", Spec: "git push*"}},
		Ask:   []Rule{{Tool: "bash", Spec: "git *"}},
		Allow: []Rule{{Tool: "bash"}},
	}
	r := req("bash", TierExec, []Spec{{KindCommand, "git push origin main"}}, ModeDefault, rules)
	r.Interactive = true
	if d := Evaluate(r); d.Behavior != Deny {
		t.Errorf("deny must beat ask, got %v", d.Behavior)
	}

	r = req("bash", TierExec, []Spec{{KindCommand, "git status"}}, ModeDefault, rules)
	r.Interactive = true
	if d := Evaluate(r); d.Behavior != Ask {
		t.Errorf("ask must beat allow, got %v", d.Behavior)
	}

	r = req("bash", TierExec, []Spec{{KindCommand, "go test ./..."}}, ModeDefault, rules)
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("allow rule must apply, got %v", d.Behavior)
	}
}

func TestBareToolRuleMatchesWholeTool(t *testing.T) {
	rules := Rules{Deny: []Rule{{Tool: "bash"}}}
	if d := Evaluate(req("bash", TierRead, nil, ModeBypass, rules)); d.Behavior != Deny {
		t.Errorf("bare tool deny must apply to any input, got %v", d.Behavior)
	}
	if d := Evaluate(req("read", TierRead, nil, ModeBypass, rules)); d.Behavior != Allow {
		t.Errorf("bare tool deny must not hit other tools, got %v", d.Behavior)
	}
}

func TestWildcardToolRule(t *testing.T) {
	rules := Rules{Deny: []Rule{{Tool: "*"}}}
	if d := Evaluate(req("anything", TierRead, nil, ModeBypass, rules)); d.Behavior != Deny {
		t.Errorf("wildcard deny must hit every tool, got %v", d.Behavior)
	}
}

func TestNoRuleFallsThroughToMode(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "grep"}}}
	if d := Evaluate(req("bash", TierExec, nil, ModeBypass, rules)); d.Behavior != Allow {
		t.Errorf("unmatched rule must not block bypass, got %v", d.Behavior)
	}
}

func TestSplitCommandSplitsCompound(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"git status; rm -rf /", []string{"git status", "rm -rf /"}},
		{"ls && cat x", []string{"ls", "cat x"}},
		{"ls || echo no", []string{"ls", "echo no"}},
		{"cat a | grep b", []string{"cat a", "grep b"}},
		{"echo a\necho b", []string{"echo a", "echo b"}},
		{"ls & bg", []string{"ls", "bg"}},
	}
	for _, c := range cases {
		got := SplitCommand(c.cmd)
		var vals []string
		for _, s := range got {
			vals = append(vals, s.Value)
		}
		if !slices.Equal(vals, c.want) {
			t.Errorf("SplitCommand(%q) = %v, want %v", c.cmd, vals, c.want)
		}
	}
}

func TestSplitCommandCannotBeBypassedBySemicolon(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "bash", Spec: "git status"}}}
	r := req("bash", TierExec, SplitCommand("git status; rm -rf /"), ModeDefault, rules)
	r.Interactive = true
	if d := Evaluate(r); d.Behavior == Allow {
		t.Errorf("compound command must not inherit the allow rule: %s", d.Reason)
	}
}

func TestSplitCommandStripsWrappers(t *testing.T) {
	cases := map[string]string{
		"timeout 30 npm test": "npm test",
		"nice -n 10 go build": "go build",
		"nohup ./run.sh":      "./run.sh",
		"time go test":        "go test",
		"stdbuf -oL cat f":    "cat f",
	}
	for in, want := range cases {
		got := SplitCommand(in)
		if len(got) != 1 || got[0].Value != want {
			t.Errorf("SplitCommand(%q) = %v, want %q", in, got, want)
		}
	}
}

func TestSplitCommandOpaqueIsUnmatchable(t *testing.T) {
	for _, cmd := range []string{
		"echo $(whoami)",
		"echo `id`",
		"cat > out.txt",
	} {
		got := SplitCommand(cmd)
		if len(got) != 1 || got[0].Value != "" {
			t.Errorf("SplitCommand(%q) must be unmatchable, got %v", cmd, got)
		}
		rules := Rules{Allow: []Rule{{Tool: "bash", Spec: "echo*"}}}
		r := req("bash", TierExec, got, ModeDefault, rules)
		r.Interactive = true
		if d := Evaluate(r); d.Behavior == Allow {
			t.Errorf("%q must not match a spec rule", cmd)
		}
	}
}

func TestBareToolAllowCoversOpaqueCommand(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "bash"}}}
	r := req("bash", TierExec, SplitCommand("echo $(whoami)"), ModeDefault, rules)
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("bare tool allow must cover every invocation, got %v", d.Behavior)
	}
}

func TestMatchCommandPrefix(t *testing.T) {
	cases := []struct {
		rule, cmd string
		want      bool
	}{
		{"git commit *", "git commit -m x", true},
		{"git commit *", "git commit", true},
		{"git commit *", "git commitx", false},
		{"git", "git status", false},
		{"git *", "git status", true},
		{"npm test", "npm test", true},
		{"npm test", "npm test --watch", false},
		{"*", "anything", true},
	}
	for _, c := range cases {
		if got := matchCommand(c.rule, c.cmd); got != c.want {
			t.Errorf("matchCommand(%q, %q) = %v, want %v", c.rule, c.cmd, got, c.want)
		}
	}
}

func TestMatchPathGlob(t *testing.T) {
	cases := []struct {
		rule, path string
		want       bool
	}{
		{"./src/**", "src/a/b.go", true},
		{"./src/**", "src/a.go", true},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/sub/a.go", false},
		{"**/.env", ".env", true},
		{"**/.env", "a/b/.env", true},
		{"**/.env", "a/b/.env.local", false},
		{".env", "a/.env", false},
		{"src/**/test.go", "src/a/b/test.go", true},
	}
	for _, c := range cases {
		if got := matchPath(c.rule, c.path); got != c.want {
			t.Errorf("matchPath(%q, %q) = %v, want %v", c.rule, c.path, got, c.want)
		}
	}
}

func TestMatchPathWindowsSeparators(t *testing.T) {
	if !matchPath("src/**", `src\a\b.go`) {
		t.Error("backslashes must be normalised before matching")
	}
	if !matchPath(`src\**`, "src/a.go") {
		t.Error("rule-side backslashes must be normalised too")
	}
}

func TestPathRuleBlocksDotEnv(t *testing.T) {
	rules := Rules{Deny: []Rule{{Tool: "read", Spec: "**/.env"}}}
	r := req("read", TierRead, []Spec{{KindPath, ".env"}}, ModeBypass, rules)
	if d := Evaluate(r); d.Behavior != Deny {
		t.Errorf("deny on path must apply, got %v", d.Behavior)
	}
}

func TestEmptySpecNeverMatches(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "bash", Spec: "*"}}}
	r := req("bash", TierExec, []Spec{{KindCommand, ""}}, ModeDefault, rules)
	r.Interactive = true
	if d := Evaluate(r); d.Behavior == Allow {
		t.Error("empty spec must not match any rule")
	}
}

func TestParseMode(t *testing.T) {
	for _, m := range []Mode{ModeDefault, ModeAcceptEdits, ModePlan, ModeBypass} {
		got, err := ParseMode(string(m))
		if err != nil || got != m {
			t.Errorf("ParseMode(%q) = %v, %v", m, got, err)
		}
	}
	if got, err := ParseMode(""); err != nil || got != ModeDefault {
		t.Errorf("empty must mean default, got %v, %v", got, err)
	}
	if _, err := ParseMode("yolo"); err == nil {
		t.Error("unknown mode must be rejected")
	}
}

func TestRuleToolNameIsCaseInsensitive(t *testing.T) {
	rules := Rules{Deny: []Rule{{Tool: "Bash", Spec: "rm *"}}, Allow: []Rule{{Tool: "Read"}}}
	if d := Evaluate(req("bash", TierExec, []Spec{{KindCommand, "rm -rf x"}}, ModeBypass, rules)); d.Behavior != Deny {
		t.Errorf("Bash(...) must match tool bash, got %v", d.Behavior)
	}
	r := req("read", TierExec, nil, ModeDefault, rules)
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("Read must match tool read, got %v", d.Behavior)
	}
}

func TestForcePromptOverridesModeAutoAllow(t *testing.T) {
	r := req("edit", TierWrite, nil, ModeAcceptEdits, Rules{})
	r.ForcePrompt = true
	r.Interactive = true
	if d := Evaluate(r); d.Behavior != Ask {
		t.Errorf("acceptEdits must still ask when the tool forces a prompt, got %v", d.Behavior)
	}
	r.Interactive = false
	if d := Evaluate(r); d.Behavior != Deny {
		t.Errorf("forced prompt without a host must deny, got %v", d.Behavior)
	}
}

func TestForcePromptYieldsToRulesAndBypass(t *testing.T) {
	r := req("edit", TierWrite, []Spec{{KindPath, ".git/hooks/pre-commit"}}, ModeBypass, Rules{})
	r.ForcePrompt = true
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("bypass ignores forced prompts, got %v", d.Behavior)
	}

	r.Mode = ModeAcceptEdits
	r.Rules = Rules{Allow: []Rule{{Tool: "edit", Spec: ".git/**"}}}
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("an explicit allow rule beats a forced prompt, got %v", d.Behavior)
	}

	r.Rules = Rules{Deny: []Rule{{Tool: "edit", Spec: ".git/**"}}}
	if d := Evaluate(r); d.Behavior != Deny {
		t.Errorf("deny still wins, got %v", d.Behavior)
	}
}

func TestSplitCommandRespectsQuotes(t *testing.T) {
	cases := map[string][]string{
		`grep "a|b" f.txt`:            {`grep a|b f.txt`},
		`git status "x && y"`:         {`git status x && y`},
		`echo 'a;b' c`:                {`echo a;b c`},
		`echo "a\"b" c`:               {`echo a"b c`},
		`grep "a>b" f; rm -rf out`:    {`grep a>b f`, `rm -rf out`},
		`bash -c "make && make test"`: {`bash -c make && make test`},
	}
	for in, want := range cases {
		var got []string
		for _, s := range SplitCommand(in) {
			got = append(got, s.Value)
		}
		if !slices.Equal(got, want) {
			t.Errorf("SplitCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitCommandQuotedSeparatorKeepsTheAllowRule(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "bash", Spec: "grep *"}}}
	r := req("bash", TierExec, SplitCommand(`grep "a|b" f.txt`), ModeDefault, rules)
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("a quoted separator is one segment, so the allow rule must hold: %v (%s)", d.Behavior, d.Reason)
	}
	bypass := req("bash", TierExec, SplitCommand(`grep "a|b" f; rm -rf /`), ModeDefault, rules)
	bypass.Interactive = true
	if d := Evaluate(bypass); d.Behavior == Allow {
		t.Errorf("a real separator after a quoted one must still split: %s", d.Reason)
	}
}

func TestSplitCommandUnbalancedQuoteIsUnmatchable(t *testing.T) {
	for _, cmd := range []string{`echo "unclosed`, `grep 'oops x`} {
		got := SplitCommand(cmd)
		if len(got) != 1 || got[0].Value != "" {
			t.Errorf("SplitCommand(%q) must be unmatchable, got %v", cmd, got)
		}
	}
}

func TestSplitCommandIgnoresHarmlessRedirects(t *testing.T) {
	cases := map[string][]string{
		"go test ./... 2>&1":                {"go test ./..."},
		"command -v go >/dev/null 2>&1":     {"go"},
		"ls 2> /dev/null":                   {"ls"},
		"echo warn >&2":                     {"echo warn"},
		"git status >/dev/null; rm -rf out": {"git status", "rm -rf out"},
	}
	for in, want := range cases {
		var got []string
		for _, s := range SplitCommand(in) {
			got = append(got, s.Value)
		}
		if !slices.Equal(got, want) {
			t.Errorf("SplitCommand(%q) = %q, want %q", in, got, want)
		}
	}
	for _, opaqueCmd := range []string{"cat a > out.txt", "go test 2>err.log", "echo x >>/dev/nullx"} {
		if got := SplitCommand(opaqueCmd); len(got) != 1 || got[0].Value != "" {
			t.Errorf("%q writes a file and must stay unmatchable, got %+v", opaqueCmd, got)
		}
	}
}

func TestAllowRuleMatchesCommandWithStderrMerge(t *testing.T) {
	rules := Rules{Allow: []Rule{{Tool: "Bash", Spec: "go test *"}}}
	r := req("bash", TierExec, SplitCommand("go test ./... 2>&1"), ModeDefault, rules)
	if d := Evaluate(r); d.Behavior != Allow {
		t.Errorf("2>&1 must not defeat the allow rule, got %v (%s)", d.Behavior, d.Reason)
	}
}
