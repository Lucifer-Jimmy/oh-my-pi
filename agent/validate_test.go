package agent

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/llm"
	"github.com/Lucifer-Jimmy/oh-my-pi/session"
)

const argsSchema = `{
  "type": "object",
  "properties": {
    "path":  {"type": "string"},
    "limit": {"type": "integer"},
    "ratio": {"type": "number"},
    "all":   {"type": "boolean"},
    "mode":  {"type": "string", "enum": ["content", "count"]},
    "tags":  {"type": "array", "items": {"type": "string"}},
    "note":  {"type": ["string", "null"]}
  },
  "required": ["path"],
  "additionalProperties": false
}`

func TestValidateArgsAcceptsAndCorrects(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"valid arguments pass byte for byte", `{"path": "a",  "limit": 5}`, `{"path": "a",  "limit": 5}`},
		{"numeric string becomes an integer", `{"path":"a","limit":"20"}`, `{"limit":20,"path":"a"}`},
		{"integral float becomes an integer", `{"path":"a","limit":20.0}`, `{"limit":20,"path":"a"}`},
		{"exponent becomes an integer", `{"path":"a","limit":1e2}`, `{"limit":100,"path":"a"}`},
		{"numeric string becomes a number", `{"path":"a","ratio":" 0.5 "}`, `{"path":"a","ratio":0.5}`},
		{"boolean spelling becomes a boolean", `{"path":"a","all":"True"}`, `{"all":true,"path":"a"}`},
		{"null optional property is dropped", `{"path":"a","limit":null}`, `{"path":"a"}`},
		{"null is kept where the schema allows it", `{"path":"a","note":null}`, `{"path":"a","note":null}`},
		{"large integers keep every digit", `{"path":"a","limit":12345678901234567,"all":"false"}`, `{"all":false,"limit":12345678901234567,"path":"a"}`},
		{"array items are corrected too", `{"path":"a","tags":["x","y"],"limit":"1"}`, `{"limit":1,"path":"a","tags":["x","y"]}`},
	}
	for _, c := range cases {
		got, problems := validateArgs(json.RawMessage(argsSchema), json.RawMessage(c.args))
		if len(problems) > 0 {
			t.Errorf("%s: unexpected problems %q", c.name, problems)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%s: got %s, want %s", c.name, got, c.want)
		}
	}
}

func TestValidateArgsRejects(t *testing.T) {
	cases := []struct {
		name, args, want string
	}{
		{"empty arguments still need required properties", ``, `missing required property "path"`},
		{"null required property counts as missing", `{"path":null}`, `missing required property "path"`},
		{"wrong type is named", `{"path":"a","limit":"all"}`, `"limit" must be an integer, got "all"`},
		{"fraction is not an integer", `{"path":"a","limit":2.5}`, `"limit" must be an integer, got 2.5`},
		{"hex is not a number", `{"path":"a","ratio":"0x10"}`, `"ratio" must be a number, got "0x10"`},
		{"only true and false become booleans", `{"path":"a","all":"yes"}`, `"all" must be a boolean, got "yes"`},
		{"unknown property lists the valid ones", `{"path":"a","timeout":5}`, `unknown property "timeout" (valid properties: all, limit, mode, note, path, ratio, tags)`},
		{"several unknown properties share one line", `{"path":"a","x":1,"y":2}`, `unknown properties "x", "y" (valid properties: all, limit, mode, note, path, ratio, tags)`},
		{"enum lists the allowed values", `{"path":"a","mode":"lines"}`, `"mode" must be one of "content", "count", got "lines"`},
		{"array items are checked", `{"path":"a","tags":["x",5]}`, `"tags[1]" must be a string, got 5`},
		{"arguments must be an object", `[1]`, `arguments must be an object, got an array`},
		{"invalid JSON is reported", `{"path":`, `arguments are not valid JSON: unexpected EOF`},
		{"trailing data is invalid", `{"path":"a"} x`, `arguments are not valid JSON: unexpected data after the arguments`},
		{"long strings are shortened", `{"path":"a","limit":"` + strings.Repeat("9x", 30) + `"}`, `"limit" must be an integer, got "` + strings.Repeat("9x", 20) + `..."`},
	}
	for _, c := range cases {
		got, problems := validateArgs(json.RawMessage(argsSchema), json.RawMessage(c.args))
		if got != nil {
			t.Errorf("%s: rejected arguments must not be returned, got %s", c.name, got)
		}
		if !slices.Contains(problems, c.want) {
			t.Errorf("%s: got %q, want it to contain %q", c.name, problems, c.want)
		}
	}
}

