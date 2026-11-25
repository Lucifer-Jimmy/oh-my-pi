package goal

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

func runTool(t *testing.T, tool *Tool, args string) agent.ToolResult {
	t.Helper()
	return tool.Execute(context.Background(), nil, json.RawMessage(args))
}

func text(res agent.ToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func TestSchemaMatchesTheArgumentStruct(t *testing.T) {
	var schema struct {
		Type                 string                     `json:"type"`
		Properties           map[string]json.RawMessage `json:"properties"`
		Required             []string                   `json:"required"`
		AdditionalProperties *bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal([]byte(toolSchema), &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Type != "object" || schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatal("the schema is a closed object")
	}
	var tags []string
	rt := reflect.TypeOf(toolArgs{})
	for i := range rt.NumField() {
		name, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ",")
		tags = append(tags, name)
	}
	var props []string
	for name := range schema.Properties {
		props = append(props, name)
	}
	slices.Sort(tags)
	slices.Sort(props)
	if !slices.Equal(tags, props) || !slices.Equal(schema.Required, []string{"op"}) {
		t.Fatalf("schema properties %v, struct tags %v, required %v", props, tags, schema.Required)
	}
}

func TestApprovalReadsOnlyForGet(t *testing.T) {
	tool := &Tool{}
	if got := tool.Approval(json.RawMessage(`{"op":"get"}`)).Tier; got != agent.TierRead {
		t.Fatalf("get = %v", got)
	}
	for _, op := range []string{"create", "resume", "complete", "drop"} {
		if got := tool.Approval(json.RawMessage(`{"op":"` + op + `"}`)).Tier; got != agent.TierWrite {
			t.Errorf("%s = %v", op, got)
		}
	}
}

func TestToolDrivesTheTracker(t *testing.T) {
	tr, _, _ := newTracker()
	tool := &Tool{Tracker: tr}
	if res := runTool(t, tool, `{"op":"get"}`); res.IsError || text(res) != "No goal is set." {
		t.Fatalf("get without a goal = %+v", res)
	}
	if res := runTool(t, tool, `{"op":"complete"}`); !res.IsError {
		t.Fatal("completing nothing is an error")
	}
	res := runTool(t, tool, `{"op":"create","objective":"all tests green","token_budget":5000}`)
	if res.IsError || !strings.Contains(text(res), "Goal (active): all tests green") {
		t.Fatalf("create = %+v", res)
	}
	if res := runTool(t, tool, `{"op":"create","objective":"other"}`); !res.IsError || !strings.Contains(text(res), "already has a goal") {
		t.Fatalf("a second create is refused: %+v", res)
	}
	tr.Pause()
	if res := runTool(t, tool, `{"op":"get"}`); !strings.Contains(text(res), "resume it before working on it") {
		t.Fatalf("a paused goal says how to go on: %q", text(res))
	}
	if res := runTool(t, tool, `{"op":"resume"}`); res.IsError {
		t.Fatalf("resume = %+v", res)
	}
	res = runTool(t, tool, `{"op":"complete"}`)
	if res.IsError || !strings.Contains(text(res), "Goal achieved") || !strings.Contains(text(res), "of 5000") {
		t.Fatalf("complete reports the usage: %q", text(res))
	}
	if g, _ := tr.Current(); g.Status != StatusComplete {
		t.Fatalf("status = %s", g.Status)
	}
	if res := runTool(t, tool, `{"op":"explode"}`); !res.IsError {
		t.Fatal("an unknown op is an error")
	}
	if res := runTool(t, &Tool{}, `{"op":"get"}`); !res.IsError {
		t.Fatal("a tool without a tracker says so")
	}
}
