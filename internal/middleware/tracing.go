package middleware

import (
	"context"
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

// StartSpan starts a new span in the given context.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("vigilagent").Start(ctx, name)
}

// SpanFromContext returns the current span from context.
func SpanFromContext(ctx context.Context) trace.Span {
	return trace.SpanFromContext(ctx)
}
