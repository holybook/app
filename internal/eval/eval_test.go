package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/holybook/app/internal/translate"
)

type echo struct{}

// Translate returns the reference text when there is one, else the source.
func (echo) Translate(_ context.Context, req translate.ModelRequest) (*translate.ModelResponse, error) {
	resp := &translate.ModelResponse{}
	for i, p := range req.Paragraphs {
		text := p.Text
		if p.Reference != nil && i == 0 {
			text = p.Reference.Text
		}
		resp.Paragraphs = append(resp.Paragraphs, translate.ParagraphWithID{Text: text})
	}
	return resp, nil
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "case.json"), []byte(`{
		"fromLanguage": "en", "toLanguage": "de",
		"paragraphs": [{"text": "a", "id": "r1"}, {"text": "b", "id": "r2"}, {"text": "c"}],
		"referenceParagraphs": [{"text": "A!", "id": "r1"}, {"text": "B!", "id": "r2"}],
		"extra": true
	}`), 0o644)

	files, err := Files(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	s, err := Run(context.Background(), echo{}, files, 2, &out)
	if err != nil {
		t.Fatal(err)
	}
	if s != (Score{Verbatim: 2, Scored: 4}) {
		t.Errorf("score = %+v", s)
	}
	if !strings.Contains(out.String(), "Verbatim: 2 / 4  (50.0%)") {
		t.Errorf("output = %q", out.String())
	}
	if _, err := Files(filepath.Join(dir, "missing")); err == nil {
		t.Error("expected error for missing input")
	}
}
