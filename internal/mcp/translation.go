package mcp

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/holybook/go/internal/quote"
	"github.com/holybook/go/internal/store"
)

const (
	maxLookupParagraphs = 20
	maxMatches          = 3
)

func (h *Handler) addTranslationTool(s *mcp.Server, cat catalog) {
	addReadOnlyTool(s, &mcp.Tool{
		Name:  "find_authoritative_translation",
		Title: "Find authoritative translations",
		Description: fmt.Sprintf(`Use when translating a text that contains quotations from the Bahá’í writings. For each paragraph (or extracted quotation) it checks whether it quotes an authoritative text in the source language and, if so, returns the source paragraph with its citation and the authoritative translation of that paragraph in the target language. Quotations must never be translated freely when an authoritative translation exists.

Pass the paragraphs in order, at most %d per call. Best results come from passing the quoted words themselves (without surrounding commentary or citation); omissions marked with "…" are supported, and quotation marks, apostrophes and diacritics may differ.

Per input, `+"`status`"+` is `+"`authoritative`"+` (verbatim quotation found), `+"`possibly_authoritative`"+` (similar or partially overlapping text found — verify) or `+"`not_found`"+`. Each match has a type: `+"`exact`"+` (the input is a verbatim excerpt of the source paragraph), `+"`close`"+` (same passage with small wording differences) or `+"`partial`"+` (only part of the input overlaps). If the translation is `+"`available`"+`, its `+"`paragraph`"+` is the authoritative translation of the whole source paragraph; when the input is only part of it, use the corresponding part verbatim, never paraphrased. Otherwise `+"`reason`"+` is `+"`not_translated`"+` (the work has no translation in the target language here) or `+"`paragraph_count_mismatch`"+` (the language versions are not structured alike, so the paragraph cannot be identified reliably); handle both the same way.`, maxLookupParagraphs),
		InputSchema: objectSchema(map[string]schema{
			"paragraphs":    stringArraySchema("Paragraphs or quotations of the text being translated, in order.", maxLookupParagraphs),
			"from_language": stringSchema("Language of the given text.", cat.languages, ""),
			"to_language":   stringSchema("Language to translate into.", cat.languages, ""),
		}, "paragraphs", "from_language", "to_language"),
	}, func(ctx context.Context, args *arguments) (any, error) {
		paragraphs := args.stringList("paragraphs", maxLookupParagraphs)
		from := args.choice("from_language", cat.languages, "")
		to := args.choice("to_language", cat.languages, "")
		if err := args.Err(); err != nil {
			return nil, err
		}
		if from == to {
			return nil, argErrorf("from_language and to_language must differ.")
		}
		res := translationLookupResponse{FromLanguage: from, ToLanguage: to, Results: make([]paragraphLookup, len(paragraphs))}
		for i, text := range paragraphs {
			r, err := h.lookUp(ctx, text, from, to)
			if err != nil {
				return nil, err
			}
			r.Input = i + 1
			res.Results[i] = r
		}
		return res, nil
	})
}

func (h *Handler) lookUp(ctx context.Context, text, from, to string) (paragraphLookup, error) {
	found, err := quote.Find(ctx, h.store, from, text)
	if err != nil || len(found) == 0 {
		return paragraphLookup{Status: "not_found", Matches: []authoritativeMatch{}}, err
	}
	ids := make([]string, len(found))
	for i, m := range found {
		ids[i] = m.Book.BookID
	}
	counts, err := h.store.ParagraphCounts(ctx, ids)
	if err != nil {
		return paragraphLookup{}, err
	}
	// Among equally good matches (the same passage quoted in several works),
	// prefer those whose translation can be identified: that is where the
	// authoritative translation is.
	slices.SortStableFunc(found, func(a, b quote.Match) int {
		return cmp.Or(cmp.Compare(a.Type, b.Type),
			compareBool(aligned(counts[b.Book.BookID], from, to), aligned(counts[a.Book.BookID], from, to)))
	})
	r := paragraphLookup{Status: "possibly_authoritative"}
	if found[0].Type == quote.Exact {
		r.Status = "authoritative"
	}
	if len(quote.Words(text)) < 5 && len(found) > 1 {
		r.Note = "The text is very short and occurs in several places; pass a longer quotation to identify its source reliably."
	}
	for _, m := range found[:min(len(found), maxMatches)] {
		am, err := h.authoritativeMatch(ctx, m, to, counts[m.Book.BookID])
		if err != nil {
			return r, err
		}
		r.Matches = append(r.Matches, am)
	}
	r.MoreMatches = max(len(found)-maxMatches, 0)
	return r, nil
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	default:
		return -1
	}
}

// aligned reports whether the paragraphs of two language versions of a book
// correspond by index, given the paragraph counts per language. They do only
// if both versions have the same number of paragraphs; otherwise one of them
// adds, drops or splits paragraphs, a defect to be fixed in the data rather
// than guessed around.
func aligned(counts map[string]int, from, to string) bool {
	n, ok := counts[to]
	return ok && n == counts[from]
}

func (h *Handler) authoritativeMatch(ctx context.Context, m quote.Match, to string, counts map[string]int) (authoritativeMatch, error) {
	am := authoritativeMatch{
		Match:             m.Type.String(),
		InputCoverage:     round2(m.Overlap.QuoteCoverage),
		ParagraphCoverage: round2(m.Overlap.ParagraphCoverage),
		Source:            h.links.searchHit(m.ParagraphInBook),
	}
	languages := slices.Sorted(maps.Keys(counts))
	switch {
	case counts[to] == 0:
		am.Translation = authoritativeTranslation{Reason: "not_translated", AvailableLanguages: languages}
	case !aligned(counts, m.Book.Language, to):
		// Logged as the work list for fixing the data in holybook/data.
		slog.Warn("paragraph count mismatch", "corpus", true, "book", m.Book.BookID,
			m.Book.Language, counts[m.Book.Language], to, counts[to])
		am.Translation = authoritativeTranslation{Reason: "paragraph_count_mismatch", AvailableLanguages: languages}
	default:
		t, err := h.store.ParagraphAt(ctx, m.Book.BookID, to, m.Paragraph.Index)
		if errors.Is(err, store.ErrNotFound) {
			am.Translation = authoritativeTranslation{Reason: "not_translated", AvailableLanguages: languages}
			break
		}
		if err != nil {
			return am, err
		}
		// Versions with the same number of paragraphs can still be shifted
		// against each other (one drops a heading, another splits a
		// paragraph); corresponding paragraphs have the same type and number.
		if t.Paragraph.Type != m.Paragraph.Type || t.Paragraph.Label() != m.Paragraph.Label() {
			slog.Warn("paragraph structure mismatch", "corpus", true, "book", m.Book.BookID, "index", m.Paragraph.Index,
				m.Book.Language, m.Paragraph.Label(), to, t.Paragraph.Label())
			am.Translation = authoritativeTranslation{Reason: "paragraph_count_mismatch", AvailableLanguages: languages}
			break
		}
		p := h.links.paragraphResult(t.Book, t.Paragraph)
		am.Translation = authoritativeTranslation{Available: true, Title: t.Book.Title, Paragraph: &p}
	}
	return am, nil
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }
