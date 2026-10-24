package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/holybook/app/internal/book"
)

// Queries backing agent-driven research (the MCP tools). Unlike the queries of
// the website they filter, return the full paragraph text and always carry the
// book metadata needed to cite a paragraph.

// ParagraphInBook is a paragraph with the language version of the book it
// belongs to.
type ParagraphInBook struct {
	Book      Translation
	Paragraph book.Paragraph
}

// ParagraphQuery is a full-text query for FindParagraphs.
type ParagraphQuery struct {
	Language string
	// Query uses web search syntax ("quoted phrases", -exclude, or).
	Query string
	// Author and BookID restrict the search if not empty.
	Author string
	BookID string
	Limit  int
	Offset int
}

// ParagraphPage is one page of FindParagraphs results.
type ParagraphPage struct {
	Paragraphs []ParagraphInBook
	// Total is the number of matches across all pages (0 if the page is empty).
	Total int
}

var translationColumns = `b.id, t.language, t.title, ` + authorNameSQL("b.author", "t.language") + `, b.author, b.published_at`

// scanParagraphInBook scans paragraphColumns followed by translationColumns.
func scanParagraphInBook(row pgx.Row, extra ...any) (ParagraphInBook, error) {
	var r ParagraphInBook
	var published *time.Time
	p, err := scanParagraph(row, append([]any{&r.Book.BookID, &r.Book.Language, &r.Book.Title,
		&r.Book.Author, &r.Book.AuthorCode, &published}, extra...)...)
	r.Paragraph = p
	if published != nil {
		r.Book.PublishedAt = *published
	}
	return r, err
}

// FindParagraphs runs a full-text query over the paragraphs of a language,
// best matches first.
func (s *Store) FindParagraphs(ctx context.Context, q ParagraphQuery) (ParagraphPage, error) {
	// The query is computed once in a materialized CTE: inlined, the generic
	// plan of the cached prepared statement evaluates it for every row.
	rows, _ := s.pool.Query(ctx, `
		WITH q AS MATERIALIZED (SELECT websearch_to_tsquery(search_config($1), $2) AS query)
		SELECT `+paragraphColumns+`, `+translationColumns+`, COUNT(*) OVER ()
		FROM q, paragraphs p
		JOIN books b ON b.id = p.book
		JOIN translations t ON t.book = p.book AND t.language = p.language
		WHERE p.language = $1 AND p.text_tokens @@ q.query
		  AND ($3::text = '' OR b.author = $3) AND ($4::text = '' OR p.book = $4)
		ORDER BY ts_rank(p.text_tokens, q.query) DESC, p.book, p.index
		LIMIT $5 OFFSET $6`,
		q.Language, q.Query, q.Author, q.BookID, q.Limit, q.Offset)
	page := ParagraphPage{Paragraphs: []ParagraphInBook{}}
	for rows.Next() {
		r, err := scanParagraphInBook(rows, &page.Total)
		if err != nil {
			rows.Close()
			return page, err
		}
		page.Paragraphs = append(page.Paragraphs, r)
	}
	return page, rows.Err()
}

// QuoteCandidates returns the paragraphs of a language that contain any of the
// given phrases (in the language's full-text configuration, so inflections
// and punctuation do not matter), best matches first.
func (s *Store) QuoteCandidates(ctx context.Context, language string, phrases []string, limit int) ([]ParagraphInBook, error) {
	if len(phrases) == 0 {
		return nil, nil
	}
	args := []any{language, limit}
	terms := make([]string, len(phrases))
	for i, p := range phrases {
		args = append(args, p)
		terms[i] = fmt.Sprintf("phraseto_tsquery(search_config($1), $%d)", len(args))
	}
	// Materialized for the same reason as in FindParagraphs.
	rows, _ := s.pool.Query(ctx, `
		WITH q AS MATERIALIZED (SELECT (`+strings.Join(terms, " || ")+`) AS query)
		SELECT `+paragraphColumns+`, `+translationColumns+`
		FROM q, paragraphs p
		JOIN books b ON b.id = p.book
		JOIN translations t ON t.book = p.book AND t.language = p.language
		WHERE p.language = $1 AND p.text_tokens @@ q.query
		ORDER BY ts_rank(p.text_tokens, q.query) DESC, p.book, p.index
		LIMIT $2`, args...)
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (ParagraphInBook, error) {
		return scanParagraphInBook(row)
	})
}

// ParagraphCounts returns the number of paragraphs of each of the given books
// per language: book -> language -> count. A book is listed with exactly the
// languages it is available in.
func (s *Store) ParagraphCounts(ctx context.Context, bookIDs []string) (map[string]map[string]int, error) {
	rows, _ := s.pool.Query(ctx, `
		SELECT book, language, COUNT(*) FROM paragraphs
		WHERE book = ANY($1)
		GROUP BY book, language`, bookIDs)
	counts := map[string]map[string]int{}
	var bookID, language string
	var n int
	_, err := pgx.ForEachRow(rows, []any{&bookID, &language, &n}, func() error {
		if counts[bookID] == nil {
			counts[bookID] = map[string]int{}
		}
		counts[bookID][language] = n
		return nil
	})
	return counts, err
}

// ParagraphAt returns the paragraph at an index of a book in a language, or
// ErrNotFound.
func (s *Store) ParagraphAt(ctx context.Context, bookID, language string, index int) (ParagraphInBook, error) {
	r, err := scanParagraphInBook(s.pool.QueryRow(ctx, `
		SELECT `+paragraphColumns+`, `+translationColumns+`
		FROM paragraphs p
		JOIN books b ON b.id = p.book
		JOIN translations t ON t.book = p.book AND t.language = p.language
		WHERE p.book = $1 AND p.language = $2 AND p.index = $3`,
		bookID, language, index))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}
