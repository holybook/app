package store

import (
	"context"
	"iter"
	"os"
	"strings"
	"testing"

	"github.com/holybook/app/internal/book"
)

// testStore connects to HOLYBOOK_TEST_DATABASE_URL and imports a small fixture
// corpus. The database is wiped, so never point this at real data.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("HOLYBOOK_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("HOLYBOOK_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, Config{URL: url})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	stats, err := s.Import(ctx, fixtures(t), "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if stats != (ImportStats{Books: 3, Translations: 4, Paragraphs: 14}) {
		t.Errorf("stats = %+v", stats)
	}
	return s
}

var fixtureXML = []string{
	`<book id="bahaullah_hidden-words" language="en" title="The Hidden Words" author="bahaullah">
  <section title="Part One">
    <p>O Son of Spirit! My first counsel is this: Possess a pure, kindly and radiant heart.</p>
    <p>O Son of Spirit! The best beloved of all things in My sight is Justice.</p>
  </section>
  <section title="Part Two">
    <p>O Son of Being! Love Me, that I may love thee.</p>
  </section>
</book>`,
	`<book id="bahaullah_hidden-words" language="de" title="Die Verborgenen Worte" author="bahaullah">
  <section title="Teil Eins">
    <p>O Sohn des Geistes! Mein erster Rat ist dieser: Besitze ein reines, gütiges und strahlendes Herz.</p>
    <p>O Sohn des Geistes! Das Liebste von allem ist Mir die Gerechtigkeit.</p>
  </section>
  <section title="Teil Zwei">
    <p>O Sohn des Seins! Liebe Mich, damit Ich dich liebe.</p>
  </section>
</book>`,
	`<book id="uhj20200320_001" language="en" title="Naw-Rúz 2020" author="uhj" publishedAt="2020-03-20">
  <p type="letter-head">The Universal House of Justice</p>
  <p type="salutation">Dearly loved Friends,</p>
  <p>Justice &amp; unity are the "foundation".</p>
</book>`,
	// English only, and sorts before the Hidden Words.
	`<book id="abdulbaha_talks" language="en" title="Talks" author="abdulbaha">
  <p>He said: "The best beloved of all things in My sight is Justice."</p>
</book>`,
}

func fixtures(t *testing.T) iter.Seq2[*book.Content, error] {
	return func(yield func(*book.Content, error) bool) {
		for _, x := range fixtureXML {
			c, err := book.ReadXML(strings.NewReader(x))
			if !yield(c, err) {
				return
			}
		}
	}
}

