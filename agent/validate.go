package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var jsonNumberLiteral = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

func validateArgs(schema, args json.RawMessage) (json.RawMessage, []string) {
	var root map[string]any
	if json.Unmarshal(schema, &root) != nil {
		return args, nil
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	v, err := decodeArgs(args)
	if err != nil {
		return nil, []string{"arguments are not valid JSON: " + err.Error()}
	}
	c := &argChecker{}
	out := c.check(root, v, "")
	if len(c.problems) > 0 {
		return nil, c.problems
	}
	if !c.changed {
		return args, nil
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, []string{"arguments could not be re-encoded: " + err.Error()}
	}
	return b, nil
}

func decodeArgs(args json.RawMessage) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(args))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after the arguments")
	}
	return v, nil
}

type argChecker struct {
	problems []string
	changed  bool
}

func (c *argChecker) fail(format string, a ...any) {
	c.problems = append(c.problems, fmt.Sprintf(format, a...))
}

func (c *argChecker) check(s map[string]any, v any, path string) any {
	types := typesOf(s)
	if len(types) > 0 && !slices.ContainsFunc(types, func(t string) bool { return isType(t, v) }) {
		fixed, ok := coerce(types, v)
		if !ok {
			c.fail("%s must be %s, got %s", subject(path), typeNames(types), describe(v))
			return v
		}
		v, c.changed = fixed, true
	}
	if enum, ok := s["enum"].([]any); ok && !slices.ContainsFunc(enum, func(e any) bool { return sameValue(e, v) }) {
		c.fail("%s must be one of %s, got %s", subject(path), literals(enum), describe(v))
		return v
	}
	switch x := v.(type) {
	case map[string]any:
		c.object(s, x, path)
	case []any:
		if items, ok := s["items"].(map[string]any); ok {
			for i := range x {
				x[i] = c.check(items, x[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	}
	return v
}

func (c *argChecker) object(s, obj map[string]any, path string) {
	props, _ := s["properties"].(map[string]any)
	var unknown []string
	for _, key := range slices.Sorted(maps.Keys(obj)) {
		sub, known := props[key]
		if !known {
			unknown = append(unknown, strconv.Quote(join(path, key)))
			continue
		}
		ps, _ := sub.(map[string]any)
		if obj[key] == nil && dropsNull(ps) {
			delete(obj, key)
			c.changed = true
			continue
		}
		obj[key] = c.check(ps, obj[key], join(path, key))
	}
	if len(unknown) > 0 && s["additionalProperties"] == false {
		noun := "property"
		if len(unknown) > 1 {
			noun = "properties"
		}
		if len(props) == 0 {
			c.fail("unknown %s %s (no properties are accepted)", noun, strings.Join(unknown, ", "))
		} else {
			c.fail("unknown %s %s (valid properties: %s)", noun, strings.Join(unknown, ", "), strings.Join(slices.Sorted(maps.Keys(props)), ", "))
		}
	}
	for _, r := range requiredOf(s) {
		if _, ok := obj[r]; !ok {
			c.fail("missing required property %s", strconv.Quote(join(path, r)))
		}
	}
}

func typesOf(s map[string]any) []string {
	switch t := s["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, x := range t {
			if name, ok := x.(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func requiredOf(s map[string]any) []string {
	list, _ := s["required"].([]any)
	var out []string
	for _, r := range list {
		if name, ok := r.(string); ok {
			out = append(out, name)
		}
	}
	return out
}

func dropsNull(s map[string]any) bool {
	types := typesOf(s)
	return len(types) > 0 && !slices.Contains(types, "null")
}

func isType(t string, v any) bool {
	switch t {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := strconv.ParseInt(string(n), 10, 64)
		return err == nil
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "null":
		return v == nil
	}
	return true
}

func coerce(types []string, v any) (any, bool) {
	for _, t := range types {
		switch t {
		case "integer":
			if n, ok := numberOf(v); ok {
				if i, ok := integerOf(n); ok {
					return json.Number(strconv.FormatInt(i, 10)), true
				}
			}
		case "number":
			if n, ok := numberOf(v); ok {
				return n, true
			}
		case "boolean":
			if s, ok := v.(string); ok {
				switch strings.ToLower(strings.TrimSpace(s)) {
				case "true":
					return true, true
				case "false":
					return false, true
				}
			}
		}
	}
	return nil, false
}

func numberOf(v any) (json.Number, bool) {
	switch x := v.(type) {
	case json.Number:
		return x, true
	case string:
		if s := strings.TrimSpace(x); jsonNumberLiteral.MatchString(s) {
			return json.Number(s), true
		}
	}
	return "", false
}

func integerOf(n json.Number) (int64, bool) {
	if i, err := strconv.ParseInt(string(n), 10, 64); err == nil {
		return i, true
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		return 0, false
	}
	return int64(f), true
}

func sameValue(a, b any) bool {
	if x, ok := floatOf(a); ok {
		y, ok := floatOf(b)
		return ok && x == y
	}
	return reflect.DeepEqual(a, b)
}

func floatOf(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

var typeArticles = map[string]string{
	"string":  "a string",
	"integer": "an integer",
	"number":  "a number",
	"boolean": "a boolean",
	"object":  "an object",
	"array":   "an array",
	"null":    "null",
}

func typeNames(types []string) string {
	names := make([]string, len(types))
	for i, t := range types {
		names[i] = t
		if n, ok := typeArticles[t]; ok {
			names[i] = n
		}
	}
	return strings.Join(names, " or ")
}

func literals(values []any) string {
	out := make([]string, len(values))
	for i, v := range values {
		b, err := json.Marshal(v)
		if err != nil {
			out[i] = fmt.Sprint(v)
			continue
		}
		out[i] = string(b)
	}
	return strings.Join(out, ", ")
}

func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		if r := []rune(x); len(r) > 40 {
			x = string(r[:40]) + "..."
		}
		return strconv.Quote(x)
	case json.Number:
		return string(x)
	case bool:
		return strconv.FormatBool(x)
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	}
	return fmt.Sprint(v)
}

func subject(path string) string {
	if path == "" {
		return "arguments"
	}
	return strconv.Quote(path)
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
