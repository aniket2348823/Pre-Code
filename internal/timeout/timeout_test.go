package timeout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMiddlewareTimeout(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte("slow"))
	})

	handler := Middleware(50 * time.Millisecond)(inner)
	req := httptest.NewRequest("GET", "/slow", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}
}

func TestMiddlewareFastResponse(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fast"))
	})

	handler := Middleware(1 * time.Second)(inner)
	req := httptest.NewRequest("GET", "/fast", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Body.String() != "fast" {
		t.Fatalf("expected 'fast', got %q", rec.Body.String())
	}
}

func TestMiddlewareContextCancelled(t *testing.T) {
	// The handler is held on `release` until AFTER the middleware has already
	// responded 504, so the late write is guaranteed to hit the timedOut
	// suppression path. (Previously the handler's write raced the middleware's
	// timeout decision at the deadline boundary and the test flaked.)
	release := make(chan struct{})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		<-release
		w.Write([]byte("aborted"))
	})

	handler := Middleware(50 * time.Millisecond)(inner)
	req := httptest.NewRequest("GET", "/ctx", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected 504, got %d", rec.Code)
	}

	// Now let the handler attempt its late write — it must be suppressed.
	close(release)
	time.Sleep(10 * time.Millisecond)

	if strings.Contains(rec.Body.String(), "aborted") {
		t.Fatal("handler write after cancellation must be suppressed")
	}
}
