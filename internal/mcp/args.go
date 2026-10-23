package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// argumentError is an invalid tool argument. Its message is returned to the
// agent so it can correct the call.
type argumentError struct{ msg string }

func (e *argumentError) Error() string { return e.msg }

func argErrorf(format string, a ...any) error {
	return &argumentError{fmt.Sprintf(format, a...)}
}

// arguments gives typed, validating access to the arguments of a tool call.
// Like bufio.Scanner it records the first error, so a handler reads all its
// arguments and checks Err once. Agents are not always precise, so it is
// lenient where the intent is clear: blank strings count as missing, numbers
// may be sent as strings and a single string as a list.
type arguments struct {
	values map[string]json.RawMessage
	err    error
}

func parseArguments(raw json.RawMessage) *arguments {
	a := &arguments{values: map[string]json.RawMessage{}}
	if len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		if err := json.Unmarshal(raw, &a.values); err != nil {
			a.err = argErrorf("The arguments must be a JSON object.")
		}
	}
	return a
}

// Err returns the first invalid argument read.
func (a *arguments) Err() error { return a.err }

func (a *arguments) fail(format string, args ...any) {
	if a.err == nil {
		a.err = argErrorf(format, args...)
	}
}

// raw returns the value of an argument, or nil if it is missing or null.
func (a *arguments) raw(name string) json.RawMessage {
	v := a.values[name]
	if bytes.Equal(v, []byte("null")) {
		return nil
	}
	return v
}

// scalar returns a string, number or boolean argument as text.
func scalar(v json.RawMessage) (string, bool) {
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s, true
	}
	if len(v) > 0 && v[0] != '{' && v[0] != '[' {
		return string(v), true
	}
	return "", false
}

func (a *arguments) optionalString(name string) string {
	v := a.raw(name)
	if v == nil {
		return ""
	}
	s, ok := scalar(v)
	if !ok {
		a.fail("Argument '%s' must be a string.", name)
	}
	return strings.TrimSpace(s)
}

func (a *arguments) string(name string) string {
	s := a.optionalString(name)
	if s == "" {
		a.fail("Missing required argument '%s'.", name)
	}
	return s
}

// choice returns an argument that must be one of allowed, or def if it is
// missing. An empty def makes the argument required.
func (a *arguments) choice(name string, allowed []string, def string) string {
	s := a.optionalChoice(name, allowed)
	if s == "" {
		s = def
	}
	if s == "" {
		a.fail("Missing required argument '%s'.", name)
	}
	return s
}

func (a *arguments) optionalChoice(name string, allowed []string) string {
	s := a.optionalString(name)
	if s != "" && !slices.Contains(allowed, s) {
		a.fail("Invalid value '%s' for '%s'. Allowed values: %s.", s, name, strings.Join(allowed, ", "))
		return ""
	}
	return s
}

// int returns an integer argument in [min, max], or def if it is missing.
func (a *arguments) int(name string, def, min, max int) int {
	n, ok := a.optionalInt(name, min, max)
	if !ok {
		return def
	}
	return n
}

func (a *arguments) optionalInt(name string, min, max int) (int, bool) {
	v := a.raw(name)
	if v == nil {
		return 0, false
	}
	s, _ := scalar(v)
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		a.fail("Argument '%s' must be an integer.", name)
		return 0, false
	}
	if n < min || n > max {
		a.fail("Argument '%s' must be between %d and %d, was %d.", name, min, max, n)
		return 0, false
	}
	return n, true
}

// stringList returns a required list of 1 to maxItems strings.
func (a *arguments) stringList(name string, maxItems int) []string {
	v := a.raw(name)
	if v == nil {
		a.fail("Missing required argument '%s'.", name)
		return nil
	}
	var list []string
	if s, ok := scalar(v); ok {
		// Some clients send a single string where a list is expected.
		list = []string{s}
	} else if err := json.Unmarshal(v, &list); err != nil {
		a.fail("Argument '%s' must be an array of strings.", name)
		return nil
	}
	if len(list) == 0 || len(list) > maxItems {
		a.fail("Argument '%s' must contain between 1 and %d items.", name, maxItems)
		return nil
	}
	return list
}

// JSON schemas of tool parameters.

type schema = map[string]any

func objectSchema(properties map[string]schema, required ...string) schema {
	s := schema{"type": "object", "properties": properties}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// stringSchema describes a string parameter; enum and def are optional.
func stringSchema(description string, enum []string, def string) schema {
	s := schema{"type": "string", "description": description}
	if enum != nil {
		s["enum"] = enum
	}
	if def != "" {
		s["default"] = def
	}
	return s
}

// integerSchema describes an integer parameter; max and def are optional
// (nil).
func integerSchema(description string, min int, max, def *int) schema {
	s := schema{"type": "integer", "description": description, "minimum": min}
	if max != nil {
		s["maximum"] = *max
	}
	if def != nil {
		s["default"] = *def
	}
	return s
}

func stringArraySchema(description string, maxItems int) schema {
	return schema{
		"type":        "array",
		"description": description,
		"items":       schema{"type": "string"},
		"minItems":    1,
		"maxItems":    maxItems,
	}
}

func ptr[T any](v T) *T { return &v }
