package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
	"github.com/holybook/app/internal/translate"
)

type fakeStore struct{}

func intp(n int) *int { return &n }

var (
	hiddenWordsEN = store.Translation{BookID: "hw", Language: "en", Title: "The Hidden Words", Author: "Bahá’u’lláh", AuthorCode: "bahaullah"}
	hiddenWordsDE = store.Translation{BookID: "hw", Language: "de", Title: "Die Verborgenen Worte", Author: "Bahá’u’lláh", AuthorCode: "bahaullah"}
)

func uhjMessages() []store.Translation {
	var out []store.Translation
	for i := range 14 {
		d := time.Date(2026-i/5, 3, 20, 0, 0, 0, 0, time.UTC)
		out = append(out, store.Translation{BookID: "uhj" + d.Format("20060102"), Language: "en",
			Title: "Message " + d.Format("2006"), Author: "The Universal House of Justice", AuthorCode: "uhj", PublishedAt: d})
	}
	return out
}

func (fakeStore) Ping(context.Context) error { return nil }
func (fakeStore) Languages(context.Context) ([]string, error) {
	return []string{"de", "en"}, nil
}
func (fakeStore) Authors(context.Context) ([]store.Author, error) {
	return store.SeedAuthors, nil
}
func (fakeStore) Library(_ context.Context, lang string) ([]store.Translation, error) {
	if lang == "de" {
		return []store.Translation{hiddenWordsDE}, nil
	}
	return append([]store.Translation{hiddenWordsEN}, uhjMessages()...), nil
}
func (fakeStore) Book(_ context.Context, id string) (*store.Book, error) {
	if id != "hw" {
		return nil, store.ErrNotFound
	}
	return &store.Book{ID: "hw", ParagraphCount: 3, Translations: []store.Translation{hiddenWordsDE, hiddenWordsEN}}, nil
}
func (fakeStore) Paragraphs(_ context.Context, id, lang string, start, end *int) ([]book.Paragraph, error) {
	text := "O Son of Spirit! <Possess> a pure heart."
	if lang == "de" {
		text = "O Sohn des Geistes! Besitze ein reines Herz."
	}
	return []book.Paragraph{
		{Index: 0, Text: "Part One", Type: book.SectionTitle, SectionPath: "1"},
		{Index: 1, Text: text, Type: book.Body, Number: intp(1), SectionPath: "1"},
		{Index: 2, Text: "* * *", Type: book.Separator, SectionPath: "1"},
	}, nil
}
func (fakeStore) Search(_ context.Context, lang, q string, limit, offset int) (store.SearchPage, error) {
	var page store.SearchPage
	page.Total = 25
	if limit <= 0 {
		limit = page.Total
	}
	for i := offset; i < min(offset+limit, page.Total); i++ {
		page.Results = append(page.Results, store.SearchResult{
			BookID: "hw", Author: "Bahá’u’lláh", Title: "The Hidden Words",
			HighlightedText: "a <b>pure</b> heart &amp; mind",
			Paragraph:       book.Paragraph{Index: i, Number: intp(i + 1), Type: book.Body},
		})
	}
	return page, nil
}
func (fakeStore) Verbatim(_ context.Context, from, to, text string) (*store.VerbatimResult, error) {
	if !strings.Contains("O Son of Spirit! Possess a pure heart.", text) || to != "de" {
		return nil, store.ErrNotFound
	}
	return &store.VerbatimResult{
		TranslatedParagraph: book.Paragraph{Index: 1, Number: intp(1), SectionPath: "1", Text: "O Sohn des Geistes! Besitze ein reines Herz."},
		AllOriginalResults:  []store.SearchResult{{BookID: "hw", Title: "The Hidden Words", Author: "Bahá’u’lláh"}},
	}, nil
}

type fakeTranslator struct{ err error }

func (f fakeTranslator) Translate(_ context.Context, from, to, text string) (*translate.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &translate.Result{Paragraphs: []translate.AnnotatedParagraph{
		{Text: "[" + to + "] " + text},
		{Text: "Besitze ein reines Herz.", Annotation: &translate.Annotation{BookID: "hw", Index: 1, Number: intp(1), Title: "The Hidden Words"}},
	}}, nil
}

