package middleware

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("vigilagent/router")

// TracingMiddleware creates spans for each incoming request.
func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path,
			trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.url", r.URL.String()),
				attribute.String("http.host", r.Host),
				attribute.String("http.user_agent", r.UserAgent()),
				attribute.String("http.remote_addr", r.RemoteAddr),
			),
		)
		defer span.End()

		// Wrap response writer to capture status code
		ww := &tracingRecorder{ResponseWriter: w, statusCode: 200}
		next.ServeHTTP(ww, r.WithContext(ctx))

		span.SetAttributes(
			attribute.Int("http.status_code", ww.statusCode),
		)

		if ww.statusCode >= 400 {
			span.SetAttributes(attribute.Bool("http.error", true))
		}
	})
}

// tracingRecorder captures the response status code.
type tracingRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *tracingRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController and
// downstream middleware can reach the real writer (Hijacker/Flusher support).
func (r *tracingRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// Flush forwards to the underlying Flusher (SSE streaming support).
func (r *tracingRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack forwards to the underlying Hijacker (WebSocket upgrade support).
func (r *tracingRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// StartSpan starts a new span in the given context.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("vigilagent").Start(ctx, name)
}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}
