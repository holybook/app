package mcp

import (
	"slices"
	"testing"
)

func TestArguments(t *testing.T) {
	t.Run("strings are trimmed and blank strings count as missing", func(t *testing.T) {
		a := parseArguments([]byte(`{"query": "  unity  ", "book_id": "   "}`))
		if a.string("query") != "unity" || a.optionalString("book_id") != "" || a.Err() != nil {
			t.Errorf("err = %v", a.Err())
		}
		a.string("book_id")
		if a.Err() == nil || a.Err().Error() != "Missing required argument 'book_id'." {
			t.Errorf("err = %v", a.Err())
		}
	})

	t.Run("choices are validated with a helpful message", func(t *testing.T) {
		a := parseArguments([]byte(`{"language": "fr"}`))
		a.choice("language", []string{"de", "en"}, "en")
		if a.Err() == nil || a.Err().Error() != "Invalid value 'fr' for 'language'. Allowed values: de, en." {
			t.Errorf("err = %v", a.Err())
		}
		if got := parseArguments(nil).choice("language", []string{"en"}, "en"); got != "en" {
			t.Errorf("default = %q", got)
		}
	})

	t.Run("integers accept numeric strings and are range checked", func(t *testing.T) {
		if n := parseArguments([]byte(`{"limit": "5"}`)).int("limit", 10, 1, 50); n != 5 {
			t.Errorf("limit = %d", n)
		}
		if n := parseArguments([]byte(`{}`)).int("limit", 10, 1, 50); n != 10 {
			t.Errorf("default = %d", n)
		}
		a := parseArguments([]byte(`{"limit": 99}`))
		a.int("limit", 10, 1, 50)
		if a.Err() == nil {
			t.Error("out of range accepted")
		}
		a = parseArguments([]byte(`{"limit": 2.5}`))
		a.int("limit", 10, 1, 50)
		if a.Err() == nil {
			t.Error("fraction accepted")
		}
	})

	t.Run("a single string is accepted where a list is expected", func(t *testing.T) {
		if l := parseArguments([]byte(`{"paragraphs": "a"}`)).stringList("paragraphs", 20); !slices.Equal(l, []string{"a"}) {
			t.Errorf("list = %q", l)
		}
		a := parseArguments([]byte(`{"paragraphs": []}`))
		a.stringList("paragraphs", 20)
		if a.Err() == nil {
			t.Error("empty list accepted")
		}
	})

	t.Run("the first error is kept", func(t *testing.T) {
		a := parseArguments([]byte(`{"query": ["x"], "limit": "many"}`))
		a.string("query")
		a.int("limit", 10, 1, 50)
		if a.Err() == nil || a.Err().Error() != "Argument 'query' must be a string." {
			t.Errorf("err = %v", a.Err())
		}
	})

	t.Run("arguments must be an object", func(t *testing.T) {
		if parseArguments([]byte(`[1]`)).Err() == nil {
			t.Error("array accepted")
		}
	})
}
