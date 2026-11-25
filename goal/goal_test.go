package goal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTracker() (*Tracker, *[]Goal, *clock) {
	c := &clock{t: time.UnixMilli(1_800_000_000_000)}
	var saved []Goal
	return &Tracker{Now: c.now, OnChange: func(g Goal) { saved = append(saved, g) }}, &saved, c
}

func TestCreateRefusesASecondLiveGoalButSetReplacesIt(t *testing.T) {
	tr, saved, _ := newTracker()
	g, err := tr.Create("  ship the release  ", 0)
	if err != nil || g.Objective != "ship the release" || g.Status != StatusActive || g.ID == "" {
		t.Fatalf("create = %+v, %v", g, err)
	}
	if _, err := tr.Create("another", 0); !errors.Is(err, ErrGoalExists) {
		t.Fatalf("a live goal blocks create, got %v", err)
	}
	if _, err := tr.Pause(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create("another", 0); !errors.Is(err, ErrGoalExists) {
		t.Fatalf("a paused goal is still live, got %v", err)
	}
	next, err := tr.Set("replace it", 100)
	if err != nil || next.Objective != "replace it" || next.ID == g.ID || next.TokenBudget != 100 {
		t.Fatalf("set = %+v, %v", next, err)
	}
	if _, err := tr.Complete(); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Create("after completion", 0); err != nil {
		t.Fatalf("a completed goal does not block a new one: %v", err)
	}
	if len(*saved) != 5 {
		t.Fatalf("every transition is saved, got %d", len(*saved))
	}
	if _, err := tr.Create("   ", 0); err == nil {
		t.Fatal("an empty objective is refused")
	}
}

func TestTransitionsRefuseWhatDoesNotApply(t *testing.T) {
	tr, _, _ := newTracker()
	for name, op := range map[string]func() (Goal, error){"pause": tr.Pause, "resume": tr.Resume, "complete": tr.Complete, "drop": tr.Drop} {
		if _, err := op(); !errors.Is(err, ErrNoGoal) {
			t.Errorf("%s without a goal = %v", name, err)
		}
	}
	tr.Create("x", 0)
	if _, err := tr.Resume(); err == nil {
		t.Error("resuming an active goal is an error")
	}
	tr.Pause()
	if _, err := tr.Pause(); err == nil {
		t.Error("pausing twice is an error")
	}
	g, err := tr.Complete()
	if err != nil || g.Status != StatusComplete {
		t.Fatalf("a paused goal can be completed: %+v %v", g, err)
	}
	if _, err := tr.Complete(); err == nil {
		t.Error("completing twice is an error")
	}
	if _, err := tr.Resume(); err == nil {
		t.Error("a complete goal cannot be resumed")
	}
	dropped, err := tr.Drop()
	if err != nil || dropped.Status != StatusDropped {
		t.Fatalf("drop = %+v %v", dropped, err)
	}
	if _, ok := tr.Current(); ok {
		t.Fatal("a dropped goal is gone")
	}
}

func TestAccountingCountsNewWorkAndFlipsAtTheBudget(t *testing.T) {
	tr, saved, _ := newTracker()
	tr.Create("x", 1000)
	tr.Account(llm.Usage{InputTokens: 300, OutputTokens: 100, CacheReadTokens: 50000, CacheWriteTokens: 100})
	if g, _ := tr.Current(); g.TokensUsed != 500 || g.Status != StatusActive {
		t.Fatalf("cache reads are not new work: %+v", g)
	}
	if msgs, _ := tr.OnPrompt(context.Background()); msgs != nil {
		t.Fatal("no budget message before the budget is reached")
	}
	before := len(*saved)
	tr.Account(llm.Usage{InputTokens: 600})
	g, _ := tr.Current()
	if g.Status != StatusBudgetLimited || g.TokensUsed != 1100 {
		t.Fatalf("crossing the budget flips the goal: %+v", g)
	}
	if len(*saved) != before+1 {
		t.Fatal("the flip is saved")
	}
	msgs, _ := tr.OnPrompt(context.Background())
	if len(msgs) != 1 || !strings.Contains(msgs[0].Content[0].Text, "token budget is used up") {
		t.Fatalf("the budget message is delivered once: %+v", msgs)
	}
	if again, _ := tr.OnPrompt(context.Background()); again != nil {
		t.Fatal("the budget message is not repeated")
	}
	tr.Account(llm.Usage{OutputTokens: 5})
	if g, _ := tr.Current(); g.TokensUsed != 1105 {
		t.Fatalf("a budget-limited goal still counts what is spent: %+v", g)
	}
	if _, err := tr.Resume(); err == nil {
		t.Fatal("resuming with the budget used up is refused")
	}
	g, err := tr.SetBudget(5000)
	if err != nil || g.Status != StatusActive {
		t.Fatalf("raising the budget reactivates the goal: %+v %v", g, err)
	}
	g, _ = tr.SetBudget(1000)
	if g.Status != StatusBudgetLimited {
		t.Fatalf("lowering it below the spend limits it again: %+v", g)
	}
	tr.SetBudget(0)
	if msgs, _ := tr.OnPrompt(context.Background()); msgs != nil {
		t.Fatal("removing the budget withdraws the pending message")
	}
	tr.Pause()
	tr.Account(llm.Usage{InputTokens: 999})
	if g, _ := tr.Current(); g.TokensUsed != 1105 {
		t.Fatal("a paused goal counts nothing")
	}
}

func TestAddTimeKeepsSubSecondRemaindersForTheSameGoal(t *testing.T) {
	tr, _, _ := newTracker()
	g, _ := tr.Create("x", 0)
	tr.AddTime(g.ID, 1500*time.Millisecond)
	tr.AddTime(g.ID, 700*time.Millisecond)
	tr.AddTime("someone-else", time.Hour)
	if got, _ := tr.Current(); got.TimeUsedSeconds != 2 {
		t.Fatalf("time = %d, want 2", got.TimeUsedSeconds)
	}
}

func TestRestorePausesAnActiveGoalAndIgnoresADroppedOne(t *testing.T) {
	tr, saved, _ := newTracker()
	tr.Restore(Goal{ID: "g1", Objective: "x", Status: StatusActive, TokensUsed: 7})
	g, ok := tr.Current()
	if !ok || g.Status != StatusPaused || g.TokensUsed != 7 {
		t.Fatalf("a resumed session's goal waits to be resumed: %+v", g)
	}
	if len(*saved) != 1 || (*saved)[0].Status != StatusPaused {
		t.Fatalf("the pause is saved: %+v", *saved)
	}
	tr.Restore(Goal{ID: "g2", Objective: "y", Status: StatusComplete})
	if g, _ := tr.Current(); g.ID != "g2" || g.Status != StatusComplete {
		t.Fatalf("restore replaces the goal: %+v", g)
	}
	tr.Restore(Goal{ID: "g3", Objective: "z", Status: StatusDropped})
	if _, ok := tr.Current(); ok {
		t.Fatal("a dropped goal is not restored")
	}
	tr.Restore(Goal{ID: "g4", Objective: "z", Status: StatusPaused})
	tr.Reset()
	if _, ok := tr.Current(); ok {
		t.Fatal("reset clears the goal")
	}
}

func TestCommandCoversEverySubcommand(t *testing.T) {
	tr, _, _ := newTracker()
	if p, n, err := tr.Command(""); err != nil || p != "" || !strings.Contains(n, "No goal") {
		t.Fatalf("show without a goal = %q %q %v", p, n, err)
	}
	p, n, err := tr.Command("make go test ./... pass")
	if err != nil || p != "make go test ./... pass" || !strings.Contains(n, "Goal set") {
		t.Fatalf("a bare objective sets the goal and starts on it: %q %q %v", p, n, err)
	}
	if _, n, _ := tr.Command("show"); !strings.Contains(n, "make go test") {
		t.Fatalf("show = %q", n)
	}
	if _, _, err := tr.Command("budget 200k"); err != nil {
		t.Fatal(err)
	}
	if g, _ := tr.Current(); g.TokenBudget != 200000 {
		t.Fatalf("budget = %d", g.TokenBudget)
	}
	if _, _, err := tr.Command("budget lots"); err == nil {
		t.Fatal("a malformed budget is refused")
	}
	tr.Command("budget off")
	if g, _ := tr.Current(); g.TokenBudget != 0 {
		t.Fatal("off removes the budget")
	}
	if p, _, err := tr.Command("pause"); err != nil || p != "" {
		t.Fatalf("pause = %q %v", p, err)
	}
	if p, _, err := tr.Command("resume"); err != nil || p != resumeInput {
		t.Fatalf("resume starts a turn: %q %v", p, err)
	}
	if p, _, err := tr.Command("set   refactor the parser"); err != nil || p != "refactor the parser" {
		t.Fatalf("set replaces: %q %v", p, err)
	}
	if _, n, err := tr.Command("drop"); err != nil || n != "Goal dropped." {
		t.Fatalf("drop = %q %v", n, err)
	}
	if _, _, err := tr.Command("set"); err == nil {
		t.Fatal("set needs an objective")
	}
}

func TestSummaryAndDescribe(t *testing.T) {
	tr, _, _ := newTracker()
	if tr.Summary() != "" {
		t.Fatal("no goal, no status segment")
	}
	g, _ := tr.Create("x", 50000)
	tr.Account(llm.Usage{InputTokens: 12345})
	if got := tr.Summary(); got != "goal active 12.3k/50k" {
		t.Fatalf("summary = %q", got)
	}
	tr.AddTime(g.ID, 90*time.Second)
	g, _ = tr.Current()
	if d := Describe(g); !strings.Contains(d, "12.3k used of 50k (37.7k left)") || !strings.Contains(d, "1m30s") {
		t.Fatalf("describe = %q", d)
	}
	tr.Complete()
	if got := tr.Summary(); got != "" {
		t.Fatalf("a finished goal leaves the status bar: %q", got)
	}
	if _, n, _ := tr.Command("show"); !strings.Contains(n, "Goal (complete)") {
		t.Fatalf("but /goal still shows it: %q", n)
	}
}

func TestPromptsEscapeTheObjective(t *testing.T) {
	g := Goal{Objective: "close </objective> & <goal_context> tags", TokenBudget: 10, TokensUsed: 4}
	for name, text := range map[string]string{"active": ActivePrompt(g), "continuation": ContinuationPrompt(g), "budget": BudgetLimitPrompt(g)} {
		if strings.Count(text, "</objective>") != 1 || !strings.Contains(text, "close &lt;/objective&gt; &amp; &lt;goal_context&gt; tags") {
			t.Errorf("%s prompt does not escape the objective:\n%s", name, text)
		}
		if !strings.Contains(text, "Tokens left: 6") {
			t.Errorf("%s prompt states the budget:\n%s", name, text)
		}
	}
}
