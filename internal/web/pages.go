package web

import (
	"context"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
	"github.com/holybook/app/internal/translate"
)

const defaultLanguage = "en"

// pageBase is the data every page's layout needs.
type pageBase struct {
	Title       string
	Description string
	// Nav is the active primary navigation entry.
	Nav string
	// Lang is the language of the content shown.
	Lang string
	// Query prefills the search box.
	Query string
	// LangOptions are the entries of the language menu (none hides it).
	LangOptions []langOption
}

type langOption struct {
	Code, Name, URL string
	Active          bool
}

func requestLanguage(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l != "" {
		return l
	}
	return defaultLanguage
}

// base builds the layout data; languages are the options of the language menu,
// which switches the lang parameter of the current URL.
func (s *Server) base(r *http.Request, nav string, languages []string) pageBase {
	b := pageBase{Nav: nav, Lang: requestLanguage(r), Query: r.URL.Query().Get("q")}
	for _, l := range languages {
		b.LangOptions = append(b.LangOptions, langOption{
			Code: l, Name: languageName(l), Active: l == b.Lang,
			URL: withQuery(r.URL, "lang", l, "offset", ""),
		})
	}
	return b
}

func (b pageBase) ActiveLangName() string { return languageName(b.Lang) }

// --- library -----------------------------------------------------------------

type libraryPage struct {
	pageBase
	Filter       string
	TotalWorks   int
	TotalAuthors int
	MatchWorks   int
	Groups       []authorGroup
}

type authorGroup struct {
	Name  string
	Count int
	Open  bool
	// Years groups long lists of dated works (messages) by year; otherwise
	// Books lists the works directly.
	Years []yearGroup
	Books []bookItem
}

type yearGroup struct {
	Year  int
	Open  bool
	Books []bookItem
}

type bookItem struct {
	Title, URL, Date string
}

// yearGroupingThreshold is the size from which an author's dated works are
// grouped by year.
const yearGroupingThreshold = 12

