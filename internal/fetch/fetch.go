// Package fetch retrieves book indices and book content from the source
// websites (bahai.org reference library, bibliothek.bahai.de) and converts
// them into the holybook content format.
package fetch

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
)

// Client is the HTTP client used for all fetching.
var Client = &http.Client{Timeout: 2 * time.Minute}

const userAgent = "holybook-fetch/1.0 (+https://holybook.app)"

// get downloads a URL, failing on non-200 responses.
func get(ctx context.Context, rawURL string) (body []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := Client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("GET %s: status %s", rawURL, resp.Status)
	}
	body, err = io.ReadAll(resp.Body)
	return body, resp.Header.Get("Content-Type"), err
}

func parseHTML(body []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(strings.NewReader(string(body)))
}

// normalizeSpace collapses whitespace runs into single spaces and trims, like
// the text extraction of browsers and jsoup.
func normalizeSpace(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}

// text is the normalized text of a selection. Like jsoup (which produced the
// existing content), a <br> or a block element boundary separates words.
func text(s *goquery.Selection) string {
	var sb strings.Builder
	for _, n := range s.Nodes {
		appendText(&sb, n)
	}
	return normalizeSpace(sb.String())
}

func appendText(sb *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		sb.WriteString(n.Data)
		return
	case html.ElementNode:
		if n.Data == "br" || blockElements[n.Data] {
			sb.WriteByte(' ')
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		appendText(sb, c)
	}
	if n.Type == html.ElementNode && blockElements[n.Data] {
		sb.WriteByte(' ')
	}
}

var blockElements = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "dd": true, "div": true,
	"dl": true, "dt": true, "fieldset": true, "figcaption": true, "figure": true, "footer": true,
	"form": true, "h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"header": true, "hr": true, "li": true, "main": true, "nav": true, "ol": true, "p": true,
	"pre": true, "section": true, "table": true, "tbody": true, "td": true, "tfoot": true,
	"th": true, "thead": true, "tr": true, "ul": true,
}

// ownText is the normalized text of the element's direct text children only.
func ownText(s *goquery.Selection) string {
	var sb strings.Builder
	s.Contents().Each(func(_ int, c *goquery.Selection) {
		if goquery.NodeName(c) == "#text" {
			sb.WriteString(c.Text())
		}
	})
	return normalizeSpace(sb.String())
}

func className(s *goquery.Selection) string {
	return strings.TrimSpace(s.AttrOr("class", ""))
}

// xmlContentTypes are the content types the content parsers accept.
var xmlContentTypes = []string{"application/xml", "application/xhtml+xml", "text/html"}

// matcher decides whether a parser handles a response, by content type and
// host name suffix.
type matcher struct {
	contentTypes []string
	host         string
}

func (m matcher) matches(contentType string, u *url.URL) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	for _, ct := range m.contentTypes {
		if strings.EqualFold(mediaType, ct) {
			return strings.HasSuffix(u.Hostname(), m.host)
		}
	}
	return false
}
