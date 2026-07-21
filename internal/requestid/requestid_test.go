package requestid

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerate(t *testing.T) {
	id := Generate()
	if len(id) != 32 {
		t.Fatalf("expected 32 chars, got %d", len(id))
	}
	id2 := Generate()
	if id == id2 {
		t.Fatal("two calls to Generate should produce different IDs")
	}
}

func TestMiddlewareGeneratesID(t *testing.T) {
	var captured string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("X-Request-Id")
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured == "" {
		t.Fatal("expected request ID to be set on request")
	}
	if rec.Header().Get("X-Request-Id") != captured {
		t.Fatal("response header should match request header")
	}
}

func TestMiddlewareReusesExistingID(t *testing.T) {
	var captured string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Get("X-Request-Id")
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Request-Id", "existing-id-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if captured != "existing-id-123" {
		t.Fatalf("expected existing ID preserved, got %q", captured)
	}
}

func TestFromContext(t *testing.T) {
	ctx := context.WithValue(context.Background(), key, "test-id")
	if id := FromContext(ctx); id != "test-id" {
		t.Fatalf("expected 'test-id', got %q", id)
	}
}

func TestFromContextEmpty(t *testing.T) {
	if id := FromContext(context.Background()); id != "" {
		t.Fatalf("expected empty string, got %q", id)
	}
}

func TestMiddlewarePropagatesToContext(t *testing.T) {
	var ctxID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctxID = FromContext(r.Context())
	})

	handler := Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if ctxID == "" {
		t.Fatal("expected request ID in context")
	}
}
