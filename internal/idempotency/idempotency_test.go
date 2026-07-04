package idempotency

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.KeyHeader != "Idempotency-Key" {
		t.Fatalf("expected 'Idempotency-Key', got %q", cfg.KeyHeader)
	}
	if cfg.MaxAge != 24*time.Hour {
		t.Fatalf("expected 24h, got %v", cfg.MaxAge)
	}
}

func TestStoreSetAndGet(t *testing.T) {
	store := NewStore(DefaultConfig())
	store.Set("key1", 200, []byte("hello"), http.Header{"Content-Type": []string{"text/plain"}})

	entry := store.Get("key1")
	if entry == nil {
		t.Fatal("expected cached entry")
	}
	if entry.statusCode != 200 {
		t.Fatalf("expected 200, got %d", entry.statusCode)
	}
	if string(entry.body) != "hello" {
		t.Fatalf("expected 'hello', got %q", string(entry.body))
	}
}

func TestStoreMiss(t *testing.T) {
	store := NewStore(DefaultConfig())
	if store.Get("nonexistent") != nil {
		t.Fatal("expected nil for cache miss")
	}
}

func TestStoreExpiration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 1 * time.Millisecond
	store := NewStore(cfg)

	store.Set("key1", 200, []byte("data"), nil)
	time.Sleep(2 * time.Millisecond)

	if store.Get("key1") != nil {
		t.Fatal("expected nil for expired entry")
	}
}

func TestStoreDelete(t *testing.T) {
	store := NewStore(DefaultConfig())
	store.Set("key1", 200, []byte("data"), nil)
	store.Delete("key1")
	if store.Get("key1") != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestStoreEviction(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxEntries = 2
	store := NewStore(cfg)

	store.Set("a", 200, []byte("a"), nil)
	store.Set("b", 200, []byte("b"), nil)
	store.Set("c", 200, []byte("c"), nil) // should evict oldest

	if store.Size() > 2 {
		t.Fatalf("expected at most 2 entries, got %d", store.Size())
	}
}

func TestCleanup(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 1 * time.Millisecond
	store := NewStore(cfg)

	store.Set("a", 200, []byte("a"), nil)
	store.Set("b", 200, []byte("b"), nil)
	time.Sleep(2 * time.Millisecond)

	removed := store.Cleanup()
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if store.Size() != 0 {
		t.Fatalf("expected 0 entries, got %d", store.Size())
	}
}

func TestMiddlewareFirstRequest(t *testing.T) {
	store := NewStore(DefaultConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})

	handler := store.Middleware(inner)
	req := httptest.NewRequest("POST", "/api", nil)
	req.Header.Set("Idempotency-Key", "req-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if rec.Body.String() != "created" {
		t.Fatalf("expected 'created', got %q", rec.Body.String())
	}
	if rec.Header().Get("Idempotent-Replayed") == "true" {
		t.Fatal("first request should not be replayed")
	}
}

func TestMiddlewareReplay(t *testing.T) {
	store := NewStore(DefaultConfig())
	var callCount int
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("result"))
	})

	handler := store.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Idempotency-Key", "req-2")

	// First request
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if callCount != 1 {
		t.Fatalf("handler should be called once, got %d", callCount)
	}

	// Second request with same key
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if callCount != 1 {
		t.Fatal("handler should NOT be called on replay")
	}
	if rec2.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatal("replayed response should have Idempotent-Replayed header")
	}
}

func TestMiddlewareNoKeyPassthrough(t *testing.T) {
	store := NewStore(DefaultConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := store.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	// No Idempotency-Key header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
