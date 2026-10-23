package plugins

import (
	"strings"
	"testing"
)

func TestSkillBody(t *testing.T) {
	for _, name := range []string{"bahai-writings-research", "bahai-quote-translation"} {
		body, err := SkillBody(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(body, "# ") || strings.Contains(body, "description:") {
			t.Errorf("%s: body should start with its heading, without front matter:\n%.200s", name, body)
		}
	}
}
