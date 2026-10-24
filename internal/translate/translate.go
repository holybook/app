// Package translate implements the AI translation of passages that may quote
// the holy writings: quoted passages must use the authoritative published
// translation verbatim, everything else is translated by a language model.
package translate

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/holybook/app/internal/store"
)

// ParagraphWithID is a paragraph optionally tied to an authoritative
// translation by id ("<bookId>/<index>").
type ParagraphWithID struct {
	Text string `json:"text"`
	ID   string `json:"id,omitempty"`
}

// ParagraphWithReference is a paragraph to translate and, if it quotes the
// writings, the authoritative translation of the quoted paragraph.
type ParagraphWithReference struct {
	Text      string           `json:"text"`
	Reference *ParagraphWithID `json:"reference,omitempty"`
}

// ModelRequest is the input of a Translator.
type ModelRequest struct {
	FromLanguage string                   `json:"fromLanguage"`
	ToLanguage   string                   `json:"toLanguage"`
	Paragraphs   []ParagraphWithReference `json:"paragraphs"`
}

// ModelResponse is the output of a Translator: one paragraph per input
// paragraph; ID is set where the authoritative translation was used.
type ModelResponse struct {
	Paragraphs []ParagraphWithID `json:"paragraphs"`
}

// Translator translates a request, honoring authoritative references.
type Translator interface {
	Translate(ctx context.Context, req ModelRequest) (*ModelResponse, error)
}

// Annotation cites the authoritative source a translated paragraph came from.
type Annotation struct {
	BookID string `json:"bookId"`
	Index  int    `json:"index"`
	Number *int   `json:"number"`
	Title  string `json:"title"`
}

// AnnotatedParagraph is a translated paragraph with its optional citation.
type AnnotatedParagraph struct {
	Annotation *Annotation `json:"annotation"`
	Text       string      `json:"text"`
}

// Result is the result of translating a text.
type Result struct {
	Paragraphs []AnnotatedParagraph `json:"paragraphs"`
}

// VerbatimFinder looks up the authoritative translation of a passage.
type VerbatimFinder interface {
	Verbatim(ctx context.Context, fromLanguage, toLanguage, text string) (*store.VerbatimResult, error)
}

// Service orchestrates a translation: it splits the text into paragraphs,
// looks up an authoritative translation for each, lets the Translator produce
// the result and verifies that authoritative passages were kept verbatim.
type Service struct {
	Finder     VerbatimFinder
	Translator Translator
}

// ErrNotVerbatim is returned when the model altered an authoritative passage.
var ErrNotVerbatim = errors.New("translation does not match the authoritative translation")

// Translate translates text (one paragraph per non-blank line).
func (s *Service) Translate(ctx context.Context, fromLanguage, toLanguage, text string) (*Result, error) {
	var req = ModelRequest{FromLanguage: fromLanguage, ToLanguage: toLanguage}
	authoritative := map[string]*store.VerbatimResult{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		p := ParagraphWithReference{Text: line}
		v, err := s.Finder.Verbatim(ctx, fromLanguage, toLanguage, line)
		switch {
		case err == nil:
			id := referenceID(v)
			p.Reference = &ParagraphWithID{Text: v.TranslatedParagraph.Text, ID: id}
			authoritative[id] = v
		case !errors.Is(err, store.ErrNotFound):
			return nil, err
		}
		req.Paragraphs = append(req.Paragraphs, p)
	}
	if len(req.Paragraphs) == 0 {
		return &Result{Paragraphs: []AnnotatedParagraph{}}, nil
	}

	resp, err := s.Translator.Translate(ctx, req)
	if err != nil {
		return nil, err
	}

	result := &Result{Paragraphs: make([]AnnotatedParagraph, 0, len(resp.Paragraphs))}
	for _, p := range resp.Paragraphs {
		ap := AnnotatedParagraph{Text: p.Text}
		if v, ok := authoritative[p.ID]; ok {
			if !strings.Contains(strings.ToLower(v.TranslatedParagraph.Text), strings.ToLower(p.Text)) {
				return nil, fmt.Errorf("%w: paragraph %q (%s)", ErrNotVerbatim, p.Text, p.ID)
			}
			first := v.AllOriginalResults[0]
			ap.Annotation = &Annotation{
				BookID: first.BookID,
				Index:  v.TranslatedParagraph.Index,
				Number: v.TranslatedParagraph.Number,
				Title:  first.Title,
			}
		}
		result.Paragraphs = append(result.Paragraphs, ap)
	}
	return result, nil
}

func referenceID(v *store.VerbatimResult) string {
	return v.AllOriginalResults[0].BookID + "/" + strconv.Itoa(v.TranslatedParagraph.Index)
}