func newServer(t *testing.T, tr Translator) *Server {
	t.Helper()
	s, err := New(Options{Store: fakeStore{}, Translator: tr})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type response struct {
	status int
	header http.Header
	body   string
}

func do(t *testing.T, s http.Handler, method, target string, form url.Values, headers ...string) response {
	t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return response{rec.Code, rec.Header(), rec.Body.String()}
}

func assertContains(t *testing.T, r response, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(r.body, w) {
			t.Errorf("response (status %d) does not contain %q", r.status, w)
		}
	}
}

func assertNotContains(t *testing.T, r response, unwanted ...string) {
	t.Helper()
	for _, w := range unwanted {
		if strings.Contains(r.body, w) {
			t.Errorf("response unexpectedly contains %q", w)
		}
	}
}

var htmx = []string{"HX-Request", "true"}

func TestLibrary(t *testing.T) {
	s := newServer(t, nil)

	r := do(t, s, "GET", "/", nil)
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
		t.Fatalf("status %d", r.status)
	}
	assertContains(t, r, "<!doctype html>", "15 works by 2 authors", "The Hidden Words",
		`href="/books/hw?lang=en"`, "The Universal House of Justice",
		`<span>2026</span>`, "20 March 2026", `hreflang="de"`, `href="/?lang=de"`)
	// The newest year is open, older ones collapsed.
	if !strings.Contains(r.body, `<details class="year" open>`) || !strings.Contains(r.body, `<details class="year">`) {
		t.Error("expected only the newest year expanded")
	}

	// htmx filter requests get only the results fragment.
	r = do(t, s, "GET", "/?filter=hidden&lang=en", nil, htmx...)
	assertContains(t, r, "1 work matches “hidden”", "The Hidden Words")
	assertNotContains(t, r, "<!doctype html>", "Universal House")

	// Accent- and apostrophe-insensitive author match keeps all their works.
	r = do(t, s, "GET", "/?filter=bahaullah", nil)
	assertContains(t, r, "The Hidden Words", "1 work matches")

	// History restoration needs the full page.
	r = do(t, s, "GET", "/?filter=hidden", nil, "HX-Request", "true", "HX-History-Restore-Request", "true")
	assertContains(t, r, "<!doctype html>")

	r = do(t, s, "GET", "/?filter=zzz", nil, htmx...)
	assertContains(t, r, "No works match “zzz”.")

	r = do(t, s, "GET", "/?lang=de", nil)
	assertContains(t, r, `<div id="library" class="library" lang="de"`, "Die Verborgenen Worte", `href="/books/hw?lang=de"`)
}

func TestReader(t *testing.T) {
	s := newServer(t, nil)
	r := do(t, s, "GET", "/books/hw?lang=de", nil)
	if r.status != 200 {
		t.Fatalf("status %d", r.status)
	}
	assertContains(t, r, "<title>Die Verborgenen Worte · Holybook</title>", `<article class="book" lang="de">`,
		`id="p-0" class="section-title section-title--l1"`, `<a href="#p-0" data-toc-link="0">`,
		`<p id="p-1" class="par par--body"><a class="par__num" href="#p-1" aria-label="Paragraph 1:1">1:1</a>O Sohn des Geistes!`,
		`class="par par--separator"`, `href="/books/hw?lang=en"`)

	// Unknown languages fall back to English; text is escaped.
	r = do(t, s, "GET", "/books/hw?lang=fr", nil)
	assertContains(t, r, `<article class="book" lang="en">`, "&lt;Possess&gt;")

	r = do(t, s, "GET", "/books/nope", nil)
	if r.status != 404 {
		t.Errorf("missing book: status %d", r.status)
	}
	assertContains(t, r, "Page not found")
}

func TestSearch(t *testing.T) {
	s := newServer(t, nil)

	r := do(t, s, "GET", "/search", nil)
	assertContains(t, r, "Search the writings", "excludes a word")

	r = do(t, s, "GET", "/search?q=pure&lang=en", nil)
	assertContains(t, r, "25 results", "<q>pure</q>", "a <b>pure</b> heart &amp; mind",
		`href="/books/hw?lang=en#p-0"`, "¶ 1", `hx-get="/search?lang=en&amp;offset=20&amp;q=pure"`,
		`value="pure"`)

	// The next page is a fragment of list items, without a further page.
	r = do(t, s, "GET", "/search?q=pure&lang=en&offset=20", nil, htmx...)
	assertNotContains(t, r, "<!doctype html>", "offset=40")
	if n := strings.Count(r.body, `class="result"`); n != 5 {
		t.Errorf("got %d results on the last page", n)
	}
}

