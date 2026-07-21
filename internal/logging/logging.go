// Package logging provides structured logging helpers for the deterministic
// engine layers. All engines receive a *slog.Logger that outputs JSON to
// stdout by default, making logs machine-parseable for observability stacks.
package logging

import (
	"context"
	"log/slog"
	"os"
)

// ContextKey is the type for context keys in this package.
// Exported so that RequestIDKey and TraceIDKey are usable outside this package.
type ContextKey string

const (
	// RequestIDKey is the context key for the request ID.
	RequestIDKey ContextKey = "request_id"
	// TraceIDKey is the context key for the OpenTelemetry trace ID.
	TraceIDKey ContextKey = "trace_id"
	loggerKey  ContextKey = "logger"
)

// New creates a new structured logger with the given component name.
// Output goes to stdout as JSON.
func New(component string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})).With("component", component)
}

// WithRequestID attaches a request ID to the logger context.
func WithRequestID(ctx context.Context, logger *slog.Logger) *slog.Logger {
	if id, ok := ctx.Value(RequestIDKey).(string); ok && id != "" {
		return logger.With("request_id", id)
	}
	return logger
}

// WithAttrs adds arbitrary attributes to a logger.
func WithAttrs(logger *slog.Logger, attrs ...any) *slog.Logger {
	return logger.With(attrs...)
}

// FromContext extracts a logger from context, or returns a default one.
func FromContext(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return New("default")
	}
	logger, ok := ctx.Value(loggerKey).(*slog.Logger)
	if !ok || logger == nil {
		return New("default")
	}
	return logger
}

// ContextWithLogger attaches a logger to the context.
func ContextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}
