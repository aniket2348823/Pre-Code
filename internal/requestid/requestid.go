// Package requestid provides middleware for generating and propagating
// unique request identifiers across HTTP handlers.
package requestid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type contextKey string

const key contextKey = "request_id"

// Middleware generates a unique request ID and attaches it to the context
// and response headers. If the incoming request has an X-Request-Id header,
// it is reused.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = Generate()
		}
		r.Header.Set("X-Request-Id", id)
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), key, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// FromContext extracts the request ID from the context.
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(key).(string); ok {
		return v
	}
	return ""
}

// Generate creates a new random request ID (16 bytes = 32 hex chars).
func Generate() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
