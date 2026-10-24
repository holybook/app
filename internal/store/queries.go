package store

import (
	"context"
	"errors"
	"html"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/holybook/app/internal/book"
)

// Translation is one language version of a book, as listed in the library.
type Translation struct {
	BookID   string `json:"bookId"`
	Language string `json:"language"`
	Title    string `json:"title"`
	// Author is the author's display name in Language.
	Author      string    `json:"author"`
	AuthorCode  string    `json:"-"`
	PublishedAt time.Time `json:"-"`
}

// Book is a book with all of its language versions.
type Book struct {
	ID             string        `json:"id"`
	ParagraphCount int64         `json:"paragraphCount"`
	Translations   []Translation `json:"translations"`
}

// Translation returns the version in the given language, or false.
func (b *Book) Translation(language string) (Translation, bool) {
	for _, t := range b.Translations {
		if t.Language == language {
			return t, true
		}
	}
	return Translation{}, false
}

// Languages returns the languages the book is available in.
func (b *Book) Languages() []string {
	langs := make([]string, len(b.Translations))
	for i, t := range b.Translations {
		langs[i] = t.Language
	}
	return langs
}

// SearchResult is a paragraph matching a query.
type SearchResult struct {
	BookID string `json:"bookId"`
	Author string `json:"author"`
	Title  string `json:"title"`
	// HighlightedText is an HTML-escaped excerpt with matches wrapped in <b>.
	HighlightedText string         `json:"highlightedText"`
	Paragraph       book.Paragraph `json:"paragraph"`
}

// VerbatimResult is the authoritative translation of a passage: the paragraph
// at the same position in the target language, plus all source paragraphs that
// contained the passage (the first one is the one that was translated).
type VerbatimResult struct {
	TranslatedParagraph book.Paragraph `json:"translatedParagraph"`
	AllOriginalResults  []SearchResult `json:"allOriginalResults"`
}

// authorName resolves the display name of an author in a language, falling
// back to English and then to the author code.
const authorName = `COALESCE(
  (SELECT name FROM author_names WHERE author = %[1]s AND language = %[2]s),
  (SELECT name FROM author_names WHERE author = %[1]s AND language = 'en'),
  %[1]s)`

func authorNameSQL(authorCol, languageExpr string) string {
	return strings.NewReplacer("%[1]s", authorCol, "%[2]s", languageExpr).Replace(authorName)
}

// Languages returns all languages any book is available in.
func (s *Store) Languages(ctx context.Context) ([]string, error) {
	rows, _ := s.pool.Query(ctx, `SELECT DISTINCT language FROM translations ORDER BY language`)
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// Authors returns all authors in display order.
func (s *Store) Authors(ctx context.Context) ([]Author, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT a.id, n.language, n.name
		FROM authors a LEFT JOIN author_names n ON n.author = a.id
		ORDER BY a.position, a.id`)
	var authors []Author
	var id string
	var lang, name *string
	_, err := pgx.ForEachRow(rows, []any{&id, &lang, &name}, func() error {
		if len(authors) == 0 || authors[len(authors)-1].ID != id {
			authors = append(authors, Author{ID: id, Names: map[string]string{}})
		}
		if lang != nil {
			authors[len(authors)-1].Names[*lang] = *name
		}
		return nil
	})
	return authors, err
}

// Library lists all books available in a language, ordered by author (in
// canonical order), then newest first for dated works, then by title.
func (s *Store) Library(ctx context.Context, language string) ([]Translation, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT b.id, t.language, t.title, `+authorNameSQL("b.author", "t.language")+`, b.author, b.published_at
		FROM translations t
		JOIN books b ON b.id = t.book
		JOIN authors a ON a.id = b.author
		WHERE t.language = $1
		ORDER BY a.position, a.id, b.published_at DESC NULLS LAST, t.title`, language)
	return collectTranslations(rows)
}

func collectTranslations(rows pgx.Rows) ([]Translation, error) {
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Translation, error) {
		var t Translation
		var published *time.Time
		err := row.Scan(&t.BookID, &t.Language, &t.Title, &t.Author, &t.AuthorCode, &published)
		if published != nil {
			t.PublishedAt = *published
		}
		return t, err
	})
}

// ErrNotFound is returned when a requested entity does not exist.
var ErrNotFound = errors.New("not found")

// Book returns a book with its translations, or ErrNotFound.
func (s *Store) Book(ctx context.Context, id string) (*Book, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT b.id, t.language, t.title, `+authorNameSQL("b.author", "t.language")+`, b.author, b.published_at
		FROM books b JOIN translations t ON t.book = b.id
		WHERE b.id = $1
		ORDER BY t.language`, id)
	translations, err := collectTranslations(rows)
	if err != nil {
		return nil, err
	}
	if len(translations) == 0 {
		return nil, ErrNotFound
	}
	b := &Book{ID: id, Translations: translations}
	err = s.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(n), 0) FROM (
		  SELECT COUNT(*) AS n FROM paragraphs WHERE book = $1 GROUP BY language
		) counts`, id).Scan(&b.ParagraphCount)
	return b, err
}

const paragraphColumns = `p.index, p.number, p.section_path, p.type, p.text`

func scanParagraph(row pgx.Row, extra ...any) (book.Paragraph, error) {
	var p book.Paragraph
	var number *int32
	var typ string
	if err := row.Scan(append([]any{&p.Index, &number, &p.SectionPath, &typ, &p.Text}, extra...)...); err != nil {
		return p, err
	}
	if number != nil && *number > 0 {
		n := int(*number)
		p.Number = &n
	}
	t, err := book.ParseParagraphType(typ)
	p.Type = t
	return p, err
}

