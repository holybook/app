// Package quote recognizes quotations of the writings: it finds the
// paragraphs a text quotes, tolerating the differences that quotations "in the
// wild" have from their source.
//
// Quotations rarely match the source byte for byte: typographic quotes and
// apostrophes differ (Bahá’u’lláh vs Baha'u'llah), diacritics are dropped,
// whitespace and punctuation vary and omissions are marked with ellipses. All
// comparisons therefore run on a normalized form that keeps only lower-cased
// letters and digits without diacritics.
package quote

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Normalize lower-cases text, removes diacritics and replaces every run of
// other characters than letters and digits by a single space.
func Normalize(text string) string {
	var b strings.Builder
	gap := false
	for _, r := range norm.NFKD.String(text) {
		switch {
		case unicode.Is(unicode.M, r):
			// A diacritic of the preceding letter.
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if gap && b.Len() > 0 {
				b.WriteByte(' ')
			}
			gap = false
			b.WriteRune(unicode.ToLower(r))
		default:
			gap = true
		}
	}
	return b.String()
}

// Words returns the normalized words of text.
func Words(text string) []string {
	return strings.Fields(Normalize(text))
}

// ellipsis matches omission marks inside a quotation: "...", ". . .", "…",
// optionally in square brackets.
var ellipsis = regexp.MustCompile(`\[?(?:\.\s?\.\s?\.|…)\]?`)

// fragments splits a quotation at its omission marks into the normalized
// fragments that must appear in the source.
func fragments(quote string) []string {
	var out []string
	for _, f := range ellipsis.Split(quote, -1) {
		if n := Normalize(f); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// IsExcerptOf reports whether quote is a (possibly abridged) verbatim excerpt
// of paragraph: every fragment between omission marks occurs in the paragraph
// on word boundaries, in order.
func IsExcerptOf(quote, paragraph string) bool {
	frags := fragments(quote)
	if len(frags) == 0 {
		return false
	}
	haystack := " " + Normalize(paragraph) + " "
	from := 0
	for _, f := range frags {
		at := strings.Index(haystack[from:], " "+f+" ")
		if at < 0 {
			return false
		}
		// Continue at the space after the fragment.
		from += at + len(f) + 1
	}
	return true
}

// Overlap is the word-level overlap between a quotation and a paragraph.
type Overlap struct {
	// QuoteCoverage is the share of the quotation that occurs in the paragraph.
	QuoteCoverage float64
	// ParagraphCoverage is the share of the paragraph that occurs in the
	// quotation.
	ParagraphCoverage float64
}

// MeasureOverlap compares a quotation and a paragraph on word trigrams (or
// single words for very short texts).
func MeasureOverlap(quote, paragraph string) Overlap {
	var quoteFragments [][]string
	quoteWords := 0
	for _, f := range fragments(quote) {
		words := strings.Fields(f)
		quoteFragments = append(quoteFragments, words)
		quoteWords += len(words)
	}
	paragraphWords := Words(paragraph)
	n := 1
	if min(quoteWords, len(paragraphWords)) >= 3 {
		n = 3
	}
	// Shingles must not span an omission, since those words are not adjacent
	// in the source.
	quoteShingles := map[string]bool{}
	for _, words := range quoteFragments {
		for s := range shingles(words, n) {
			quoteShingles[s] = true
		}
	}
	paragraphShingles := shingles(paragraphWords, n)
	if len(quoteShingles) == 0 || len(paragraphShingles) == 0 {
		return Overlap{}
	}
	common := 0
	for s := range quoteShingles {
		if paragraphShingles[s] {
			common++
		}
	}
	return Overlap{
		QuoteCoverage:     float64(common) / float64(len(quoteShingles)),
		ParagraphCoverage: float64(common) / float64(len(paragraphShingles)),
	}
}

func shingles(words []string, n int) map[string]bool {
	set := map[string]bool{}
	for i := 0; i+n <= len(words); i++ {
		set[strings.Join(words[i:i+n], " ")] = true
	}
	return set
}

const (
	phraseWindow = 5
	phraseStep   = 3
	maxPhrases   = 30
)

// SearchPhrases returns short phrases of the original (unnormalized) wording
// that full-text search can use to find candidate paragraphs. Overlapping
// windows make sure a candidate is found even if the quotation is embedded in
// other text or differs from the source in a few places.
func SearchPhrases(quote string) []string {
	var phrases []string
	seen := map[string]bool{}
	add := func(tokens []string) {
		p := strings.Join(tokens, " ")
		if p != "" && !seen[p] {
			seen[p] = true
			phrases = append(phrases, p)
		}
	}
	for _, fragment := range ellipsis.Split(quote, -1) {
		var tokens []string
		for _, t := range strings.Fields(fragment) {
			if strings.IndexFunc(t, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
				tokens = append(tokens, t)
			}
		}
		if len(tokens) <= phraseWindow {
			add(tokens)
			continue
		}
		for i := 0; i+phraseWindow <= len(tokens); i += phraseStep {
			add(tokens[i : i+phraseWindow])
		}
		add(tokens[len(tokens)-phraseWindow:])
	}
	if len(phrases) <= maxPhrases {
		return phrases
	}
	// Sample evenly so long inputs are still covered from beginning to end.
	sampled := make([]string, maxPhrases)
	for i := range sampled {
		sampled[i] = phrases[i*len(phrases)/maxPhrases]
	}
	return sampled
}
