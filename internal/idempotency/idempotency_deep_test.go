package idempotency

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSet_EmptyKey(t *testing.T) {
	s := NewStore(DefaultConfig())
	s.Set("", 200, []byte("data"), nil)
	if s.Size() != 1 {
		t.Error("empty key should be stored")
	}
}

func TestSet_EmptyBody(t *testing.T) {
	s := NewStore(DefaultConfig())
	s.Set("k1", 200, []byte{}, nil)
	entry := s.Get("k1")
	if entry == nil {
		t.Error("empty body should be stored")
	}
}

func TestGet_AfterExpiration(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 50 * time.Millisecond
	s := NewStore(cfg)
	s.Set("k1", 200, []byte("data"), nil)
	time.Sleep(100 * time.Millisecond)
	if s.Get("k1") != nil {
		t.Error("expired entry should return nil")
	}
}

func TestGet_AfterDeletion(t *testing.T) {
	s := NewStore(DefaultConfig())
	s.Set("k1", 200, []byte("data"), nil)
	s.Delete("k1")
	if s.Get("k1") != nil {
		t.Error("deleted entry should return nil")
	}
}

func TestSet_TTL0(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 0
	s := NewStore(cfg)
	s.Set("k1", 200, []byte("data"), nil)
	// TTL=0 means any duration > 0 is expired
	time.Sleep(10 * time.Millisecond)
	if s.Get("k1") != nil {
		t.Error("TTL=0 should expire immediately")
	}
}

func TestSet_TTLNegative(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = -1 * time.Second
	s := NewStore(cfg)
	s.Set("k1", 200, []byte("data"), nil)
	time.Sleep(1 * time.Millisecond)
	if s.Get("k1") != nil {
		t.Error("negative TTL should expire")
	}
}

func TestEviction_WhenFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxEntries = 2
	s := NewStore(cfg)
	s.Set("a", 200, []byte("a"), nil)
	s.Set("b", 200, []byte("b"), nil)
	s.Set("c", 200, []byte("c"), nil)
	if s.Size() > 2 {
		t.Errorf("should evict oldest, got %d entries", s.Size())
	}
}

func TestMiddleware_NoHeader_Passthrough(t *testing.T) {
	s := NewStore(DefaultConfig())
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	handler := s.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !called {
		t.Error("handler should be called without idempotency key")
	}
}

func TestMiddleware_CachesResponse(t *testing.T) {
	s := NewStore(DefaultConfig())
	calls := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("result"))
	})
	handler := s.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Idempotency-Key", "key-1")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
	// Second request with same key
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if calls != 1 {
		t.Fatal("handler should NOT be called on replay")
	}
	if rec2.Header().Get("Idempotent-Replayed") != "true" {
		t.Error("replay header missing")
	}
}

func TestMiddleware_ReplayStatusCode(t *testing.T) {
	s := NewStore(DefaultConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("created"))
	})
	handler := s.Middleware(inner)
	req := httptest.NewRequest("POST", "/api", nil)
	req.Header.Set("Idempotency-Key", "key-2")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusCreated {
		t.Errorf("replay should return 201, got %d", rec2.Code)
	}
}

func TestMiddleware_ReplayBody(t *testing.T) {
	s := NewStore(DefaultConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})
	handler := s.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Idempotency-Key", "key-3")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Body.String() != "hello" {
		t.Errorf("replay body = %q, want %q", rec2.Body.String(), "hello")
	}
}

func TestConcurrentSet_SameKey(t *testing.T) {
	s := NewStore(DefaultConfig())
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Set("k1", 200, []byte("data"), nil)
		}()
	}
	wg.Wait()
}

func TestCleanup_RemovesExpired(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxAge = 1 * time.Millisecond
	s := NewStore(cfg)
	s.Set("a", 200, []byte("a"), nil)
	s.Set("b", 200, []byte("b"), nil)
	time.Sleep(5 * time.Millisecond)
	removed := s.Cleanup()
	if removed != 2 {
		t.Errorf("expected 2 removed, got %d", removed)
	}
	if s.Size() != 0 {
		t.Errorf("expected 0 entries, got %d", s.Size())
	}
}

func TestCleanup_KeepsValid(t *testing.T) {
	s := NewStore(DefaultConfig())
	s.Set("a", 200, []byte("a"), nil)
	removed := s.Cleanup()
	if removed != 0 {
		t.Error("valid entries should not be removed")
	}
	if s.Size() != 1 {
		t.Errorf("expected 1 entry, got %d", s.Size())
	}
}

func TestMiddleware_ReplayHeaders(t *testing.T) {
	s := NewStore(DefaultConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
	})
	handler := s.Middleware(inner)
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Idempotency-Key", "key-4")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Header().Get("X-Custom") != "value" {
		t.Error("replay should preserve headers")
	}
}
