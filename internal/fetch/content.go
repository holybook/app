package fetch

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/holybook/app/internal/book"
)

// parsedBook is what a content parser extracts from a source document.
type parsedBook struct {
	title      string
	paragraphs []book.Paragraph
}

type rule struct {
	matcher matcher
	parse   func(*goquery.Document) parsedBook
}

var rules = []rule{
	{matcher{xmlContentTypes, "bahai.org"}, parseReferenceLibrary},
	{matcher{xmlContentTypes, "bibliothek.bahai.de"}, parseBibliothek},
}

// parseContent picks the parser for a response and runs it.
func parseContent(contentType string, u *url.URL, body []byte) (*parsedBook, error) {
	for _, r := range rules {
		if r.matcher.matches(contentType, u) {
			doc, err := parseHTML(body)
			if err != nil {
				return nil, err
			}
			pb := r.parse(doc)
			return &pb, nil
		}
	}
	return nil, fmt.Errorf("no parser for content type %q and url %s", contentType, u)
}

// FetchContent downloads and parses the book a descriptor points to.
func FetchContent(ctx context.Context, d book.Descriptor) (*book.Content, error) {
	slog.Info("fetching content", "url", d.URL)
	u, err := url.Parse(d.URL)
	if err != nil {
		return nil, err
	}
	body, contentType, err := get(ctx, d.URL)
	if err != nil {
		return nil, err
	}
	pb, err := parseContent(contentType, u, body)
	if err != nil {
		return nil, err
	}
	return &book.Content{
		ID:          d.ID,
		Language:    d.Language,
		Title:       pb.title,
		Author:      d.Author,
		PublishedAt: d.PublishedAt(),
		Paragraphs:  pb.paragraphs,
	}, nil
}

// FetchAll fetches every descriptor whose file does not exist yet in
// contentDir. Failures are logged and skipped.
func FetchAll(ctx context.Context, descriptors []book.Descriptor, contentDir string) {
	for _, d := range descriptors {
		if ctx.Err() != nil {
			return
		}
		path := d.FilePath(contentDir)
		if _, err := os.Stat(path); err == nil {
			slog.Info("file already exists", "file", filepath.Base(path))
			continue
		}
		c, err := FetchContent(ctx, d)
		if err == nil {
			err = writeContent(path, c)
		}
		if err != nil {
			slog.Warn("failed to process", "url", d.URL, "err", err)
		}
	}
}

func writeContent(path string, c *book.Content) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = c.WriteXML(f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// parseReferenceLibrary parses a bahai.org reference library xhtml. Heading
// tags (<h2>..<h6>) carry the section nesting of structured works; <h1> is the
// work title and is dropped. A header-class paragraph (used by some letters) is
// treated as the deepest heading. Works without any of these stay flat.
func parseReferenceLibrary(doc *goquery.Document) parsedBook {
	b := book.NewBuilder()
	doc.Find("div.b").First().Find("h1, h2, h3, h4, h5, h6, p").Each(func(_ int, el *goquery.Selection) {
		// Skip table-of-contents entries, which the source renders inside <li>.
		if el.ParentsFiltered("li").Length() > 0 {
			return
		}
		t := text(el)
		if t == "" {
			return
		}
		tag := goquery.NodeName(el)
		switch {
		case tag == "h1":
		case len(tag) == 2 && tag[0] == 'h':
			b.Heading(int(tag[1]-'0'), t)
		default:
			typ := referenceLibraryType(className(el))
			if typ == book.Header {
				b.Heading(7, t)
			} else {
				b.Add(t, typ)
			}
		}
	})
	return parsedBook{title: text(doc.Find("head title").First()), paragraphs: b.Paragraphs()}
}

func referenceLibraryType(class string) book.ParagraphType {
	switch class {
	case "c xb", "c yb", "c zb", "c ab":
		return book.LetterHead
	case "cc", "wb":
		return book.Date
	case "ub":
		return book.Addressee
	case "tb":
		return book.Salutation
	case "c cd hb":
		return book.Header
	case "sb vb":
		return book.Separator
	case "pb":
		return book.Signature
	default:
		return book.Body
	}
}

// parseBibliothek parses a bibliothek.bahai.de MAS-XML export.
func parseBibliothek(doc *goquery.Document) parsedBook {
	b := book.NewBuilder()
	doc.Find("par").Each(func(_ int, el *goquery.Selection) {
		b.Add(ownText(el), bibliothekType(className(el)))
	})
	var titles []string
	doc.Find("metadata titlename").Each(func(_ int, el *goquery.Selection) {
		if t := text(el); t != "" {
			titles = append(titles, t)
		}
	})
	return parsedBook{title: strings.Join(titles, " "), paragraphs: b.Paragraphs()}
}

func bibliothekType(class string) book.ParagraphType {
	switch class {
	case "letter sender uhj":
		return book.LetterHead
	case "letter date":
		return book.Date
	case "letter addressee":
		return book.Addressee
	case "letter salutation":
		return book.Salutation
	case "letter signed":
		return book.Signature
	default:
		return book.Body
	}
}