func TestStore(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	t.Run("applied commit", func(t *testing.T) {
		if c, err := s.AppliedCommit(ctx); err != nil || c != "abc123" {
			t.Errorf("commit = %q, %v", c, err)
		}
		if ok, err := s.SchemaUpToDate(ctx); err != nil || !ok {
			t.Errorf("schema up to date = %v, %v", ok, err)
		}
	})

	t.Run("languages", func(t *testing.T) {
		langs, err := s.Languages(ctx)
		if err != nil || strings.Join(langs, ",") != "de,en" {
			t.Errorf("languages = %v, %v", langs, err)
		}
	})

	t.Run("authors", func(t *testing.T) {
		authors, err := s.Authors(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(authors) != len(SeedAuthors) || authors[0].ID != "bab" || authors[4].Names["de"] != "Das Universale Haus der Gerechtigkeit" {
			t.Errorf("authors = %+v", authors)
		}
	})

	t.Run("library", func(t *testing.T) {
		books, err := s.Library(ctx, "en")
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 3 || books[0].Author != "Bahá’u’lláh" || books[1].AuthorCode != "abdulbaha" || books[2].PublishedAt.Year() != 2020 {
			t.Errorf("library = %+v", books)
		}
		de, _ := s.Library(ctx, "de")
		if len(de) != 1 || de[0].Title != "Die Verborgenen Worte" {
			t.Errorf("library(de) = %+v", de)
		}
	})

	t.Run("book", func(t *testing.T) {
		b, err := s.Book(ctx, "bahaullah_hidden-words")
		if err != nil {
			t.Fatal(err)
		}
		if b.ParagraphCount != 5 || strings.Join(b.Languages(), ",") != "de,en" {
			t.Errorf("book = %+v", b)
		}
		if _, err := s.Book(ctx, "nope"); err != ErrNotFound {
			t.Errorf("missing book err = %v", err)
		}
	})

	t.Run("paragraphs", func(t *testing.T) {
		ps, err := s.Paragraphs(ctx, "bahaullah_hidden-words", "en", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 5 || ps[0].Type != book.SectionTitle || ps[4].Label() != "2:1" {
			t.Errorf("paragraphs = %+v", ps)
		}
		one, two := 1, 2
		ps, _ = s.Paragraphs(ctx, "bahaullah_hidden-words", "en", &one, &two)
		if len(ps) != 2 || ps[0].Index != 1 {
			t.Errorf("range = %+v", ps)
		}
	})

	t.Run("search", func(t *testing.T) {
		page, err := s.Search(ctx, "en", "justice", 10, 0)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || len(page.Results) != 4 {
			t.Fatalf("page = %+v", page)
		}
		for _, r := range page.Results {
			if !strings.Contains(r.HighlightedText, "<b>Justice</b>") {
				t.Errorf("highlight = %q", r.HighlightedText)
			}
		}
		// Source text is escaped; only the highlight tags are markup.
		for _, r := range page.Results {
			if r.BookID == "uhj20200320_001" && r.Paragraph.Index == 2 && !strings.Contains(r.HighlightedText, "&amp; unity are the &#34;foundation&#34;") {
				t.Errorf("not escaped: %q", r.HighlightedText)
			}
		}
		// Stemming via the language's configuration.
		if page, _ := s.Search(ctx, "de", "Geist", 0, 0); page.Total != 2 {
			t.Errorf("german stemming: %+v", page)
		}
		if page, _ := s.Search(ctx, "en", "justice", 1, 1); len(page.Results) != 1 || page.Total != 4 {
			t.Errorf("paging: %+v", page)
		}
	})

	t.Run("verbatim", func(t *testing.T) {
		r, err := s.Verbatim(ctx, "en", "de", "best beloved of all things")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(r.TranslatedParagraph.Text, "O Sohn des Geistes! Das Liebste") || r.AllOriginalResults[0].Title != "The Hidden Words" {
			t.Errorf("verbatim = %+v", r)
		}
		if _, err := s.Verbatim(ctx, "en", "de", "Dearly loved"); err != ErrNotFound {
			t.Errorf("no german version: %v", err)
		}
		// The passage also occurs in a book without German version; the match
		// in the bilingual book must win.
		if r, err := s.Verbatim(ctx, "en", "de", "best beloved of all things"); err != nil || r.AllOriginalResults[0].BookID != "bahaullah_hidden-words" || len(r.AllOriginalResults) != 2 {
			t.Errorf("verbatim(Justice) = %+v, %v", r, err)
		}
		if _, err := s.Verbatim(ctx, "en", "de", "100%_"); err != ErrNotFound {
			t.Errorf("wildcards must be literal: %v", err)
		}
	})

	t.Run("find paragraphs", func(t *testing.T) {
		page, err := s.FindParagraphs(ctx, ParagraphQuery{Language: "en", Query: "justice", Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 4 || len(page.Paragraphs) != 4 {
			t.Fatalf("page = %+v", page)
		}
		for _, r := range page.Paragraphs {
			if r.Book.BookID == "uhj20200320_001" && r.Paragraph.Index == 2 && (r.Book.PublishedAt.Year() != 2020 || r.Book.Author != "The Universal House of Justice" ||
				r.Paragraph.Text != `Justice & unity are the "foundation".`) {
				t.Errorf("letter = %+v", r)
			}
		}
		page, _ = s.FindParagraphs(ctx, ParagraphQuery{Language: "en", Query: "justice", Author: "uhj", Limit: 10})
		if page.Total != 2 || page.Paragraphs[0].Book.AuthorCode != "uhj" {
			t.Errorf("by author = %+v", page)
		}
		page, _ = s.FindParagraphs(ctx, ParagraphQuery{Language: "en", Query: "justice", BookID: "abdulbaha_talks", Limit: 10})
		if page.Total != 1 || page.Paragraphs[0].Book.Title != "Talks" {
			t.Errorf("by book = %+v", page)
		}
		page, _ = s.FindParagraphs(ctx, ParagraphQuery{Language: "en", Query: "justice", Limit: 1, Offset: 3})
		if page.Total != 4 || len(page.Paragraphs) != 1 {
			t.Errorf("paging = %+v", page)
		}
	})

	t.Run("quote candidates", func(t *testing.T) {
		ps, err := s.QuoteCandidates(ctx, "en", []string{"best beloved of all things", "nothing like this"}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(ps) != 2 {
			t.Fatalf("candidates = %+v", ps)
		}
		// Phrases match on stems, not on punctuation and inflection.
		if ps, _ := s.QuoteCandidates(ctx, "de", []string{"Liebste von allem ist Mir die Gerechtigkeit"}, 10); len(ps) != 1 || ps[0].Book.Title != "Die Verborgenen Worte" {
			t.Errorf("german = %+v", ps)
		}
		if ps, _ := s.QuoteCandidates(ctx, "en", nil, 10); len(ps) != 0 {
			t.Errorf("no phrases = %+v", ps)
		}
	})

	t.Run("paragraph counts", func(t *testing.T) {
		counts, err := s.ParagraphCounts(ctx, []string{"bahaullah_hidden-words", "abdulbaha_talks", "nope"})
		if err != nil {
			t.Fatal(err)
		}
		if len(counts) != 2 || counts["bahaullah_hidden-words"]["de"] != 5 || counts["bahaullah_hidden-words"]["en"] != 5 ||
			len(counts["abdulbaha_talks"]) != 1 {
			t.Errorf("counts = %v", counts)
		}
	})

	t.Run("paragraph at", func(t *testing.T) {
		p, err := s.ParagraphAt(ctx, "bahaullah_hidden-words", "de", 4)
		if err != nil {
			t.Fatal(err)
		}
		if p.Book.Title != "Die Verborgenen Worte" || p.Book.Author != "Bahá’u’lláh" || p.Paragraph.Label() != "2:1" {
			t.Errorf("paragraph = %+v", p)
		}
		if _, err := s.ParagraphAt(ctx, "abdulbaha_talks", "de", 0); err != ErrNotFound {
			t.Errorf("missing err = %v", err)
		}
	})

	t.Run("outdated schema forces reimport", func(t *testing.T) {
		if _, err := s.pool.Exec(ctx, `UPDATE sync_state SET value = '1' WHERE key = 'schema_version'`); err != nil {
			t.Fatal(err)
		}
		defer s.pool.Exec(ctx, `UPDATE sync_state SET value = $1 WHERE key = 'schema_version'`, SchemaVersion)
		if c, err := s.AppliedCommit(ctx); err != nil || c != "" {
			t.Errorf("commit with outdated schema = %q, %v", c, err)
		}
		if ok, _ := s.SchemaUpToDate(ctx); ok {
			t.Error("schema reported up to date")
		}
	})

	t.Run("reimport is atomic", func(t *testing.T) {
		broken := func(yield func(*book.Content, error) bool) {
			c, _ := book.ReadXML(strings.NewReader(fixtureXML[0]))
			if yield(c, nil) {
				yield(c, nil) // duplicate translation violates the primary key
			}
		}
		if _, err := s.Import(ctx, broken, "def456"); err == nil {
			t.Fatal("expected duplicate import to fail")
		}
		if c, _ := s.AppliedCommit(ctx); c != "abc123" {
			t.Errorf("commit after failed import = %q", c)
		}
		if b, err := s.Library(ctx, "en"); err != nil || len(b) != 3 {
			t.Errorf("content after failed import = %v, %v", b, err)
		}
	})
}
