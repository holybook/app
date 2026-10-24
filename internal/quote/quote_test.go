package quote

import (
	"context"
	"slices"
	"testing"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
)

const paragraph = "The earth is but one country, and mankind its citizens. Bahá’u’lláh has said that the " +
	"best beloved of all things in My sight is Justice."

func TestNormalize(t *testing.T) {
	for _, s := range []string{"Bahá’u’lláh — “Riḍván”!", `Baha'u'llah - "Ridvan"`} {
		if got := Normalize(s); got != "baha u llah ridvan" {
			t.Errorf("Normalize(%q) = %q", s, got)
		}
	}
}

func TestIsExcerptOf(t *testing.T) {
	for quote, want := range map[string]bool{
		// Typographic differences.
		`"the earth is but one country"`:             true,
		"Baha'u'llah has said that the best beloved": true,
		// Abridged quotations match if the fragments appear in order.
		"The earth is but one country … is Justice.":    true,
		"The earth is but one country [...] is Justice": true,
		"is Justice … The earth is but one country":     false,
		// Word boundaries.
		"arth is but one countr": false,
		"":                       false,
	} {
		if got := IsExcerptOf(quote, paragraph); got != want {
			t.Errorf("IsExcerptOf(%q) = %v", quote, got)
		}
	}
}

func TestMeasureOverlap(t *testing.T) {
	embedded := MeasureOverlap("As we read: the earth is but one country, and so on", paragraph)
	if embedded.QuoteCoverage < 0.3 || embedded.QuoteCoverage > 0.8 || embedded.ParagraphCoverage >= 0.5 {
		t.Errorf("embedded = %+v", embedded)
	}
	if o := MeasureOverlap("Something entirely different was written here", paragraph); o.QuoteCoverage != 0 {
		t.Errorf("unrelated = %+v", o)
	}
}

func TestSearchPhrases(t *testing.T) {
	for quote, want := range map[string][]string{
		"one two three four five six seven eight nine": {"one two three four five", "four five six seven eight", "five six seven eight nine"},
		"short quote":              {"short quote"},
		"first part … second part": {"first part", "second part"},
		"— …":                      nil,
	} {
		if got := SearchPhrases(quote); !slices.Equal(got, want) {
			t.Errorf("SearchPhrases(%q) = %q", quote, got)
		}
	}
	long := ""
	for range 200 {
		long += "word "
	}
	if got := SearchPhrases(long + "end"); len(got) != 2 {
		t.Errorf("repeated windows are deduplicated: %q", got)
	}
}

type fakeCandidates []store.ParagraphInBook

func (f fakeCandidates) QuoteCandidates(context.Context, string, []string, int) ([]store.ParagraphInBook, error) {
	return f, nil
}

func candidate(bookID, author, text string) store.ParagraphInBook {
	return store.ParagraphInBook{
		Book:      store.Translation{BookID: bookID, AuthorCode: author, Language: "en"},
		Paragraph: book.Paragraph{Text: text},
	}
}

func TestFind(t *testing.T) {
	src := fakeCandidates{
		candidate("unrelated", "bahaullah", "Something entirely different was written here."),
		candidate("uhj-letter", "uhj", "As Bahá’u’lláh wrote: The earth is but one country, and mankind its citizens."),
		candidate("talk", "abdulbaha", "The earth is but one country, and mankind its citizens, said He."),
		candidate("variant", "bahaullah", "The earth is only one country and mankind its citizens."),
	}
	matches, err := Find(context.Background(), src, "en", "“The earth is but one country, and mankind its citizens.”")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range matches {
		got = append(got, m.Book.BookID+":"+m.Type.String())
	}
	// Exact matches first, the original author before the one quoting it.
	want := []string{"talk:exact", "uhj-letter:exact", "variant:close"}
	if !slices.Equal(got, want) {
		t.Errorf("matches = %v, want %v", got, want)
	}
}
