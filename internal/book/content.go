package book

import (
	"bufio"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// DateLayout is the ISO date format used for publication dates everywhere.
const DateLayout = "2006-01-02"

// Content is one language version of a book with all of its paragraphs.
type Content struct {
	ID       string
	Language string
	Title    string
	// Author is the stable author code (e.g. "bahaullah"), not a display name.
	Author string
	// PublishedAt is the publication date; the zero value means unknown.
	PublishedAt time.Time
	Paragraphs  []Paragraph
}

// ReadXML parses a book in the holybook XML format:
//
//	<book id=".." language=".." title=".." author=".." publishedAt="YYYY-MM-DD">
//	  <p type="body">..</p>
//	  <section title=".."><p>..</p><section title="..">..</section></section>
//	</book>
//
// Flat books (only top-level <p>) and nested sections share one code path.
func ReadXML(r io.Reader) (*Content, error) {
	d := xml.NewDecoder(r)
	root, err := nextStart(d)
	if err != nil {
		return nil, fmt.Errorf("reading book element: %w", err)
	}
	if root.Name.Local != "book" {
		return nil, fmt.Errorf("unexpected root element <%s>", root.Name.Local)
	}
	c := &Content{
		ID:       attr(root, "id"),
		Language: attr(root, "language"),
		Title:    attr(root, "title"),
		Author:   attr(root, "author"),
	}
	if s := attr(root, "publishedAt"); s != "" {
		if c.PublishedAt, err = time.Parse(DateLayout, s); err != nil {
			return nil, fmt.Errorf("book %s: invalid publishedAt: %w", c.ID, err)
		}
	}

	b := NewBuilder()
	if err := readChildren(d, b); err != nil {
		return nil, fmt.Errorf("book %s: %w", c.ID, err)
	}
	c.Paragraphs = b.Paragraphs()
	return c, nil
}

// readChildren consumes the children of the current element up to and
// including its end tag, adding a paragraph per <p> and recursing into each
// <section>. Other elements are skipped.
func readChildren(d *xml.Decoder, b *Builder) error {
	for {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.EndElement:
			return nil
		case xml.StartElement:
			switch t.Name.Local {
			case "section":
				b.EnterSection(attr(t, "title"))
				if err := readChildren(d, b); err != nil {
					return err
				}
				b.ExitSection()
			case "p":
				text, err := textContent(d)
				if err != nil {
					return err
				}
				typ, err := ParseParagraphType(attr(t, "type"))
				if err != nil {
					typ = Body
				}
				b.Add(text, typ)
			default:
				if err := d.Skip(); err != nil {
					return err
				}
			}
		}
	}
}

// textContent returns all character data inside the current element (like the
// DOM textContent) and consumes its end tag.
func textContent(d *xml.Decoder) (string, error) {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		}
	}
	return sb.String(), nil
}

func nextStart(d *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return xml.StartElement{}, err
		}
		if s, ok := tok.(xml.StartElement); ok {
			return s, nil
		}
	}
}

func attr(e xml.StartElement, name string) string {
	for _, a := range e.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// WriteXML serializes the book in the holybook XML format, reconstructing the
// <section> nesting from the flat paragraph list. The output is byte-for-byte
// what the original (Java) tooling produced for the holybook/data repository:
// sorted attributes, four-space indentation.
func (c *Content) WriteXML(w io.Writer) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="no"?>` + "\n")

	attrs := map[string]string{"id": c.ID, "language": c.Language, "title": c.Title, "author": c.Author}
	if !c.PublishedAt.IsZero() {
		attrs["publishedAt"] = c.PublishedAt.Format(DateLayout)
	}
	root := &xmlNode{name: "book", attrs: attrs}

	// A SectionTitle paragraph opens a <section>; every other paragraph is
	// appended to the currently open section.
	type open struct {
		path string
		node *xmlNode
	}
	stack := []open{{"", root}}
	parentFor := func(path string) {
		for len(stack) > 1 && stack[len(stack)-1].path != path {
			stack = stack[:len(stack)-1]
		}
	}
	for _, p := range c.Paragraphs {
		if p.Type == SectionTitle {
			parentPath := ""
			if i := strings.LastIndexByte(p.SectionPath, '.'); i >= 0 {
				parentPath = p.SectionPath[:i]
			}
			parentFor(parentPath)
			n := &xmlNode{name: "section", attrs: map[string]string{"title": p.Text}}
			stack[len(stack)-1].node.children = append(stack[len(stack)-1].node.children, n)
			stack = append(stack, open{p.SectionPath, n})
			continue
		}
		parentFor(p.SectionPath)
		n := &xmlNode{name: "p", attrs: map[string]string{"type": string(p.Type)}, text: p.Text}
		stack[len(stack)-1].node.children = append(stack[len(stack)-1].node.children, n)
	}

	root.write(bw, 0)
	return bw.Flush()
}

type xmlNode struct {
	name     string
	attrs    map[string]string
	text     string
	children []*xmlNode
}

func (n *xmlNode) write(w *bufio.Writer, depth int) {
	indent := strings.Repeat("    ", depth)
	w.WriteString(indent + "<" + n.name)
	keys := make([]string, 0, len(n.attrs))
	for k := range n.attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w.WriteString(" " + k + `="` + attrEscaper.Replace(n.attrs[k]) + `"`)
	}
	switch {
	case len(n.children) > 0:
		w.WriteString(">\n")
		for _, c := range n.children {
			c.write(w, depth+1)
		}
		w.WriteString(indent + "</" + n.name + ">\n")
	case n.text != "":
		w.WriteString(">" + textEscaper.Replace(n.text) + "</" + n.name + ">\n")
	default:
		w.WriteString("/>\n")
	}
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;",
		"\n", "&#10;", "\r", "&#13;", "\t", "&#9;")
)