func TestValidateArgsReportsEveryProblem(t *testing.T) {
	_, problems := validateArgs(json.RawMessage(argsSchema), json.RawMessage(`{"limit":"x","mode":"lines"}`))
	want := []string{
		`"limit" must be an integer, got "x"`,
		`"mode" must be one of "content", "count", got "lines"`,
		`missing required property "path"`,
	}
	if !slices.Equal(problems, want) {
		t.Errorf("got %q, want %q", problems, want)
	}
}

func TestValidateArgsChecksNestedObjects(t *testing.T) {
	schema := `{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"old":{"type":"string"},"count":{"type":"integer"}},"required":["old"],"additionalProperties":false}}}}`
	got, problems := validateArgs(json.RawMessage(schema), json.RawMessage(`{"edits":[{"old":"a","count":"2"}]}`))
	if len(problems) > 0 || string(got) != `{"edits":[{"count":2,"old":"a"}]}` {
		t.Errorf("nested values are corrected: got %s %q", got, problems)
	}
	_, problems = validateArgs(json.RawMessage(schema), json.RawMessage(`{"edits":[{"new":"b"}]}`))
	want := []string{
		`unknown property "edits[0].new" (valid properties: count, old)`,
		`missing required property "edits[0].old"`,
	}
	if !slices.Equal(problems, want) {
		t.Errorf("nested problems carry their path: got %q", problems)
	}
}

func TestValidateArgsLeavesOpenSchemasAlone(t *testing.T) {
	open := `{"type":"object","properties":{"a":{"type":"string"}}}`
	in := `{"a":"x","extra":1}`
	if got, problems := validateArgs(json.RawMessage(open), json.RawMessage(in)); len(problems) > 0 || string(got) != in {
		t.Errorf("an open schema tolerates unknown properties: got %s %q", got, problems)
	}
	if got, problems := validateArgs(json.RawMessage(`not json`), json.RawMessage(`{"x":1}`)); len(problems) > 0 || string(got) != `{"x":1}` {
		t.Errorf("an unreadable schema validates nothing: got %s %q", got, problems)
	}
	closed := `{"type":"object","properties":{},"additionalProperties":false}`
	if _, problems := validateArgs(json.RawMessage(closed), json.RawMessage(`{"x":1}`)); !slices.Equal(problems, []string{`unknown property "x" (no properties are accepted)`}) {
		t.Errorf("a tool without arguments says so: %q", problems)
	}
}

type argsTool struct {
	called int
	got    json.RawMessage
}

func (a *argsTool) Name() string        { return "args" }
func (a *argsTool) Description() string { return "args" }
func (a *argsTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"limit":{"type":"integer"}},"required":["path"],"additionalProperties":false}`)
}
func (a *argsTool) Execute(ctx context.Context, tc ToolContext, args json.RawMessage) ToolResult {
	a.called++
	a.got = args
	return ToolResult{Content: []llm.ContentBlock{llm.TextBlock("ok")}}
}

func runArgsTool(t *testing.T, input string) (*argsTool, []Event) {
	t.Helper()
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		toolTurn("t1", "args", input, llm.StopReasonToolUse),
		textTurn("done"),
	}}
	reg := NewRegistry()
	tool := &argsTool{}
	reg.Register(tool)
	evs, err := collectEvents(NewLoop(reg, RunDeps{CallModel: m.call}, nil, Options{}).Run(context.Background(), session.NewMemory(nil), "go"))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	return tool, evs
}

func TestLoopRejectsInvalidArguments(t *testing.T) {
	tool, evs := runArgsTool(t, `{"limit":"many","timeout":5}`)
	if tool.called != 0 {
		t.Fatal("a call with invalid arguments must not run")
	}
	text, isErr, ok := toolResultOf(evs, "t1")
	want := `args: invalid arguments: "limit" must be an integer, got "many"; unknown property "timeout" (valid properties: limit, path); missing required property "path"`
	if !ok || !isErr || text != want {
		t.Errorf("got ok=%v isErr=%v text=%q", ok, isErr, text)
	}
	if r := lastResult(evs); r == nil || r.Reason != ReasonCompleted || r.Text != "done" {
		t.Fatalf("the run continues after a rejected call, got %+v", r)
	}
}

func TestLoopCorrectsArgumentsBeforeExecute(t *testing.T) {
	tool, evs := runArgsTool(t, `{"path":"a.go","limit":"20"}`)
	if tool.called != 1 || string(tool.got) != `{"limit":20,"path":"a.go"}` {
		t.Fatalf("the tool must receive the corrected arguments, got %s", tool.got)
	}
	if _, isErr, _ := toolResultOf(evs, "t1"); isErr {
		t.Error("a corrected call succeeds")
	}
}

func TestLoopPassesValidArgumentsVerbatim(t *testing.T) {
	in := `{"path":"a.go","limit":3}`
	tool, _ := runArgsTool(t, in)
	if string(tool.got) != in {
		t.Errorf("valid arguments must reach the tool unchanged, got %s", tool.got)
	}
}
