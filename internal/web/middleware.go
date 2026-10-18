package web

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Logging logs every request and recovers from panics.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				slog.Error("panic", "path", r.URL.Path, "panic", p, "stack", string(debug.Stack()))
				http.Error(rec, "internal server error", http.StatusInternalServerError)
			}
			level := slog.LevelInfo
			if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/healthz" {
				level = slog.LevelDebug
			}
			slog.Log(r.Context(), level, "request", "method", r.Method, "path", r.URL.RequestURI(),
				"status", rec.status, "duration", time.Since(start).Round(time.Microsecond),
				"htmx", r.Header.Get("HX-Request") == "true")
		}()
		next.ServeHTTP(rec, r)
	})
}