func TestVerbatim(t *testing.T) {
	s := newServer(t, nil)

	r := do(t, s, "GET", "/translate", nil)
	assertContains(t, r, "Verbatim translation", `<option value="en" selected>English</option>`,
		`<option value="de" selected>Deutsch</option>`, "will appear here")

	form := url.Values{"from": {"en"}, "to": {"de"}, "text": {"  Possess a pure heart  "}}
	r = do(t, s, "POST", "/translate", form, htmx...)
	assertNotContains(t, r, "<!doctype html>")
	assertContains(t, r, `<blockquote class="quote" lang="de">O Sohn des Geistes! Besitze ein reines Herz.</blockquote>`,
		`href="/books/hw?lang=de#p-1"`, "Bahá’u’lláh, Die Verborgenen Worte, par. 1:1")

	// Without htmx the full page is rendered with the form state kept.
	r = do(t, s, "POST", "/translate", form)
	assertContains(t, r, "<!doctype html>", "Besitze ein reines Herz", ">  Possess a pure heart  </textarea>")

	form.Set("to", "en")
	r = do(t, s, "POST", "/translate", form, htmx...)
	assertContains(t, r, "Could not find this passage in English.")

	// Unknown languages are ignored in favor of the defaults.
	form = url.Values{"from": {"xx"}, "to": {"de"}, "text": {"pure heart"}}
	r = do(t, s, "POST", "/translate", form, htmx...)
	assertContains(t, r, "Besitze ein reines Herz")

	r = do(t, s, "POST", "/translate", url.Values{"text": {strings.Repeat("a", 70<<10)}})
	if r.status != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized form: status %d", r.status)
	}
}

func TestAITranslate(t *testing.T) {
	r := do(t, newServer(t, nil), "GET", "/aitranslate", nil)
	assertContains(t, r, "AI translation is not available", "disabled")

	s := newServer(t, fakeTranslator{})
	r = do(t, s, "GET", "/aitranslate", nil)
	assertNotContains(t, r, "not available")

	form := url.Values{"from": {"en"}, "to": {"de"}, "text": {"Hello"}}
	r = do(t, s, "POST", "/aitranslate", form, htmx...)
	assertNotContains(t, r, "<!doctype html>")
	assertContains(t, r, `<div class="ai-paragraphs" lang="de">`, "[de] Hello",
		`href="/books/hw?lang=de#p-1"`, "Die Verborgenen Worte, par. 1")

	s = newServer(t, fakeTranslator{err: translate.ErrNotVerbatim})
	r = do(t, s, "POST", "/aitranslate", form, htmx...)
	assertContains(t, r, "did not reproduce an authorized translation")
}

