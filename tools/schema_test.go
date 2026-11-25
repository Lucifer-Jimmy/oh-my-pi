package tools

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Lucifer-Jimmy/oh-my-pi/agent"
)

var schemaKinds = map[string][]reflect.Kind{
	"string":  {reflect.String},
	"integer": {reflect.Int, reflect.Int64, reflect.Int32},
	"number":  {reflect.Float64, reflect.Float32},
	"boolean": {reflect.Bool},
	"array":   {reflect.Slice},
	"object":  {reflect.Struct, reflect.Map},
}

type toolSchema struct {
	Type       string `json:"type"`
	Properties map[string]struct {
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"properties"`
	Required             []string `json:"required"`
	AdditionalProperties *bool    `json:"additionalProperties"`
}

func TestSchemasMatchArguments(t *testing.T) {
	cases := []struct {
		tool agent.Tool
		args reflect.Type
	}{
		{&Read{}, reflect.TypeFor[readArgs]()},
		{&Glob{}, reflect.TypeFor[globArgs]()},
		{&Grep{}, reflect.TypeFor[grepArgs]()},
		{&Edit{}, reflect.TypeFor[editArgs]()},
		{&Write{}, reflect.TypeFor[writeArgs]()},
		{&Bash{}, reflect.TypeFor[bashArgs]()},
	}
	for _, c := range cases {
		name := c.tool.Name()
		var s toolSchema
		if err := json.Unmarshal(c.tool.Schema(), &s); err != nil {
			t.Errorf("%s: schema is not valid JSON: %v", name, err)
			continue
		}
		if s.Type != "object" || s.AdditionalProperties == nil || *s.AdditionalProperties {
			t.Errorf("%s: schema must be a closed object", name)
		}
		fields := map[string]reflect.Kind{}
		for i := 0; i < c.args.NumField(); i++ {
			f := c.args.Field(i)
			tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			fields[tag] = f.Type.Kind()
		}
		for prop, p := range s.Properties {
			kind, ok := fields[prop]
			switch {
			case !ok:
				t.Errorf("%s: schema property %q has no argument field", name, prop)
			case !slices.Contains(schemaKinds[p.Type], kind):
				t.Errorf("%s: %q is %s in the schema but %s in the arguments", name, prop, p.Type, kind)
			}
			if strings.TrimSpace(p.Description) == "" {
				t.Errorf("%s: %q has no description", name, prop)
			}
		}
		for field := range fields {
			if _, ok := s.Properties[field]; !ok {
				t.Errorf("%s: argument field %q is not in the schema", name, field)
			}
		}
		for _, r := range s.Required {
			if _, ok := s.Properties[r]; !ok {
				t.Errorf("%s: required %q is not a property", name, r)
			}
		}
	}
}

func TestGrepSchemaListsEveryOutputMode(t *testing.T) {
	var s struct {
		Properties struct {
			OutputMode struct {
				Enum []string `json:"enum"`
			} `json:"output_mode"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((&Grep{}).Schema(), &s); err != nil {
		t.Fatal(err)
	}
	g, dir := newGrep(t)
	mkfile(t, dir, "a.go", "hello\n")
	for _, mode := range s.Properties.OutputMode.Enum {
		if out, isErr := runGrep(t, g, `{"pattern":"hello","output_mode":"`+mode+`"}`); isErr {
			t.Errorf("schema offers %q but grep rejects it: %s", mode, out)
		}
	}
	if len(s.Properties.OutputMode.Enum) != 3 {
		t.Errorf("want the three output modes, got %q", s.Properties.OutputMode.Enum)
	}
}
