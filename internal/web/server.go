// Package web serves the holybook website (server-rendered HTML enhanced with
// htmx) and the JSON API.
package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/holybook/go/internal/book"
	"github.com/holybook/go/internal/store"
	"github.com/holybook/go/internal/translate"
)

//go:embed templates static
var embedded embed.FS

// Store is the data access the server needs (implemented by *store.Store).
type Store interface {
	Ping(ctx context.Context) error
	Languages(ctx context.Context) ([]string, error)
	Authors(ctx context.Context) ([]store.Author, error)
	Library(ctx context.Context, language string) ([]store.Translation, error)
	Book(ctx context.Context, id string) (*store.Book, error)
	Paragraphs(ctx context.Context, bookID, language string, start, end *int) ([]book.Paragraph, error)
	Search(ctx context.Context, language, query string, limit, offset int) (store.SearchPage, error)
	Verbatim(ctx context.Context, fromLanguage, toLanguage, text string) (*store.VerbatimResult, error)
}

// Translator performs AI translations (implemented by *translate.Service).
type Translator interface {
	Translate(ctx context.Context, fromLanguage, toLanguage, text string) (*translate.Result, error)
}

// Options configure a Server.
type Options struct {
	Store Store
	// Translator is nil when no Gemini API key is configured; AI translation
	// is then reported as unavailable.
	Translator Translator
	// DevDir, if set, is the directory of this package on disk: templates and
	// static files are then read from there on every request, so edits show up
	// without a rebuild.
	DevDir string
	// MCP, if set, serves the Model Context Protocol endpoint at /mcp.
	MCP http.Handler
}

// Server is the HTTP handler of the website.
type Server struct {
	store      Store
	translator Translator
	mcp        http.Handler
	files      fs.FS
	dev        bool

	mu        sync.Mutex // serializes template reloads in dev mode
	templates map[string]*template.Template

	// aiSlots bounds concurrent AI translations, which are slow and costly.
	aiSlots chan struct{}
	mux     *http.ServeMux
}

// New creates a Server.
func New(opts Options) (*Server, error) {
	s := &Server{
		store:      opts.Store,
		translator: opts.Translator,
		mcp:        opts.MCP,
		files:      embedded,
		aiSlots:    make(chan struct{}, 4),
	}
	if opts.DevDir != "" {
		s.files = os.DirFS(opts.DevDir)
		s.dev = true
	}
	if _, err := s.loadTemplates(); err != nil {
		return nil, err
	}
	s.routes()
	return s, nil
}

func (s *Server) routes() {
	m := http.NewServeMux()
	m.HandleFunc("GET /{$}", s.library)
	m.HandleFunc("GET /books/{id}", s.reader)
	m.HandleFunc("GET /search", s.search)
	m.HandleFunc("GET /translate", s.verbatim)
	m.HandleFunc("POST /translate", s.verbatim)
	m.HandleFunc("GET /aitranslate", s.aiPage)
	m.HandleFunc("POST /aitranslate", s.aiSubmit)

	m.HandleFunc("GET /api/authors", s.apiAuthors)
	m.HandleFunc("GET /api/languages", s.apiLanguages)
	m.HandleFunc("GET /api/books", s.apiBooks)
	m.HandleFunc("GET /api/books/{id}", s.apiBook)
	m.HandleFunc("GET /api/books/{id}/paragraphs", s.apiParagraphs)
	m.HandleFunc("GET /api/search", s.apiSearch)
	m.HandleFunc("POST /api/translate", s.apiVerbatim)
	m.HandleFunc("POST /api/aitranslate", s.apiAITranslate)
	m.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeJSONError(w, http.StatusNotFound, "not found")
	})
	if s.mcp != nil {
		m.Handle("/mcp", s.mcp)
	}

	m.HandleFunc("GET /static/", s.static)
	m.HandleFunc("GET /favicon.ico", s.rootFile("favicon.svg"))
	m.HandleFunc("GET /robots.txt", s.rootFile("robots.txt"))
	m.HandleFunc("GET /manifest.json", s.rootFile("manifest.json"))
	m.HandleFunc("GET /healthz", s.health)
	m.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.notFound(w, r)
	})
	s.mux = m
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	s.mux.ServeHTTP(w, r)
}