func (s *Server) library(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	lang := requestLanguage(r)
	languages, err := s.store.Languages(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	books, err := s.store.Library(ctx, lang)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	filter := strings.TrimSpace(r.URL.Query().Get("filter"))

	p := libraryPage{pageBase: s.base(r, "library", languages), Filter: filter}
	p.Title = "Library"
	p.Description = "Browse the Bahá’í writings by author and title."
	p.TotalWorks = len(books)
	p.Groups, p.TotalAuthors = groupLibrary(books, filter, lang)
	for _, g := range p.Groups {
		p.MatchWorks += g.Count
	}
	s.renderPage(w, r, "library", "library-results", p)
}

// groupLibrary groups the books by author (keeping the store's order) and
// applies the filter: an author match keeps all of their works, otherwise only
// works whose title matches are kept.
func groupLibrary(books []store.Translation, filter, lang string) ([]authorGroup, int) {
	type authorBooks struct {
		name  string
		books []store.Translation
	}
	var authors []*authorBooks
	for _, b := range books {
		if len(authors) == 0 || authors[len(authors)-1].name != b.Author {
			authors = append(authors, &authorBooks{name: b.Author})
		}
		a := authors[len(authors)-1]
		a.books = append(a.books, b)
	}

	f := fold(filter)
	var groups []authorGroup
	for _, a := range authors {
		matching := a.books
		if f != "" && !strings.Contains(fold(a.name), f) {
			matching = nil
			for _, b := range a.books {
				if strings.Contains(fold(b.Title), f) {
					matching = append(matching, b)
				}
			}
		}
		if len(matching) > 0 {
			groups = append(groups, authorGroup{Name: a.name, Count: len(matching)})
			fillGroup(&groups[len(groups)-1], matching, f != "", lang)
		}
	}
	// Expand everything while filtering or when the catalog is small.
	expand := f != "" || len(groups) <= 8
	for i := range groups {
		groups[i].Open = expand
	}
	return groups, len(authors)
}

func fillGroup(g *authorGroup, books []store.Translation, filtering bool, lang string) {
	dated := len(books) >= yearGroupingThreshold
	for _, b := range books {
		dated = dated && !b.PublishedAt.IsZero()
	}
	for _, b := range books {
		item := bookItem{
			Title: b.Title,
			URL:   bookURL(b.BookID, lang),
			Date:  formatDate(b.PublishedAt, lang),
		}
		if !dated {
			g.Books = append(g.Books, item)
			continue
		}
		year := b.PublishedAt.Year()
		if len(g.Years) == 0 || g.Years[len(g.Years)-1].Year != year {
			// The newest year starts expanded.
			g.Years = append(g.Years, yearGroup{Year: year, Open: filtering || len(g.Years) == 0})
		}
		y := &g.Years[len(g.Years)-1]
		y.Books = append(y.Books, item)
	}
}

func bookURL(id, lang string) string {
	return "/books/" + url.PathEscape(id) + "?lang=" + url.QueryEscape(lang)
}

func paragraphURL(id, lang string, index int) string {
	return bookURL(id, lang) + "#p-" + strconv.Itoa(index)
}

// --- reader ------------------------------------------------------------------

type readerPage struct {
	pageBase
	BookTitle  string
	Author     string
	Paragraphs []paragraphView
	TOC        []tocEntry
}

type paragraphView struct {
	Index int
	Text  string
	Type  book.ParagraphType
	Label string
	// Level is the heading level (1..4) of section titles.
	Level   int
	Section string
}

func (p paragraphView) IsSectionTitle() bool { return p.Type == book.SectionTitle }

type tocEntry struct {
	Index   int
	Section string
	Text    string
	Level   int
}

func (s *Server) reader(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	b, err := s.store.Book(ctx, r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	// Fall back to English, then to any available language.
	lang := requestLanguage(r)
	t, ok := b.Translation(lang)
	if !ok {
		if t, ok = b.Translation(defaultLanguage); !ok {
			t = b.Translations[0]
		}
	}
	lang = t.Language

	paragraphs, err := s.store.Paragraphs(ctx, b.ID, lang, nil, nil)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	p := readerPage{pageBase: s.base(r, "library", b.Languages()), BookTitle: t.Title, Author: t.Author}
	p.Lang = lang
	for i := range p.LangOptions {
		p.LangOptions[i].Active = p.LangOptions[i].Code == lang
	}
	p.Title = t.Title
	p.Description = t.Title + " by " + t.Author + "."
	p.Paragraphs = make([]paragraphView, len(paragraphs))
	for i, para := range paragraphs {
		v := paragraphView{Index: para.Index, Text: para.Text, Type: para.Type, Label: para.Label(), Section: para.SectionPath}
		if para.Type == book.SectionTitle {
			v.Level = min(max(para.SectionDepth(), 1), 4)
			p.TOC = append(p.TOC, tocEntry{Index: para.Index, Section: para.SectionPath, Text: para.Text, Level: v.Level})
		}
		p.Paragraphs[i] = v
	}
	s.render(w, r, http.StatusOK, "reader", "layout.html", p)
}

// --- search ------------------------------------------------------------------

const searchPageSize = 20

type searchPage struct {
	pageBase
	Total   int
	Results []searchResultView
	NextURL string
}

type searchResultView struct {
	URL    string
	HTML   template.HTML
	Author string
	Title  string
	Label  string
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	languages, err := s.store.Languages(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	p := searchPage{pageBase: s.base(r, "", languages)}
	p.Query = strings.TrimSpace(p.Query)
	p.Title = "Search"
	if p.Query != "" {
		p.Title = "“" + p.Query + "” – Search"
	}

	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	offset = max(offset, 0)
	if p.Query != "" {
		res, err := s.store.Search(ctx, p.Lang, p.Query, searchPageSize, offset)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		p.Total = res.Total
		for _, sr := range res.Results {
			p.Results = append(p.Results, searchResultView{
				URL:    paragraphURL(sr.BookID, p.Lang, sr.Paragraph.Index),
				HTML:   template.HTML(sr.HighlightedText), // escaped by the store
				Author: sr.Author,
				Title:  sr.Title,
				Label:  sr.Paragraph.Label(),
			})
		}
		if next := offset + len(res.Results); len(res.Results) > 0 && next < res.Total {
			p.NextURL = withQuery(r.URL, "offset", strconv.Itoa(next))
		}
	}
	if r.URL.Query().Has("offset") && isHTMX(r) {
		s.render(w, r, http.StatusOK, "search", "search-results", p)
		return
	}
	s.render(w, r, http.StatusOK, "search", "layout.html", p)
}

// --- verbatim translation ----------------------------------------------------

type translateForm struct {
	Languages []string
	From, To  string
	Text      string
}

func (s *Server) translateForm(ctx context.Context, r *http.Request) (translateForm, error) {
	languages, err := s.store.Languages(ctx)
	if err != nil {
		return translateForm{}, err
	}
	f := translateForm{Languages: languages, From: "en", To: "de"}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(nil, r.Body, 64<<10)
		if err := r.ParseForm(); err != nil {
			return f, err
		}
		f.Text = r.PostForm.Get("text")
		if l := r.PostForm.Get("from"); slices.Contains(languages, l) {
			f.From = l
		}
		if l := r.PostForm.Get("to"); slices.Contains(languages, l) {
			f.To = l
		}
	}
	return f, nil
}

type verbatimPage struct {
	pageBase
	Form   translateForm
	Result *verbatimResult
}

type verbatimResult struct {
	Text     string
	Lang     string
	NotFound bool
	CiteURL  string
	Cite     string
}

func (s *Server) verbatim(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	form, err := s.translateForm(ctx, r)
	if err != nil {
		s.badForm(w, r, err)
		return
	}
	p := verbatimPage{pageBase: s.base(r, "translate", nil), Form: form}
	p.Title = "Verbatim translation"
	p.Description = "Find the authorized published rendering of a passage in another language."

	if text := strings.TrimSpace(form.Text); text != "" {
		res := &verbatimResult{Lang: form.To}
		v, err := s.store.Verbatim(ctx, form.From, form.To, text)
		switch {
		case errors.Is(err, store.ErrNotFound):
			res.NotFound = true
		case err != nil:
			s.serverError(w, r, err)
			return
		default:
			res.Text = v.TranslatedParagraph.Text
			res.CiteURL = paragraphURL(v.AllOriginalResults[0].BookID, form.To, v.TranslatedParagraph.Index)
			res.Cite = s.citation(ctx, v.AllOriginalResults[0], form.To, v.TranslatedParagraph)
		}
		p.Result = res
	}
	s.renderPage(w, r, "translate", "verbatim-result", p)
}

// citation formats "Author, Title, par. N" for a paragraph, preferring the
// title of the book in the target language.
func (s *Server) citation(ctx context.Context, original store.SearchResult, lang string, para book.Paragraph) string {
	author, title := original.Author, original.Title
	if b, err := s.store.Book(ctx, original.BookID); err == nil {
		if t, ok := b.Translation(lang); ok {
			author, title = t.Author, t.Title
		}
	}
	cite := author + ", " + title
	if l := para.Label(); l != "" {
		cite += ", par. " + l
	}
	return cite
}

// bookTitle returns the title of a book in lang, or fallback.
func (s *Server) bookTitle(ctx context.Context, bookID, lang, fallback string) string {
	if b, err := s.store.Book(ctx, bookID); err == nil {
		if t, ok := b.Translation(lang); ok {
			return t.Title
		}
	}
	return fallback
}

func (s *Server) badForm(w http.ResponseWriter, r *http.Request, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		s.renderError(w, r, http.StatusRequestEntityTooLarge, "Text too long", "Please submit a shorter passage.")
		return
	}
	s.renderError(w, r, http.StatusBadRequest, "Invalid request", "The form could not be read.")
}

// --- AI translation ----------------------------------------------------------

type aiPage struct {
	pageBase
	Form      translateForm
	Available bool
	Submitted bool
	Error     string
	Result    []aiParagraph
}

type aiParagraph struct {
	Text    string
	CiteURL string
	Cite    string
}

func (s *Server) aiPage(w http.ResponseWriter, r *http.Request) {
	form, err := s.translateForm(r.Context(), r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderPage(w, r, "aitranslate", "", s.newAIPage(r, form))
}

func (s *Server) newAIPage(r *http.Request, form translateForm) aiPage {
	p := aiPage{pageBase: s.base(r, "aitranslate", nil), Form: form, Available: s.translator != nil}
	p.Title = "AI translation"
	p.Description = "Translate a passage, quoting the authorized translations of the writings verbatim."
	return p
}

func (s *Server) aiSubmit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	form, err := s.translateForm(ctx, r)
	if err != nil {
		s.badForm(w, r, err)
		return
	}
	p := s.newAIPage(r, form)
	if strings.TrimSpace(form.Text) != "" && p.Available {
		p.Submitted = true
		res, err := s.aiTranslate(ctx, form.From, form.To, form.Text)
		switch {
		case errors.Is(err, errBusy):
			p.Error = "Too many translations are running right now. Please try again in a moment."
		case errors.Is(err, translate.ErrNotVerbatim):
			p.Error = "The model did not reproduce an authorized translation faithfully, so the result was discarded. Please try again."
		case errors.Is(err, context.DeadlineExceeded):
			p.Error = "The translation took too long. Please try a shorter passage."
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			s.logError(r, err)
			p.Error = "The translation failed. Please try again."
		default:
			for _, para := range res.Paragraphs {
				ap := aiParagraph{Text: para.Text}
				if a := para.Annotation; a != nil {
					ap.CiteURL = paragraphURL(a.BookID, form.To, a.Index)
					ap.Cite = s.bookTitle(ctx, a.BookID, form.To, a.Title)
					if a.Number != nil {
						ap.Cite += ", par. " + strconv.Itoa(*a.Number)
					}
				}
				p.Result = append(p.Result, ap)
			}
		}
	}
	s.renderPage(w, r, "aitranslate", "ai-result", p)
}

var errBusy = errors.New("too many concurrent translations")

// aiTranslate runs a translation with bounded concurrency and a deadline.
func (s *Server) aiTranslate(ctx context.Context, from, to, text string) (*translate.Result, error) {
	select {
	case s.aiSlots <- struct{}{}:
		defer func() { <-s.aiSlots }()
	case <-time.After(10 * time.Second):
		return nil, errBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return s.translator.Translate(ctx, from, to, text)
}
