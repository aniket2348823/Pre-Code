// Package slogger provides a structured logging middleware for HTTP requests.
package slogger

import (
	"log/slog"
	"net/http"
	"time"
)

// StatusWriter captures the response status code.
type StatusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

// WriteHeader captures the status code.
func (w *StatusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Write captures bytes written.
func (w *StatusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// Middleware logs each HTTP request with structured fields.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &StatusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r)

		duration := time.Since(start)
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"bytes", sw.bytes,
			"duration_ms", duration.Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// Recovery is middleware that recovers from panics and logs them.
func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered",
					"error", rec,
					"method", r.Method,
					"path", r.URL.Path,
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
