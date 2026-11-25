package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
	"github.com/Lucifer-Jimmy/oh-my-pi/goal"
	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/plan"
	"github.com/Lucifer-Jimmy/oh-my-pi/presentation"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

func replayTexts(msgs []llm.Message) []string {
	s := &presentation.State{InjectedTags: injectedTags}
	s.Replay(msgs)
	var out []string
	for _, it := range s.Items {
		if it.Kind == presentation.KindUser {
			out = append(out, it.Text)
		}
	}
	return out
}

func TestInjectedTagsHideWhatTheEngineInjects(t *testing.T) {
	g := goal.Goal{Objective: "ship the release", Status: goal.StatusActive, TokenBudget: 1000}
	active := plan.WithReminder("plan this", true, nil)
	ended := plan.WithReminder("now build it", false, []llm.Message{llm.UserText(active)})
	msgs := []llm.Message{
		llm.UserText(active),
		llm.UserText(ended),
		llm.UserText(goal.ActivePrompt(g) + "\n\nkeep going"),
		llm.UserText(goal.ContinuationPrompt(g)),
		llm.UserText(goal.BudgetLimitPrompt(g)),
		llm.UserText("<hook role=\"system\">\nlint first\n</hook>"),
	}
	got := strings.Join(replayTexts(msgs), "|")
	if want := "plan this|now build it|keep going"; got != want {
		t.Fatalf("replayed user turns = %q, want %q", got, want)
	}
}

func TestInjectedTagsHideBackgroundNotifications(t *testing.T) {
	sent := false
	deps := agent.RunDeps{
		CallModel: (&stepModel{steps: []step{{text: "noted"}}}).call,
		Notify: func() []agent.Notification {
			if sent {
				return nil
			}
			sent = true
			return []agent.Notification{{ID: "bg-1", Text: "build finished\nexit 0"}}
		},
	}
	sess := session.NewMemory(nil)
	for _, err := range agent.NewLoop(agent.NewRegistry(), deps, nil, agent.Options{}).Run(context.Background(), sess, "start the build") {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n := len(sess.Messages()); n != 3 {
		t.Fatalf("expected the input, the notification and the answer, got %d messages", n)
	}
	if got := strings.Join(replayTexts(sess.Messages()), "|"); got != "start the build" {
		t.Fatalf("replayed user turns = %q", got)
	}
}
