package logging

import (
	"context"
	"sync"
	"testing"
)

func TestNew_NilConfig(t *testing.T) {
	// New doesn't take config, just component name
	logger := New("test")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithRequestID_EmptyString(t *testing.T) {
	logger := New("test")
	ctx := context.WithValue(context.Background(), RequestIDKey, "")
	log := WithRequestID(ctx, logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithRequestID_VeryLongID(t *testing.T) {
	logger := New("test")
	longID := make([]byte, 10000)
	for i := range longID {
		longID[i] = 'a'
	}
	ctx := context.WithValue(context.Background(), RequestIDKey, string(longID))
	log := WithRequestID(ctx, logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestFromContext_NilContext(t *testing.T) {
	logger := FromContext(nil)
	if logger == nil {
		t.Fatal("expected non-nil default logger")
	}
}

func TestFromContext_NoLogger(t *testing.T) {
	logger := FromContext(context.Background())
	if logger == nil {
		t.Fatal("expected non-nil default logger")
	}
}

func TestContextWithLogger_NilLogger(t *testing.T) {
	// Should not panic with nil logger
	ctx := ContextWithLogger(context.Background(), nil)
	got := FromContext(ctx)
	if got == nil {
		t.Fatal("FromContext should return default logger")
	}
}

func TestWithAttrs_NilAttributes(t *testing.T) {
	logger := New("test")
	log := WithAttrs(logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestWithAttrs_EmptyAttributes(t *testing.T) {
	logger := New("test")
	log := WithAttrs(logger)
	if log == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestConcurrentLogging(t *testing.T) {
	logger := New("test")
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("test message", "key", "value")
		}()
	}
	wg.Wait()
}
