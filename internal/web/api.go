package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/holybook/app/internal/book"
	"github.com/holybook/app/internal/store"
)

// The JSON API keeps the routes and response shapes of the original Ktor
// server, for any clients other than the website.

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) logError(r *http.Request, err error) {
	slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
}

func (s *Server) apiError(w http.ResponseWriter, r *http.Request, err error) {
	if r.Context().Err() != nil {
		return
	}
	s.logError(r, err)
	writeJSONError(w, http.StatusInternalServerError, "internal error")
}

func (s *Server) apiAuthors(w http.ResponseWriter, r *http.Request) {
	authors, err := s.store.Authors(r.Context())
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, authors)
}

func (s *Server) apiLanguages(w http.ResponseWriter, r *http.Request) {
	langs, err := s.store.Languages(r.Context())
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, langs)
}

// apiBooks returns the books of a language grouped by author display name.
func (s *Server) apiBooks(w http.ResponseWriter, r *http.Request) {
	books, err := s.store.Library(r.Context(), requestLanguage(r))
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	grouped := map[string][]store.Translation{}
	for _, b := range books {
		grouped[b.Author] = append(grouped[b.Author], b)
	}
	writeJSON(w, http.StatusOK, grouped)
}

func (s *Server) apiBook(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.Book(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "book not found")
		return
	}
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (s *Server) apiParagraphs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var bounds [2]*int
	for i, name := range []string{"start", "end"} {
		if v := q.Get(name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "invalid "+name)
				return
			}
			bounds[i] = &n
		}
	}
	ps, err := s.store.Paragraphs(r.Context(), r.PathValue("id"), requestLanguage(r), bounds[0], bounds[1])
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	if ps == nil {
		ps = []book.Paragraph{}
	}
	writeJSON(w, http.StatusOK, ps)
}

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeJSONError(w, http.StatusBadRequest, "missing q")
		return
	}
	page, err := s.store.Search(r.Context(), requestLanguage(r), query, 0, 0)
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, page.Results)
}

type translateRequest struct {
	FromLanguage string `json:"fromLanguage"`
	ToLanguage   string `json:"toLanguage"`
	Text         string `json:"text"`
}

func decodeTranslateRequest(w http.ResponseWriter, r *http.Request) (translateRequest, bool) {
	var req translateRequest
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if err != nil || req.FromLanguage == "" || req.ToLanguage == "" {
		writeJSONError(w, http.StatusBadRequest, "expected {fromLanguage, toLanguage, text}")
		return req, false
	}
	return req, true
}

func (s *Server) apiVerbatim(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeTranslateRequest(w, r)
	if !ok {
		return
	}
	res, err := s.store.Verbatim(r.Context(), req.FromLanguage, req.ToLanguage, req.Text)
	if errors.Is(err, store.ErrNotFound) {
		writeJSONError(w, http.StatusNotFound, "no translation found")
		return
	}
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) apiAITranslate(w http.ResponseWriter, r *http.Request) {
	if s.translator == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "AI translation is not configured")
		return
	}
	req, ok := decodeTranslateRequest(w, r)
	if !ok {
		return
	}
	res, err := s.aiTranslate(r.Context(), req.FromLanguage, req.ToLanguage, req.Text)
	if errors.Is(err, errBusy) {
		writeJSONError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if err != nil {
		s.apiError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
