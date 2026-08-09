package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type ctxKeyType struct{}

var ctxKey = ctxKeyType{}

const RequestIDHeader = "X-Request-ID"

// NewRequestID middleware injects a request ID into the context and response header.
func NewRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if id == "" {
			id = generateUUIDv4()
			r.Header.Set(RequestIDHeader, id)
		}
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), ctxKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext extracts the request ID stored in ctx.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(ctxKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithRequestID returns a copy of ctx associated with the given request ID.
func ContextWithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
		ctx = context.Background()
	}
	return context.WithValue(ctx, ctxKey, id)
}

func generateUUIDv4() string {
	return uuid.New().String()
}
