package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
)

// fakeStore holds books as paragraph texts per language.
type fakeStore struct {
	books map[string]fakeBook
}

type fakeBook struct {
	author string
	titles map[string]string
	texts  map[string][]string
	// salutations are the indexes of salutation paragraphs per language; all
	// others are numbered body paragraphs.
	salutations map[string]int
}

func (f *fakeStore) translation(id, language string) store.Translation {
	b := f.books[id]
	return store.Translation{BookID: id, Language: language, Title: b.titles[language], Author: b.author, AuthorCode: b.author}
}

func (f *fakeStore) paragraph(id, language string, index int) store.ParagraphInBook {
	b := f.books[id]
	p := book.Paragraph{Index: index, Type: book.Body, Text: b.texts[language][index]}
	if s, ok := b.salutations[language]; ok && s == index {
		p.Type = book.Salutation
	} else {
		n := index + 1
		if ok && s < index {
			n--
		}
		p.Number = &n
	}
	return store.ParagraphInBook{Book: f.translation(id, language), Paragraph: p}
}

func (f *fakeStore) Languages(context.Context) ([]string, error) { return []string{"de", "en"}, nil }

func (f *fakeStore) Authors(context.Context) ([]store.Author, error) {
	return []store.Author{{ID: "bahaullah", Names: map[string]string{"en": "Bahá’u’lláh"}}, {ID: "uhj"}}, nil
}

func (f *fakeStore) Library(_ context.Context, language string) ([]store.Translation, error) {
	var ts []store.Translation
	for _, id := range bookIDs {
		if _, ok := f.books[id].titles[language]; ok {
			ts = append(ts, f.translation(id, language))
		}
	}
	return ts, nil
}

func (f *fakeStore) Book(_ context.Context, id string) (*store.Book, error) {
	b, ok := f.books[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	res := &store.Book{ID: id}
	for _, lang := range []string{"de", "en"} {
		if _, ok := b.titles[lang]; ok {
			res.Translations = append(res.Translations, f.translation(id, lang))
		}
	}
	return res, nil
}

func (f *fakeStore) Paragraphs(_ context.Context, id, language string, start, end *int) ([]book.Paragraph, error) {
	var ps []book.Paragraph
	for i := range f.books[id].texts[language] {
		if i >= *start && i <= *end {
			ps = append(ps, f.paragraph(id, language, i).Paragraph)
		}
	}
	return ps, nil
}

// FindParagraphs matches paragraphs containing the query.
func (f *fakeStore) FindParagraphs(_ context.Context, q store.ParagraphQuery) (store.ParagraphPage, error) {
	page := store.ParagraphPage{Paragraphs: []store.ParagraphInBook{}}
	for _, id := range bookIDs {
		for i, text := range f.books[id].texts[q.Language] {
			if strings.Contains(strings.ToLower(text), strings.ToLower(q.Query)) && (q.Author == "" || q.Author == f.books[id].author) {
				page.Paragraphs = append(page.Paragraphs, f.paragraph(id, q.Language, i))
			}
		}
	}
	page.Total = len(page.Paragraphs)
	page.Paragraphs = page.Paragraphs[min(q.Offset, page.Total):min(q.Offset+q.Limit, page.Total)]
	return page, nil
}

// QuoteCandidates returns every paragraph of the language.
func (f *fakeStore) QuoteCandidates(_ context.Context, language string, _ []string, _ int) ([]store.ParagraphInBook, error) {
	var ps []store.ParagraphInBook
	for _, id := range bookIDs {
		for i := range f.books[id].texts[language] {
			ps = append(ps, f.paragraph(id, language, i))
		}
	}
	return ps, nil
}

func (f *fakeStore) ParagraphCounts(_ context.Context, ids []string) (map[string]map[string]int, error) {
	counts := map[string]map[string]int{}
	for _, id := range ids {
		counts[id] = map[string]int{}
		for lang, texts := range f.books[id].texts {
			counts[id][lang] = len(texts)
		}
	}
	return counts, nil
}

func (f *fakeStore) ParagraphAt(_ context.Context, id, language string, index int) (store.ParagraphInBook, error) {
	if index >= len(f.books[id].texts[language]) {
		return store.ParagraphInBook{}, store.ErrNotFound
	}
	return f.paragraph(id, language, index), nil
}

var bookIDs = []string{"hidden-words", "letter", "message"}

var testStore = &fakeStore{books: map[string]fakeBook{
	"hidden-words": {
		author: "bahaullah",
		titles: map[string]string{"en": "The Hidden Words", "de": "Die Verborgenen Worte"},
		texts: map[string][]string{
			"en": {"O Son of Spirit! My first counsel is this: Possess a pure, kindly and radiant heart.",
				"O Son of Spirit! The best beloved of all things in My sight is Justice."},
			"de": {"O Sohn des Geistes! Mein erster Rat ist dieser: Besitze ein reines, gütiges und strahlendes Herz.",
				"O Sohn des Geistes! Das Liebste von allem ist Mir die Gerechtigkeit."},
		},
	},
	// The German version has an extra paragraph, so the versions do not
	// correspond.
	"letter": {
		author: "uhj",
		titles: map[string]string{"en": "A Letter", "de": "Ein Brief"},
		texts: map[string][]string{
			"en": {"Dearly loved Friends,", "Justice is the foundation of unity among all peoples of the earth."},
			"de": {"Liebe Freunde,", "Einleitung", "Gerechtigkeit ist die Grundlage der Einheit."},
		},
	},
	// The versions have as many paragraphs, but the German one has no
	// salutation and an extra paragraph at the end.
	"message": {
		author: "uhj",
		titles: map[string]string{"en": "A Message", "de": "Eine Botschaft"},
		texts: map[string][]string{
			"en": {"Dear Friends,", "The Plan has been won through the sacrifices of the friends everywhere."},
			"de": {"Der Plan ist gewonnen.", "Ein weiterer Absatz."},
		},
		salutations: map[string]int{"en": 0},
	},
}}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(NewHandler(testStore, "https://holybook.test/"))
	t.Cleanup(srv.Close)
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil)
	cs, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