// No inline scripts are used; htmx injects its indicator styles inline.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' https://fonts.gstatic.com; img-src 'self' data:; " +
	"connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// --- templates ---------------------------------------------------------------

// pages are rendered from templates/layout.html plus templates/<page>.html.
var pages = []string{"library", "reader", "search", "translate", "aitranslate", "error"}

func (s *Server) loadTemplates() (map[string]*template.Template, error) {
	if !s.dev && s.templates != nil {
		return s.templates, nil // parsed once in New
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	hashes, err := hashStatic(s.files)
	if err != nil {
		return nil, err
	}
	funcs := templateFuncs(func(name string) string {
		return "/static/" + name + "?v=" + hashes[name]
	})
	base, err := template.New("layout.html").Funcs(funcs).ParseFS(s.files, "templates/layout.html", "templates/partials.html")
	if err != nil {
		return nil, err
	}
	ts := map[string]*template.Template{}
	for _, p := range pages {
		t, err := template.Must(base.Clone()).ParseFS(s.files, "templates/"+p+".html")
		if err != nil {
			return nil, err
		}
		ts[p] = t
	}
	if !s.dev {
		s.templates = ts
	}
	return ts, nil
}

// render executes the named template of a page (the whole document for
// "layout.html", or a fragment for htmx requests) and writes it.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, page, name string, data any) {
	ts, err := s.loadTemplates()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var buf bytes.Buffer
	if err := ts[page].ExecuteTemplate(&buf, name, data); err != nil {
		s.serverError(w, r, fmt.Errorf("rendering %s/%s: %w", page, name, err))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// renderPage renders a full page, or only the fragment template when the
// request comes from htmx and targets it.
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, page, fragment string, data any) {
	if fragment != "" && isHTMX(r) {
		s.render(w, r, http.StatusOK, page, fragment, data)
		return
	}
	s.render(w, r, http.StatusOK, page, "layout.html", data)
}

// isHTMX reports whether the request is an htmx request that expects a
// fragment (history restoration and boosted requests need the full page).
func isHTMX(r *http.Request) bool {
	h := r.Header
	return h.Get("HX-Request") == "true" && h.Get("HX-Boosted") != "true" &&
		h.Get("HX-History-Restore-Request") != "true"
}

// --- errors ------------------------------------------------------------------

type errorPage struct {
	pageBase
	Status  int
	Heading string
	Message string
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.renderError(w, r, http.StatusNotFound, "Page not found",
		"The page you are looking for does not exist or has moved.")
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return // the client went away
	}
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	s.renderError(w, r, http.StatusInternalServerError, "Something went wrong",
		"An unexpected error occurred. Please try again in a moment.")
}

func (s *Server) renderError(w http.ResponseWriter, r *http.Request, status int, heading, message string) {
	data := errorPage{pageBase: s.base(r, "", nil), Status: status, Heading: heading, Message: message}
	ts, err := s.loadTemplates()
	var buf bytes.Buffer
	if err == nil {
		err = ts["error"].ExecuteTemplate(&buf, "layout.html", data)
	}
	if err != nil {
		slog.Error("rendering error page", "err", err)
		http.Error(w, heading, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write(buf.Bytes())
}

// --- static files ------------------------------------------------------------

func hashStatic(files fs.FS) (map[string]string, error) {
	hashes := map[string]string{}
	err := fs.WalkDir(files, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		hashes[strings.TrimPrefix(p, "static/")] = hex.EncodeToString(sum[:])[:10]
		return nil
	})
	return hashes, err
}

func (s *Server) static(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/static/")
	if r.URL.Query().Get("v") != "" && !s.dev {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	s.serveFile(w, r, name)
}

func (s *Server) rootFile(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		s.serveFile(w, r, name)
	}
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, s.files, "static/"+name)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}
