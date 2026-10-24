package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/holybook/app/internal/book"
)

// source is a web page listing books, and the index file its descriptors are
// written to.
type source struct {
	url      string
	fileName string
	parse    func(*goquery.Document) []book.Descriptor
}

// referenceLibraryAuthors maps each bahai.org author index path segment to the
// internal author code. The Universal House of Justice is excluded: its works
// are messages enumerated separately rather than a flat list of works.
var referenceLibraryAuthors = []struct{ urlAuthor, code string }{
	{"the-bab", "bab"},
	{"bahaullah", "bahaullah"},
	{"abdul-baha", "abdulbaha"},
	{"shoghi-effendi", "shoghieffendi"},
	{"compilations", "compilation"},
}

func sources() []source {
	list := []source{
		{"https://bibliothek.bahai.de/pubAuthor.php?authorCode=uhj", "de.uhj.txt", bibliothekUHJList},
		{"https://www.bahai.org/library/authoritative-texts/the-universal-house-of-justice/messages/", "en.uhj.txt", referenceLibraryUHJList},
	}
	for _, a := range referenceLibraryAuthors {
		list = append(list, source{
			"https://www.bahai.org/library/authoritative-texts/" + a.urlAuthor + "/",
			"en." + a.code + ".txt",
			referenceLibraryWorkList(a.urlAuthor, a.code),
		})
	}
	return list
}

// FetchIndex downloads all source listings and writes one index file of
// descriptors per source into outputDir.
func FetchIndex(ctx context.Context, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	for _, src := range sources() {
		slog.Info("fetching sources", "url", src.url)
		body, _, err := get(ctx, src.url)
		if err != nil {
			return err
		}
		doc, err := parseHTML(body)
		if err != nil {
			return fmt.Errorf("parsing %s: %w", src.url, err)
		}
		descriptors := src.parse(doc)
		path := filepath.Join(outputDir, src.fileName)
		slog.Info("writing descriptors", "count", len(descriptors), "file", path)
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		err = book.WriteDescriptors(f, descriptors)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// referenceLibraryWorkList enumerates every work on a bahai.org author index
// page (e.g. /library/authoritative-texts/bahaullah/). Each work's full text is
// served as a single xhtml at <work>/<slug>.xhtml.
func referenceLibraryWorkList(urlAuthor, authorCode string) func(*goquery.Document) []book.Descriptor {
	workLink := regexp.MustCompile(`^/library/authoritative-texts/` + regexp.QuoteMeta(urlAuthor) + `/([^/]+)/$`)
	return func(doc *goquery.Document) []book.Descriptor {
		var result []book.Descriptor
		seen := map[string]bool{}
		doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
			href := a.AttrOr("href", "")
			href = strings.TrimPrefix(href, "https://www.bahai.org")
			href = strings.TrimPrefix(href, "https://bahai.org")
			m := workLink.FindStringSubmatch(href)
			if m == nil || seen[m[1]] {
				return
			}
			slug := m[1]
			seen[slug] = true
			result = append(result, book.Descriptor{
				ID:       truncate(authorCode+"_"+slug, 128),
				Language: "en",
				Author:   authorCode,
				URL:      "https://www.bahai.org/library/authoritative-texts/" + urlAuthor + "/" + slug + "/" + slug + ".xhtml",
			})
		})
		return result
	}
}

// messageID matches the id of a message (date and sequence number) in the
// links of the bahai.org message list, which may be relative ("20200722_001/1")
// or absolute ("/library/.../messages/20200722_001/1").
var messageID = regexp.MustCompile(`\b(\d{8}_\d{3})\b`)

func referenceLibraryUHJList(doc *goquery.Document) []book.Descriptor {
	var result []book.Descriptor
	seen := map[string]bool{}
	doc.Find("td.col-date a[href]").Each(func(_ int, a *goquery.Selection) {
		m := messageID.FindStringSubmatch(a.AttrOr("href", ""))
		if m == nil || seen[m[1]] {
			return
		}
		id := m[1]
		seen[id] = true
		result = append(result, book.Descriptor{
			ID:       "uhj" + id,
			Language: "en",
			Author:   "uhj",
			URL:      "https://bahai.org/library/authoritative-texts/the-universal-house-of-justice/messages/" + id + "/" + id + ".xhtml",
		})
	})
	return result
}

func bibliothekUHJList(doc *goquery.Document) []book.Descriptor {
	var result []book.Descriptor
	doc.Find("div#divMessageList a[href]").Each(func(_ int, a *goquery.Selection) {
		href := a.AttrOr("href", "")
		if !strings.Contains(href, "uhj-muhj") {
			return
		}
		date := strings.ReplaceAll(strings.ReplaceAll(href, "pubReader.php?titleLangUri=uhj-muhj", ""), "-de", "")
		if len(date) < 8 {
			slog.Warn("skipping message link without date", "href", href)
			return
		}
		// Messages with an unknown day are dated YYYY-MM-00 by the source. The
		// id uses day 01 to stay aligned with the English (bahai.org) ids of the
		// same message; the URL keeps 00 so the file still resolves.
		idDate := date
		if date[6:8] == "00" {
			idDate = date[:6] + "01"
		}
		result = append(result, book.Descriptor{
			ID:       "uhj" + idDate + "_001",
			Language: "de",
			Author:   "uhj",
			URL: "https://bibliothek.bahai.de/download/xml/export_UHJ_" +
				date[0:4] + "-" + date[4:6] + "-" + date[6:8] + "_de%20(1.01)%20MAS-XML.xml",
		})
	})
	return result
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
