package timeout

import (
	"net/http"
	"net/http/httptest"
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
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		// After context cancellation, write should be silent
		w.Write([]byte("aborted"))
	})

	handler := Middleware(50 * time.Millisecond)(inner)
	req := httptest.NewRequest("GET", "/ctx", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Handler should have been cancelled — response should be 504 or empty
	if rec.Body.String() == "aborted" {
		t.Fatal("handler should not complete after context cancellation")
	}
}
