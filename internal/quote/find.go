package quote

import (
	"cmp"
	"context"
	"slices"

	"github.com/holybook/app/internal/store"
)

// MatchType is how a text relates to a paragraph it was matched against,
// best first.
type MatchType int

const (
	// Exact: the text is a verbatim (possibly abridged) excerpt of the
	// paragraph.
	Exact MatchType = iota
	// Close: most of the text matches the paragraph, with small differences in
	// wording.
	Close
	// Partial: only part of the text matches. It quotes part of the paragraph
	// within other text, contains the whole paragraph and more, or spans
	// several paragraphs.
	Partial
)

func (t MatchType) String() string {
	return [...]string{"exact", "close", "partial"}[t]
}

// Match is a paragraph a text quotes.
type Match struct {
	store.ParagraphInBook
	Type    MatchType
	Overlap Overlap
}

// Candidates retrieves the paragraphs that contain any of the given phrases
// (implemented by *store.Store).
type Candidates interface {
	QuoteCandidates(ctx context.Context, language string, phrases []string, limit int) ([]store.ParagraphInBook, error)
}

const maxCandidates = 30

// Find returns the paragraphs in language that text quotes, best match first.
//
// Candidates are retrieved with full-text search on overlapping short phrases
// of the text and then verified on the normalized wording.
func Find(ctx context.Context, src Candidates, language, text string) ([]Match, error) {
	phrases := SearchPhrases(text)
	if len(phrases) == 0 {
		return nil, nil
	}
	candidates, err := src.QuoteCandidates(ctx, language, phrases, maxCandidates)
	if err != nil {
		return nil, err
	}
	var matches []Match
	for _, c := range candidates {
		if m, ok := classify(text, c); ok {
			matches = append(matches, m)
		}
	}
	slices.SortStableFunc(matches, func(a, b Match) int {
		return cmp.Or(
			cmp.Compare(a.Type, b.Type),
			cmp.Compare(b.Overlap.QuoteCoverage, a.Overlap.QuoteCoverage),
			cmp.Compare(citationPriority(a.Book.AuthorCode), citationPriority(b.Book.AuthorCode)),
		)
	})
	return matches, nil
}

func classify(text string, c store.ParagraphInBook) (Match, bool) {
	m := Match{ParagraphInBook: c, Overlap: MeasureOverlap(text, c.Paragraph.Text)}
	switch {
	case IsExcerptOf(text, c.Paragraph.Text):
		m.Type = Exact
	case m.Overlap.QuoteCoverage >= 0.6:
		m.Type = Close
	case m.Overlap.QuoteCoverage >= 0.2 || m.Overlap.ParagraphCoverage >= 0.6:
		m.Type = Partial
	default:
		return m, false
	}
	return m, true
}

// citationPriority ranks authors for citation. Passages are often quoted by
// later authors: Shoghi Effendi and the Universal House of Justice quote the
// Central Figures, compilations quote everyone. Where several match equally
// well, the original work is the one to cite.
func citationPriority(author string) int {
	switch author {
	case "bahaullah", "bab", "abdulbaha":
		return 0
	case "shoghieffendi":
		return 1
	case "uhj":
		return 2
	default:
		return 3
	}
}
