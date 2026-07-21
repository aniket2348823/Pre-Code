package graceful

import (
	"net/http"
	"testing"
	"time"
)

func TestNewDefaultTimeout(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 0)
	if s.timeout != 15*time.Second {
		t.Fatalf("expected 15s default timeout, got %v", s.timeout)
	}
}

func TestNewCustomTimeout(t *testing.T) {
	s := New(http.DefaultServeMux, ":8080", 30*time.Second)
	if s.timeout != 30*time.Second {
		t.Fatalf("expected 30s timeout, got %v", s.timeout)
	}
	if s.Addr() != ":8080" {
		t.Fatalf("expected :8080, got %q", s.Addr())
	}
}

func TestShutdownWithoutServe(t *testing.T) {
	s := New(http.DefaultServeMux, ":0", 1*time.Second)
	// Shutdown without starting should not panic
	if err := s.Shutdown(); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