func TestAPI(t *testing.T) {
	s := newServer(t, fakeTranslator{})
	decode := func(r response, v any) {
		t.Helper()
		if !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
			t.Errorf("content type %q", r.header.Get("Content-Type"))
		}
		if err := json.Unmarshal([]byte(r.body), v); err != nil {
			t.Fatalf("decoding %q: %v", r.body, err)
		}
	}

	var langs []string
	decode(do(t, s, "GET", "/api/languages", nil), &langs)
	if strings.Join(langs, ",") != "de,en" {
		t.Errorf("languages = %v", langs)
	}

	var authors []store.Author
	decode(do(t, s, "GET", "/api/authors", nil), &authors)
	if len(authors) != 6 || authors[1].Names["en"] != "Bahá’u’lláh" {
		t.Errorf("authors = %v", authors)
	}

	var books map[string][]map[string]any
	decode(do(t, s, "GET", "/api/books?lang=en", nil), &books)
	if b := books["Bahá’u’lláh"]; len(b) != 1 || b[0]["bookId"] != "hw" || b[0]["title"] != "The Hidden Words" {
		t.Errorf("books = %v", books)
	}

	var b map[string]any
	decode(do(t, s, "GET", "/api/books/hw", nil), &b)
	if b["id"] != "hw" || b["paragraphCount"] != float64(3) || len(b["translations"].([]any)) != 2 {
		t.Errorf("book = %v", b)
	}
	if r := do(t, s, "GET", "/api/books/nope", nil); r.status != 404 {
		t.Errorf("missing book: %d", r.status)
	}

	var ps []map[string]any
	decode(do(t, s, "GET", "/api/books/hw/paragraphs?lang=en&start=0&end=2", nil), &ps)
	if len(ps) != 3 || ps[1]["type"] != "body" || ps[1]["number"] != float64(1) || ps[1]["sectionPath"] != "1" || ps[0]["number"] != nil {
		t.Errorf("paragraphs = %v", ps)
	}
	if r := do(t, s, "GET", "/api/books/hw/paragraphs?start=x", nil); r.status != 400 {
		t.Errorf("bad start: %d", r.status)
	}

	var results []map[string]any
	decode(do(t, s, "GET", "/api/search?q=pure&lang=en", nil), &results)
	if len(results) != 25 || results[0]["highlightedText"] != "a <b>pure</b> heart &amp; mind" {
		t.Errorf("search = %d results", len(results))
	}
	if r := do(t, s, "GET", "/api/search", nil); r.status != 400 {
		t.Errorf("missing q: %d", r.status)
	}

	post := func(path, body string) response {
		req := httptest.NewRequest("POST", path, strings.NewReader(body))
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		return response{rec.Code, rec.Header(), rec.Body.String()}
	}
	var v map[string]any
	decode(post("/api/translate", `{"fromLanguage":"en","toLanguage":"de","text":"pure heart"}`), &v)
	if v["translatedParagraph"].(map[string]any)["index"] != float64(1) || len(v["allOriginalResults"].([]any)) != 1 {
		t.Errorf("translate = %v", v)
	}
	if r := post("/api/translate", `{"fromLanguage":"en","toLanguage":"en","text":"x"}`); r.status != 404 {
		t.Errorf("not found: %d", r.status)
	}
	if r := post("/api/translate", `nope`); r.status != 400 {
		t.Errorf("bad body: %d", r.status)
	}

	var ai map[string][]map[string]any
	decode(post("/api/aitranslate", `{"fromLanguage":"en","toLanguage":"de","text":"Hello"}`), &ai)
	if p := ai["paragraphs"]; len(p) != 2 || p[0]["annotation"] != nil || p[1]["annotation"].(map[string]any)["bookId"] != "hw" {
		t.Errorf("aitranslate = %v", ai)
	}
	if r := do(t, newServer(t, nil), "POST", "/api/aitranslate", nil); r.status != 503 {
		t.Errorf("unconfigured AI: %d", r.status)
	}

	if r := do(t, s, "GET", "/api/nope", nil); r.status != 404 || !strings.Contains(r.body, `"error"`) {
		t.Errorf("unknown API route: %d %s", r.status, r.body)
	}
}

func TestStaticAndMisc(t *testing.T) {
	s := newServer(t, nil)

	r := do(t, s, "GET", "/", nil)
	i := strings.Index(r.body, "/static/app.css?v=")
	if i < 0 {
		t.Fatal("no fingerprinted stylesheet link")
	}
	cssURL := r.body[i : i+strings.Index(r.body[i:], `"`)]
	r = do(t, s, "GET", cssURL, nil)
	if r.status != 200 || !strings.Contains(r.header.Get("Cache-Control"), "immutable") || !strings.HasPrefix(r.header.Get("Content-Type"), "text/css") {
		t.Errorf("css: %d %v", r.status, r.header)
	}
	if r := do(t, s, "GET", "/static/../server.go", nil); r.status == 200 {
		t.Errorf("path traversal: %d", r.status)
	}
	for _, p := range []string{"/favicon.ico", "/robots.txt", "/manifest.json", "/static/htmx.min.js", "/healthz"} {
		if r := do(t, s, "GET", p, nil); r.status != 200 {
			t.Errorf("%s: %d", p, r.status)
		}
	}
	r = do(t, s, "GET", "/nope", nil)
	if r.status != 404 || r.header.Get("Content-Security-Policy") == "" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("404 page: %d %v", r.status, r.header)
	}
}

func TestFold(t *testing.T) {
	for in, want := range map[string]string{
		"Bahá’u’lláh":   "bahaullah",
		"‘Abdu’l-Bahá":  "abdul-baha",
		"Kitáb-i-Íqán":  "kitab-i-iqan",
		"Riḍván":        "ridvan",
		"Zusammenstell": "zusammenstell",
	} {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}
