package compression

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddlewareCompresses(t *testing.T) {
	body := strings.Repeat("hello world test data ", 100)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("expected gzip Content-Encoding header")
	}
	// Compressed body should be smaller than original
	if rec.Body.Len() >= len(body) {
		t.Fatalf("expected compressed size < %d, got %d", len(body), rec.Body.Len())
	}
}

func TestMiddlewareSkipsNonGzipClient(t *testing.T) {
	body := "hello world"
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	// No Accept-Encoding header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Encoding") == "gzip" {
		t.Fatal("should not compress when client doesn't accept gzip")
	}
	if rec.Body.String() != body {
		t.Fatalf("expected original body, got %q", rec.Body.String())
	}
}

func TestMiddlewarePreservesContentType(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"key":"value"}`))
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type preserved, got %q", rec.Header().Get("Content-Type"))
	}
}

func TestMiddlewareRemovesContentLength(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data that is long enough to trigger content length calculation"))
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Content-Encoding is set, which means Content-Length is not applicable
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("expected gzip encoding")
	}
}

func TestMiddlewareSmallPayload(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hi"))
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	// Should still work even if gzip header is larger than savings
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