// Paragraphs returns the paragraphs of a book in a language, optionally
// restricted to an inclusive index range.
func (s *Store) Paragraphs(ctx context.Context, bookID, language string, start, end *int) ([]book.Paragraph, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT `+paragraphColumns+` FROM paragraphs p
		WHERE p.book = $1 AND p.language = $2
		  AND ($3::int IS NULL OR p.index >= $3)
		  AND ($4::int IS NULL OR p.index <= $4)
		ORDER BY p.index`, bookID, language, start, end)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (book.Paragraph, error) {
		return scanParagraph(row)
	})
}

// Highlight markers used by ts_headline. Control characters cannot occur in
// the texts, so they can be safely replaced after HTML-escaping the excerpt.
const (
	highlightStart   = "\x02"
	highlightStop    = "\x03"
	headlineOptions  = "StartSel=" + highlightStart + ", StopSel=" + highlightStop + ", MinWords=20, MaxWords=45"
	highlightOpenTag = "<b>"
	highlightEndTag  = "</b>"
)

var highlightReplacer = strings.NewReplacer(highlightStart, highlightOpenTag, highlightStop, highlightEndTag)

// highlightHTML escapes a ts_headline excerpt of text, turns its markers
// into tags, and marks truncation with ellipses.
func highlightHTML(headline, text string) string {
	h := highlightReplacer.Replace(html.EscapeString(headline))
	plain := strings.NewReplacer(highlightStart, "", highlightStop, "").Replace(headline)
	if plain != "" && !strings.HasPrefix(strings.TrimSpace(text), strings.TrimSpace(plain)) {
		h = "… " + h
	}
	if plain != "" && !strings.HasSuffix(strings.TrimSpace(text), strings.TrimSpace(plain)) {
		h += " …"
	}
	return h
}

// SearchPage is one page of search results.
type SearchPage struct {
	Results []SearchResult
	// Total is the number of matches across all pages (0 if the page is empty).
	Total int
}

// Search runs a web-search style full-text query ("quoted phrases", -exclude,
// or) over the paragraphs of a language, best matches first. A limit <= 0
// returns all results.
func (s *Store) Search(ctx context.Context, language, query string, limit, offset int) (SearchPage, error) {
	var lim *int
	if limit > 0 {
		lim = &limit
	}
	rows, _ := s.pool.Query(ctx, `
		WITH q AS (SELECT websearch_to_tsquery(search_config($1), $2) AS query),
		hits AS (
		  SELECT p.book, p.index, ts_rank(p.text_tokens, q.query) AS rank, COUNT(*) OVER () AS total
		  FROM paragraphs p, q
		  WHERE p.language = $1 AND p.text_tokens @@ q.query
		  ORDER BY rank DESC, p.book, p.index
		  LIMIT $3 OFFSET $4
		)
		SELECT `+paragraphColumns+`, p.book, t.title, `+authorNameSQL("b.author", "$1")+`,
		       ts_headline(search_config($1), p.text, q.query, $5), h.total
		FROM hits h
		CROSS JOIN q
		JOIN paragraphs p ON p.book = h.book AND p.language = $1 AND p.index = h.index
		JOIN books b ON b.id = p.book
		JOIN translations t ON t.book = p.book AND t.language = p.language
		ORDER BY h.rank DESC, p.book, p.index`,
		language, query, lim, offset, headlineOptions)
	var page SearchPage
	page.Results = []SearchResult{}
	for rows.Next() {
		var r SearchResult
		var headline string
		p, err := scanParagraph(rows, &r.BookID, &r.Title, &r.Author, &headline, &page.Total)
		if err != nil {
			rows.Close()
			return page, err
		}
		r.Paragraph = p
		r.HighlightedText = highlightHTML(headline, p.Text)
		page.Results = append(page.Results, r)
	}
	return page, rows.Err()
}

// escapeLike escapes the LIKE wildcards in s.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Verbatim finds the authoritative translation of a passage: the first source
// paragraph (in fromLanguage) containing text, preferring books that exist in
// toLanguage, and the paragraph at the same position of that book in
// toLanguage. Returns ErrNotFound if the passage
// does not occur or the book has no such paragraph in the target language.
func (s *Store) Verbatim(ctx context.Context, fromLanguage, toLanguage, text string) (*VerbatimResult, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT `+paragraphColumns+`, p.book, t.title, `+authorNameSQL("b.author", "$1")+`,
		       ts_headline(search_config($1), p.text, websearch_to_tsquery(search_config($1), $3), $4)
		FROM paragraphs p
		JOIN books b ON b.id = p.book
		JOIN translations t ON t.book = p.book AND t.language = p.language
		WHERE p.language = $1 AND p.text ILIKE $2
		ORDER BY EXISTS (
		    SELECT 1 FROM paragraphs x WHERE x.book = p.book AND x.index = p.index AND x.language = $5
		  ) DESC, p.book, p.index`,
		fromLanguage, "%"+escapeLike(text)+"%", text, headlineOptions, toLanguage)
	originals, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (SearchResult, error) {
		var r SearchResult
		var headline string
		p, err := scanParagraph(row, &r.BookID, &r.Title, &r.Author, &headline)
		r.Paragraph = p
		r.HighlightedText = highlightHTML(headline, p.Text)
		return r, err
	})
	if err != nil {
		return nil, err
	}
	if len(originals) == 0 {
		return nil, ErrNotFound
	}
	first := originals[0]
	translated, err := scanParagraph(s.pool.QueryRow(ctx, `
		SELECT `+paragraphColumns+` FROM paragraphs p
		WHERE p.book = $1 AND p.index = $2 AND p.language = $3`,
		first.BookID, first.Paragraph.Index, toLanguage))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &VerbatimResult{TranslatedParagraph: translated, AllOriginalResults: originals}, nil
}
