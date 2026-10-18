// Package book holds the content model shared by every holybook component:
// paragraphs with their section hierarchy, whole books, and the XML format the
// books are stored in (the holybook/data repository).
package book

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParagraphType classifies a paragraph. The string values are persisted in the
// database and in the XML content files, so they must not change.
type ParagraphType string

const (
	Body         ParagraphType = "body"
	SectionTitle ParagraphType = "section-title"
	Header       ParagraphType = "header"
	Title        ParagraphType = "title"
	LetterHead   ParagraphType = "letter-head"
	Date         ParagraphType = "date"
	Addressee    ParagraphType = "addressee"
	Salutation   ParagraphType = "salutation"
	Separator    ParagraphType = "separator"
	Signature    ParagraphType = "signature"
)

var paragraphTypes = map[ParagraphType]bool{
	Body: true, SectionTitle: true, Header: true, Title: true, LetterHead: true,
	Date: true, Addressee: true, Salutation: true, Separator: true, Signature: true,
}

// ParseParagraphType validates a persisted paragraph type value.
func ParseParagraphType(s string) (ParagraphType, error) {
	t := ParagraphType(s)
	if !paragraphTypes[t] {
		return "", fmt.Errorf("unknown paragraph type %q", s)
	}
	return t, nil
}

func (t *ParagraphType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	parsed, err := ParseParagraphType(s)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// Paragraph is one entry of a book's flat, indexed paragraph list.
//
// Index is the position in the book and is identical across the language
// versions of a book, which is what lets the verbatim translation look up the
// paragraph at the same index in another language.
type Paragraph struct {
	Index int           `json:"index"`
	Text  string        `json:"text"`
	Type  ParagraphType `json:"type"`
	// Number is the running number of a body paragraph within its section and
	// nil for every other type.
	Number *int `json:"number"`
	// SectionPath is the dotted path of the section the paragraph belongs to
	// (e.g. "1", "1.1"); empty for top-level paragraphs of flat books. For a
	// SectionTitle paragraph it is the path of the section it opens.
	SectionPath string `json:"sectionPath"`
}

// Label is the reference shown next to a body paragraph: "1.1:5" inside a
// section, or just "5" in a flat book. Empty for unnumbered paragraphs.
func (p Paragraph) Label() string {
	if p.Number == nil {
		return ""
	}
	if p.SectionPath == "" {
		return strconv.Itoa(*p.Number)
	}
	return p.SectionPath + ":" + strconv.Itoa(*p.Number)
}

// SectionDepth is the nesting depth of the paragraph's section: "" -> 0,
// "1" -> 1, "1.1" -> 2.
func (p Paragraph) SectionDepth() int {
	if p.SectionPath == "" {
		return 0
	}
	return strings.Count(p.SectionPath, ".") + 1
}

// Builder builds a flat, indexed paragraph list while tracking an arbitrarily
// deep section hierarchy.
//
// Sections are numbered per parent ("1", "2", then "1.1", "1.2", ...). Body
// paragraph numbers restart at 1 within each section, so the rendered label is
// "sectionPath:number" (e.g. "1.1:5"). Top-level paragraphs have an empty
// section path and a plain running number.
type Builder struct {
	paragraphs []Paragraph
	stack      []*frame
	// Source heading level of each open section, parallel to stack (excluding
	// the root frame). Used by Heading to turn a stream of leveled headings
	// (e.g. <h2>/<h3>) into proper nesting.
	headingLevels []int
}

type frame struct {
	path       string
	childCount int
	bodyCount  int
}

func NewBuilder() *Builder {
	return &Builder{stack: []*frame{{}}}
}

func (b *Builder) current() *frame { return b.stack[len(b.stack)-1] }

// EnterSection opens a new (sub)section and emits a SectionTitle paragraph for
// its title.
func (b *Builder) EnterSection(title string) {
	parent := b.current()
	parent.childCount++
	path := strconv.Itoa(parent.childCount)
	if parent.path != "" {
		path = parent.path + "." + path
	}
	b.stack = append(b.stack, &frame{path: path})
	b.paragraphs = append(b.paragraphs, Paragraph{
		Index: len(b.paragraphs), Text: title, Type: SectionTitle, SectionPath: path,
	})
}

// ExitSection closes the current section, returning to its parent.
func (b *Builder) ExitSection() {
	if len(b.stack) <= 1 {
		panic("book: ExitSection without matching EnterSection")
	}
	b.stack = b.stack[:len(b.stack)-1]
}

// Heading opens a section for a heading at the given source level (e.g. 2 for
// <h2>), first closing any open sections at the same or a deeper level so the
// nesting mirrors the heading hierarchy. Do not mix with manual
// EnterSection/ExitSection in the same build.
func (b *Builder) Heading(level int, title string) {
	for n := len(b.headingLevels); n > 0 && b.headingLevels[n-1] >= level; n-- {
		b.ExitSection()
		b.headingLevels = b.headingLevels[:n-1]
	}
	b.EnterSection(title)
	b.headingLevels = append(b.headingLevels, level)
}

// Add appends a paragraph to the current section. Body paragraphs are numbered.
func (b *Builder) Add(text string, typ ParagraphType) {
	p := Paragraph{Index: len(b.paragraphs), Text: text, Type: typ, SectionPath: b.current().path}
	if typ == Body {
		f := b.current()
		f.bodyCount++
		n := f.bodyCount
		p.Number = &n
	}
	b.paragraphs = append(b.paragraphs, p)
}

func (b *Builder) Paragraphs() []Paragraph { return b.paragraphs }
