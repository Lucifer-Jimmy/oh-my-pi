package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/permission"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
	"github.com/Lucifer-Jimmy/oh-my-pi/tools"
)

func storedGoal(t *testing.T, path string) goal.Goal {
	t.Helper()
	s, _, err := session.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var g goal.Goal
	if err := json.Unmarshal(s.Goal(), &g); err != nil {
		t.Fatalf("no goal stored: %v", err)
	}
	return g
}

func TestAGoalKeepsThePrintRunGoingUntilTheModelCompletesIt(t *testing.T) {
	dir := t.TempDir()
	store, err := session.Create(t.TempDir(), session.NewID(time.Now()), dir, "m")
	if err != nil {
		t.Fatal(err)
	}
	conv := &conversation{sess: store, store: store}
	goals := new(goal.Tracker)
	conv.attachGoal(goals)

	m := &stepModel{steps: []step{
		{id: "g1", tool: "goal", input: map[string]any{"op": "create", "objective": "make the build green"}},
		{text: "started"},
		{id: "g2", tool: "goal", input: map[string]any{"op": "complete"}},
		{text: "done, the build is green"},
	}}
	policy := permission.NewPolicy(permission.ModeAcceptEdits, permission.Rules{})
	deps := agent.RunDeps{CallModel: m.call}
	reg := assemble(dir, tools.ResolveShell(""), policy, deps, config{}, nil, nil, nil, goals, nil)
	opts := agent.Options{CWD: dir, Workspaces: []string{dir}, Hooks: withGoalPrompt(agent.Hooks{}, goals)}
	run := goalRunner(goals, policy, withPlanReminder(agent.NewLoop(reg, deps, nil, opts), policy))

	var out, errb bytes.Buffer
	code := runPrint(context.Background(), run, conv.sess, []string{"set a goal: make the build green"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if m.calls != 4 {
		t.Fatalf("the goal must keep the run going into a continuation, model called %d times", m.calls)
	}
	if !strings.Contains(out.String(), "done, the build is green") {
		t.Fatalf("the final answer is printed: %q", out.String())
	}
	continued := false
	for _, msg := range conv.sess.Messages() {
		if msg.Role == llm.RoleUser && len(msg.Content) > 0 && strings.HasPrefix(msg.Content[0].Text, "<goal_continuation>") {
			continued = true
		}
	}
	if !continued {
		t.Fatal("the automatic turn is recorded in the session")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if g := storedGoal(t, store.Path()); g.Status != goal.StatusComplete || g.Objective != "make the build green" {
		t.Fatalf("the completed goal is in the session file: %+v", g)
	}
}

func TestAResumedSessionBringsItsGoalBackPaused(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	dir := session.ProjectDir(root, cwd)
	s, err := session.Create(dir, session.NewID(time.Now()), cwd, "m")
	if err != nil {
		t.Fatal(err)
	}
	s.Append(llm.UserText("start"))
	b, _ := json.Marshal(goal.Goal{ID: "g1", Objective: "migrate the config\nand its docs", Status: goal.StatusActive, TokensUsed: 900})
	s.SetGoal(b)
	path := s.Path()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	conv, err := openConversation(config{resume: path, sessionsDir: root}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	goals := new(goal.Tracker)
	conv.attachGoal(goals)
	g, ok := goals.Current()
	if !ok || g.Status != goal.StatusPaused || g.TokensUsed != 900 {
		t.Fatalf("an active goal comes back paused: %+v", g)
	}
	if !strings.Contains(conv.note, "paused goal: migrate the config (/goal resume continues it)") {
		t.Fatalf("the startup note names the goal: %q", conv.note)
	}
	if err := conv.Close(); err != nil {
		t.Fatal(err)
	}
	if got := storedGoal(t, path); got.Status != goal.StatusPaused {
		t.Fatalf("the pause is written back to the session: %+v", got)
	}

	fresh := &conversation{}
	goals.OnChange = fresh.saveGoal
	fresh.restoreGoal(goals)
	if _, ok := goals.Current(); ok {
		t.Fatal("a conversation without a stored goal clears the tracker")
	}
}

func TestChildToolsLeaveOutWhatASubagentCannotUse(t *testing.T) {
	got := childTools([]string{"read", "task", "exit_plan_mode", "goal", "background", "bash"})
	if strings.Join(got, ",") != "read,bash" {
		t.Fatalf("child tools = %v", got)
	}
}