// call calls a tool and decodes its JSON result into v (or returns the error
// text of a tool error).
func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any, v any) string {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if res.IsError {
		return text
	}
	if err := json.Unmarshal([]byte(text), v); err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return ""
}

func TestServer(t *testing.T) {
	cs := connect(t)
	ctx := context.Background()

	if !strings.Contains(cs.InitializeResult().Instructions, "find_authoritative_translation") {
		t.Errorf("instructions = %q", cs.InitializeResult().Instructions)
	}

	t.Run("tools", func(t *testing.T) {
		res, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range res.Tools {
			names = append(names, tool.Name)
			if !tool.Annotations.ReadOnlyHint {
				t.Errorf("%s is not read-only", tool.Name)
			}
		}
		if strings.Join(names, ",") != "find_authoritative_translation,get_passage,list_books,search_writings" {
			t.Errorf("tools = %v", names)
		}
		schema, _ := json.Marshal(res.Tools[3].InputSchema)
		if !strings.Contains(string(schema), `"enum":["bahaullah","uhj"]`) || !strings.Contains(string(schema), "bahaullah (Bahá’u’lláh), uhj (uhj)") {
			t.Errorf("search schema = %s", schema)
		}
	})

	t.Run("search", func(t *testing.T) {
		var res searchResponse
		if msg := call(t, cs, "search_writings", map[string]any{"query": "justice", "limit": 1}, &res); msg != "" {
			t.Fatal(msg)
		}
		if res.Total != 2 || len(res.Results) != 1 || *res.NextOffset != 1 {
			t.Fatalf("res = %+v", res)
		}
		hit := res.Results[0]
		if hit.Title != "The Hidden Words" || hit.Number != "2" || hit.Type != "" ||
			hit.URL != "https://holybook.test/books/hidden-words?lang=en#p-1" {
			t.Errorf("hit = %+v", hit)
		}
		if msg := call(t, cs, "search_writings", map[string]any{"query": "nothing"}, &res); msg != "" || !strings.Contains(res.Hint, "available: de, en") {
			t.Errorf("hint = %q, %q", res.Hint, msg)
		}
	})

	t.Run("invalid arguments are reported to the agent", func(t *testing.T) {
		msg := call(t, cs, "search_writings", map[string]any{"query": "x", "language": "fr"}, nil)
		if msg != "Invalid value 'fr' for 'language'. Allowed values: de, en." {
			t.Errorf("msg = %q", msg)
		}
		if msg := call(t, cs, "get_passage", map[string]any{"book_id": "letter", "start_index": 0, "end_index": 100}, nil); !strings.Contains(msg, "At most 40") {
			t.Errorf("msg = %q", msg)
		}
		if msg := call(t, cs, "get_passage", map[string]any{"book_id": "nope"}, nil); msg != "Unknown book 'nope'." {
			t.Errorf("msg = %q", msg)
		}
	})

	t.Run("passage", func(t *testing.T) {
		var res passageResponse
		if msg := call(t, cs, "get_passage", map[string]any{"book_id": "letter", "language": "de", "start_index": 1, "end_index": 1}, &res); msg != "" {
			t.Fatal(msg)
		}
		if res.Book.Title != "Ein Brief" || res.ParagraphCount != 3 || len(res.Paragraphs) != 1 ||
			res.Paragraphs[0].Text != "Einleitung" || *res.NextIndex != 2 || strings.Join(res.AvailableLanguages, ",") != "de,en" {
			t.Errorf("res = %+v", res)
		}
	})

	t.Run("books", func(t *testing.T) {
		var res bookListResponse
		if msg := call(t, cs, "list_books", map[string]any{"title_contains": "hidden"}, &res); msg != "" {
			t.Fatal(msg)
		}
		if res.Total != 1 || res.Books[0].BookID != "hidden-words" || res.Books[0].AuthorID != "bahaullah" {
			t.Errorf("res = %+v", res)
		}
		if msg := call(t, cs, "list_books", map[string]any{"author": "uhj", "offset": 5}, &res); msg != "" || res.Total != 2 || len(res.Books) != 0 {
			t.Errorf("res = %+v, %q", res, msg)
		}
	})

	t.Run("authoritative translation", func(t *testing.T) {
		var res translationLookupResponse
		msg := call(t, cs, "find_authoritative_translation", map[string]any{
			"paragraphs": []string{
				"“The best beloved of all things in My sight is Justice”",
				"Justice is the foundation of unity among all peoples",
				"Something nobody ever wrote down anywhere at all",
				"The Plan has been won through the sacrifices of the friends",
			},
			"from_language": "en",
			"to_language":   "de",
		}, &res)
		if msg != "" {
			t.Fatal(msg)
		}
		if len(res.Results) != 4 {
			t.Fatalf("res = %+v", res)
		}

		found := res.Results[0]
		if found.Status != "authoritative" || found.Matches[0].Match != "exact" {
			t.Fatalf("found = %+v", found)
		}
		tr := found.Matches[0].Translation
		if !tr.Available || tr.Title != "Die Verborgenen Worte" || tr.Paragraph.Text != "O Sohn des Geistes! Das Liebste von allem ist Mir die Gerechtigkeit." {
			t.Errorf("translation = %+v", tr)
		}

		mismatch := res.Results[1]
		if mismatch.Input != 2 || mismatch.Status != "authoritative" || mismatch.Matches[0].Translation.Reason != "paragraph_count_mismatch" {
			t.Errorf("mismatch = %+v", mismatch)
		}

		if nf := res.Results[2]; nf.Status != "not_found" || nf.Matches == nil {
			t.Errorf("not found = %+v", nf)
		}

		if shifted := res.Results[3].Matches[0]; shifted.Source.BookID != "message" || shifted.Translation.Reason != "paragraph_count_mismatch" {
			t.Errorf("shifted = %+v", shifted)
		}

		if msg := call(t, cs, "find_authoritative_translation", map[string]any{"paragraphs": "x", "from_language": "en", "to_language": "en"}, nil); msg != "from_language and to_language must differ." {
			t.Errorf("msg = %q", msg)
		}
	})

	t.Run("prompts", func(t *testing.T) {
		res, err := cs.GetPrompt(ctx, &mcp.GetPromptParams{
			Name:      "translate_with_authoritative_quotes",
			Arguments: map[string]string{"text": "Hello", "to_language": "de"},
		})
		if err != nil {
			t.Fatal(err)
		}
		text := res.Messages[0].Content.(*mcp.TextContent).Text
		if !strings.HasPrefix(text, "# Translating texts") || !strings.HasSuffix(text, "into 'de':\n\nHello") {
			t.Errorf("prompt = %.100s … %s", text, text[len(text)-50:])
		}
	})
}
