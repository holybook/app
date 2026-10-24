package translate

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
)

type fakeFinder map[string]*store.VerbatimResult

func (f fakeFinder) Verbatim(_ context.Context, _, _, text string) (*store.VerbatimResult, error) {
	if v, ok := f[text]; ok {
		return v, nil
	}
	return nil, store.ErrNotFound
}

type fakeTranslator func(ModelRequest) *ModelResponse

func (f fakeTranslator) Translate(_ context.Context, req ModelRequest) (*ModelResponse, error) {
	return f(req), nil
}

func intp(n int) *int { return &n }

var hiddenWords = &store.VerbatimResult{
	TranslatedParagraph: book.Paragraph{Index: 7, Number: intp(3), Text: "O Sohn des Seins! Liebe Mich, damit Ich dich liebe."},
	AllOriginalResults:  []store.SearchResult{{BookID: "hw", Title: "The Hidden Words"}},
}

func TestServiceAnnotatesAuthoritativeParagraphs(t *testing.T) {
	var got ModelRequest
	s := &Service{
		Finder: fakeFinder{"Love Me, that I may love thee.": hiddenWords},
		Translator: fakeTranslator(func(req ModelRequest) *ModelResponse {
			got = req
			return &ModelResponse{Paragraphs: []ParagraphWithID{
				{Text: "Ein Satz."},
				{Text: "Liebe Mich, damit Ich dich liebe.", ID: "hw/7"},
			}}
		}),
	}
	res, err := s.Translate(context.Background(), "en", "de", "  A sentence. \r\n\n Love Me, that I may love thee.\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Paragraphs) != 2 || got.Paragraphs[0].Reference != nil || got.Paragraphs[1].Reference.ID != "hw/7" ||
		got.Paragraphs[1].Reference.Text != hiddenWords.TranslatedParagraph.Text || got.Paragraphs[0].Text != "A sentence." {
		t.Errorf("model request = %+v", got)
	}
	if res.Paragraphs[0].Annotation != nil {
		t.Errorf("unexpected annotation %+v", res.Paragraphs[0])
	}
	a := res.Paragraphs[1].Annotation
	if a == nil || a.BookID != "hw" || a.Index != 7 || *a.Number != 3 || a.Title != "The Hidden Words" {
		t.Errorf("annotation = %+v", a)
	}
}

func TestServiceRejectsAlteredAuthoritativeText(t *testing.T) {
	s := &Service{
		Finder: fakeFinder{"Love Me": hiddenWords},
		Translator: fakeTranslator(func(ModelRequest) *ModelResponse {
			return &ModelResponse{Paragraphs: []ParagraphWithID{{Text: "Liebe mich, so liebe ich dich.", ID: "hw/7"}}}
		}),
	}
	if _, err := s.Translate(context.Background(), "en", "de", "Love Me"); !errors.Is(err, ErrNotVerbatim) {
		t.Errorf("err = %v", err)
	}
}

func TestServiceEmptyText(t *testing.T) {
	s := &Service{Finder: fakeFinder{}, Translator: fakeTranslator(func(ModelRequest) *ModelResponse {
		t.Fatal("translator must not be called")
		return nil
	})}
	res, err := s.Translate(context.Background(), "en", "de", " \n ")
	if err != nil || len(res.Paragraphs) != 0 {
		t.Errorf("res = %+v, %v", res, err)
	}
}

type fakeParagraphTranslator struct{}

func (fakeParagraphTranslator) TranslateParagraph(_ context.Context, _, _, text string) (string, error) {
	return "translated:" + text, nil
}

type fakeMatcher struct{}

func (fakeMatcher) FindBestMatch(_ context.Context, _, _, source, reference string) (string, error) {
	if source == "nomatch" {
		return "", nil
	}
	return "match:" + reference, nil
}

func TestIncremental(t *testing.T) {
	inc := Incremental{Translator: fakeParagraphTranslator{}, Matcher: fakeMatcher{}}
	resp, err := inc.Translate(context.Background(), ModelRequest{Paragraphs: []ParagraphWithReference{
		{Text: "plain"},
		{Text: "quoted", Reference: &ParagraphWithID{Text: "ref", ID: "b/1"}},
		{Text: "nomatch", Reference: &ParagraphWithID{Text: "ref", ID: "b/2"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []ParagraphWithID{{Text: "translated:plain"}, {Text: "match:ref", ID: "b/1"}, {Text: "translated:nomatch", ID: "b/2"}}
	for i, p := range resp.Paragraphs {
		if p != want[i] {
			t.Errorf("paragraph %d = %+v, want %+v", i, p, want[i])
		}
	}
}

// TestMonolithicWireFormat checks the request sent to the Gemini API against a
// fake server: structured JSON output, schema, system prompt, no thinking.
func TestMonolithicWireFormat(t *testing.T) {
	var body map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		data, _ := io.ReadAll(r.Body)
		json.Unmarshal(data, &body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"{\"paragraphs\":[{\"text\":\"Hallo\",\"id\":\"b/1\"}]}"}]}}],
			"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}}`)
	}))
	defer srv.Close()

	g, err := newGemini(context.Background(), &genai.ClientConfig{
		APIKey: "test", Backend: genai.BackendGeminiAPI, HTTPOptions: genai.HTTPOptions{BaseURL: srv.URL},
	}, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := Monolithic{g}.Translate(context.Background(), ModelRequest{
		FromLanguage: "en", ToLanguage: "de",
		Paragraphs: []ParagraphWithReference{{Text: "Hello", Reference: &ParagraphWithID{Text: "Hallo Welt", ID: "b/1"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Paragraphs) != 1 || resp.Paragraphs[0] != (ParagraphWithID{Text: "Hallo", ID: "b/1"}) {
		t.Errorf("response = %+v", resp)
	}
	if !strings.HasSuffix(path, "/models/test-model:generateContent") {
		t.Errorf("path = %s", path)
	}
	cfg, _ := body["generationConfig"].(map[string]any)
	if cfg["responseMimeType"] != "application/json" || cfg["responseSchema"] == nil {
		t.Errorf("generationConfig = %v", cfg)
	}
	if tc, _ := cfg["thinkingConfig"].(map[string]any); tc == nil || tc["thinkingBudget"] != float64(0) {
		t.Errorf("thinkingConfig = %v", cfg["thinkingConfig"])
	}
	sys, _ := json.Marshal(body["systemInstruction"])
	if !strings.Contains(string(sys), "professional translator") {
		t.Errorf("systemInstruction = %s", sys)
	}
	contents, _ := json.Marshal(body["contents"])
	if !strings.Contains(string(contents), `\"fromLanguage\":\"en\"`) || !strings.Contains(string(contents), `\"reference\":{\"text\":\"Hallo Welt\",\"id\":\"b/1\"}`) {
		t.Errorf("contents = %s", contents)
	}
}
