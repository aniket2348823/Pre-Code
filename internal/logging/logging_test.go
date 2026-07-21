package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestNew(t *testing.T) {
	logger := New("test-component")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithRequestID(t *testing.T) {
	logger := New("test")
	ctx := context.WithValue(context.Background(), RequestIDKey, "req-123")
	log := WithRequestID(ctx, logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithRequestID_Missing(t *testing.T) {
	logger := New("test")
	log := WithRequestID(context.Background(), logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestFromContext_Default(t *testing.T) {
	logger := FromContext(context.Background())
	if logger == nil {
		t.Fatal("expected non-nil default logger")
	}
}

func TestContextWithLogger(t *testing.T) {
	logger := New("test")
	ctx := ContextWithLogger(context.Background(), logger)
	got := FromContext(ctx)
	if got != logger {
		t.Error("expected logger from context to match")
	}
}

func TestWithAttrs(t *testing.T) {
	logger := New("test")
	log := WithAttrs(logger, "key", "value")
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestEnsureSlogUsed(t *testing.T) {
	// Verify slog.Info is referenced (prevents unused import)
	slog.Info("test log entry", "component", "logging_test")
}
