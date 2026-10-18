package fetch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/holybook/go/internal/book"
)

// TestLiveParity fetches a few books from the real sources and compares them
// with the files of the holybook/data repository, which the original tooling
// produced. (Some files in the data repo were later hand-sanitized; the books
// below were not.) It needs network access and the data repo:
//
//	HOLYBOOK_DATA=../data go test ./internal/fetch -run Live
func TestLiveParity(t *testing.T) {
	dataDir := os.Getenv("HOLYBOOK_DATA")
	if dataDir == "" {
		t.Skip("HOLYBOOK_DATA not set")
	}
	lines := []string{
		"en shoghieffendi shoghieffendi_god-passes-by https://www.bahai.org/library/authoritative-texts/shoghi-effendi/god-passes-by/god-passes-by.xhtml",
		"en bahaullah bahaullah_kitab-i-aqdas https://www.bahai.org/library/authoritative-texts/bahaullah/kitab-i-aqdas/kitab-i-aqdas.xhtml",
		"en uhj uhj20251201_001 https://bahai.org/library/authoritative-texts/the-universal-house-of-justice/messages/20251201_001/20251201_001.xhtml",
		"de uhj uhj20250320_001 https://bibliothek.bahai.de/download/xml/export_UHJ_2025-03-20_de%20(1.01)%20MAS-XML.xml",
	}
	for _, line := range lines {
		d, err := book.ParseDescriptor(line)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(d.ID, func(t *testing.T) {
			got, err := FetchContent(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(d.FilePath(filepath.Join(dataDir, "content")))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			want, err := book.ReadXML(f)
			if err != nil {
				t.Fatal(err)
			}
			if got.Title != want.Title || !got.PublishedAt.Equal(want.PublishedAt) {
				t.Errorf("metadata: got %q %v, want %q %v", got.Title, got.PublishedAt, want.Title, want.PublishedAt)
			}
			if len(got.Paragraphs) != len(want.Paragraphs) {
				t.Errorf("got %d paragraphs, want %d", len(got.Paragraphs), len(want.Paragraphs))
			}
			for i := range min(len(got.Paragraphs), len(want.Paragraphs)) {
				if !reflect.DeepEqual(got.Paragraphs[i], want.Paragraphs[i]) {
					t.Errorf("paragraph %d:\n got %+v\nwant %+v", i, got.Paragraphs[i], want.Paragraphs[i])
					break
				}
			}
		})
	}
}

func TestListParsers(t *testing.T) {
	doc, _ := parseHTML([]byte(`<div id="divMessageList">
		<a href="pubReader.php?titleLangUri=uhj-muhj20200320-de">x</a>
		<a href="pubReader.php?titleLangUri=uhj-muhj20190500-de">y</a>
		<a href="other.php">z</a></div>`))
	got := bibliothekUHJList(doc)
	if len(got) != 2 || got[0].ID != "uhj20200320_001" || got[1].ID != "uhj20190501_001" ||
		got[1].URL != "https://bibliothek.bahai.de/download/xml/export_UHJ_2019-05-00_de%20(1.01)%20MAS-XML.xml" {
		t.Errorf("bibliothek list = %+v", got)
	}

	doc, _ = parseHTML([]byte(`<table>
		<tr><td class="col-date"><a href="20200722_001/1">x</a></td></tr>
		<tr><td class="col-date"><a href="/library/authoritative-texts/the-universal-house-of-justice/messages/20191126_001/1">y</a></td></tr>
		<tr><td class="col-date"><a href="20200722_001/1">dup</a></td></tr></table>`))
	got = referenceLibraryUHJList(doc)
	if len(got) != 2 || got[1].ID != "uhj20191126_001" || got[0].ID != "uhj20200722_001" ||
		got[0].URL != "https://bahai.org/library/authoritative-texts/the-universal-house-of-justice/messages/20200722_001/20200722_001.xhtml" {
		t.Errorf("uhj list = %+v", got)
	}

	doc, _ = parseHTML([]byte(`
		<a href="/library/authoritative-texts/bahaullah/hidden-words/">a</a>
		<a href="https://www.bahai.org/library/authoritative-texts/bahaullah/hidden-words/">dup</a>
		<a href="/library/authoritative-texts/bahaullah/kitab-i-aqdas/">b</a>
		<a href="/library/authoritative-texts/bahaullah/kitab-i-aqdas/1">deep</a>`))
	got = referenceLibraryWorkList("bahaullah", "bahaullah")(doc)
	if len(got) != 2 || got[0].ID != "bahaullah_hidden-words" || got[1].URL != "https://www.bahai.org/library/authoritative-texts/bahaullah/kitab-i-aqdas/kitab-i-aqdas.xhtml" {
		t.Errorf("work list = %+v", got)
	}
}
