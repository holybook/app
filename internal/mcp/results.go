package mcp

import (
	"net/url"
	"strconv"

	"github.com/holybook/go/internal/book"
	"github.com/holybook/go/internal/store"
)

// Result shapes of the tools. They are tuned for agents: everything needed to
// quote and cite a paragraph is inline, and fields without information (e.g.
// the type of ordinary body paragraphs) are omitted to save tokens.

type bookResult struct {
	BookID   string `json:"bookId"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	AuthorID string `json:"authorId"`
	// Date is the publication date (ISO 8601), known mostly for letters.
	Date     string `json:"date,omitempty"`
	Language string `json:"language"`
	URL      string `json:"url"`
}

type paragraphResult struct {
	// Index is the position within the book; use with get_passage.
	Index int `json:"index"`
	// Number is the paragraph number as printed, e.g. "12" or "2.1:4".
	Number string `json:"number,omitempty"`
	// Type is omitted for ordinary body paragraphs.
	Type string `json:"type,omitempty"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

type searchHit struct {
	BookID string `json:"bookId"`
	Title  string `json:"title"`
	Author string `json:"author"`
	Date   string `json:"date,omitempty"`
	paragraphResult
}

type searchResponse struct {
	Query      string      `json:"query"`
	Language   string      `json:"language"`
	Total      int         `json:"total"`
	Offset     int         `json:"offset"`
	Results    []searchHit `json:"results"`
	NextOffset *int        `json:"nextOffset,omitempty"`
	Hint       string      `json:"hint,omitempty"`
}

type passageResponse struct {
	Book               bookResult        `json:"book"`
	ParagraphCount     int               `json:"paragraphCount"`
	AvailableLanguages []string          `json:"availableLanguages"`
	Paragraphs         []paragraphResult `json:"paragraphs"`
	NextIndex          *int              `json:"nextIndex,omitempty"`
}

type bookListResponse struct {
	Language   string       `json:"language"`
	Total      int          `json:"total"`
	Offset     int          `json:"offset"`
	Books      []bookResult `json:"books"`
	NextOffset *int         `json:"nextOffset,omitempty"`
}

type translationLookupResponse struct {
	FromLanguage string            `json:"fromLanguage"`
	ToLanguage   string            `json:"toLanguage"`
	Results      []paragraphLookup `json:"results"`
}

type paragraphLookup struct {
	// Input is the 1-based position of the paragraph in the request.
	Input int `json:"input"`
	// Status is "authoritative", "possibly_authoritative" or "not_found".
	Status      string               `json:"status"`
	Matches     []authoritativeMatch `json:"matches"`
	MoreMatches int                  `json:"moreMatches,omitempty"`
	Note        string               `json:"note,omitempty"`
}

type authoritativeMatch struct {
	// Match is "exact", "close" or "partial", see the tool description.
	Match string `json:"match"`
	// InputCoverage is the share of the input found in the source paragraph.
	InputCoverage float64 `json:"inputCoverage"`
	// ParagraphCoverage is the share of the source paragraph found in the
	// input.
	ParagraphCoverage float64                  `json:"paragraphCoverage"`
	Source            searchHit                `json:"source"`
	Translation       authoritativeTranslation `json:"translation"`
}

type authoritativeTranslation struct {
	Available bool `json:"available"`
	// Reason tells why no translation is given: "not_translated" or
	// "paragraph_count_mismatch".
	Reason string `json:"reason,omitempty"`
	Title  string `json:"title,omitempty"`
	// Paragraph is the authoritative translation of the whole source
	// paragraph.
	Paragraph          *paragraphResult `json:"paragraph,omitempty"`
	AvailableLanguages []string         `json:"availableLanguages,omitempty"`
}

// links builds links into the website's reader.
type links struct{ baseURL string }

func (l links) book(bookID, language string) string {
	return l.baseURL + "/books/" + url.PathEscape(bookID) + "?lang=" + url.QueryEscape(language)
}

func (l links) paragraph(bookID, language string, index int) string {
	return l.book(bookID, language) + "#p-" + strconv.Itoa(index)
}

func formatDate(t store.Translation) string {
	if t.PublishedAt.IsZero() {
		return ""
	}
	return t.PublishedAt.Format("2006-01-02")
}

func (l links) bookResult(t store.Translation) bookResult {
	return bookResult{
		BookID:   t.BookID,
		Title:    t.Title,
		Author:   t.Author,
		AuthorID: t.AuthorCode,
		Date:     formatDate(t),
		Language: t.Language,
		URL:      l.book(t.BookID, t.Language),
	}
}

func (l links) paragraphResult(t store.Translation, p book.Paragraph) paragraphResult {
	r := paragraphResult{
		Index:  p.Index,
		Number: p.Label(),
		Text:   p.Text,
		URL:    l.paragraph(t.BookID, t.Language, p.Index),
	}
	if p.Type != book.Body {
		r.Type = string(p.Type)
	}
	return r
}

func (l links) searchHit(r store.ParagraphInBook) searchHit {
	return searchHit{
		BookID:          r.Book.BookID,
		Title:           r.Book.Title,
		Author:          r.Book.Author,
		Date:            formatDate(r.Book),
		paragraphResult: l.paragraphResult(r.Book, r.Paragraph),
	}
}

// nextOffset returns the offset of the next page, or nil after the last one.
func nextOffset(offset, limit, total int) *int {
	if offset+limit < total {
		return ptr(offset + limit)
	}
	return nil
}
