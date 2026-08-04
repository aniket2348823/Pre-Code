package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ─── ETag Middleware Tests ───────────────────────────────────────────────

func TestETag_SetsETagHeader(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":"test"}`))
	}))

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag header to be set")
	}
	if etag[0] != '"' {
		t.Errorf("expected strong ETag, got %s", etag)
	}
}

func TestETag_WeakETag(t *testing.T) {
	cfg := DefaultETagConfig()
	cfg.Weak = true
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	etag := w.Header().Get("ETag")
	if len(etag) < 3 || etag[:2] != "W/" {
		t.Errorf("expected weak ETag prefix W/, got %s", etag)
	}
}

func TestETag_IfNoneMatchReturns304(t *testing.T) {
	cfg := DefaultETagConfig()
	body := []byte(`{"data":"test"}`)
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))

	// First request to get the ETag
	req1 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")

	// Second request with matching If-None-Match
	req2 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Errorf("expected empty body for 304, got %d bytes", w2.Body.Len())
	}
}

func TestETag_IfNoneMismatchServesFull(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("If-None-Match", `"deadbeef"`)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for mismatched ETag, got %d", w.Code)
	}
}

func TestETag_SkipsNonGET(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		req := httptest.NewRequest(method, "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Header().Get("ETag") != "" {
			t.Errorf("%s should not get ETag header", method)
		}
	}
}

func TestETag_SkipsConfiguredPaths(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("ETag") != "" {
		t.Error("health endpoint should be skipped")
	}
}

func TestETag_PathPatternFilter(t *testing.T) {
	cfg := DefaultETagConfig()
	cfg.WithPathPatterns([]string{"/api/v1/tasks"})
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	// Should cache /api/v1/tasks
	req1 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Header().Get("ETag") == "" {
		t.Error("/api/v1/tasks should get ETag")
	}

	// Should NOT cache /api/v1/agents
	req2 := httptest.NewRequest("GET", "/api/v1/agents", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Header().Get("ETag") != "" {
		t.Error("/api/v1/agents should not get ETag when filtered")
	}
}

func TestETag_SetsCacheControl(t *testing.T) {
	cfg := DefaultETagConfig()
	cfg.MaxAge = 60 * time.Second
	cfg.StaleWhileRevalidate = 120 * time.Second
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	cc := w.Header().Get("Cache-Control")
	if cc == "" {
		t.Fatal("expected Cache-Control header")
	}
	if !containsStr(cc, "max-age=60") {
		t.Errorf("expected max-age=60, got %s", cc)
	}
	if !containsStr(cc, "stale-while-revalidate=120") {
		t.Errorf("expected stale-while-revalidate=120, got %s", cc)
	}
	if !containsStr(cc, "private") {
		t.Error("expected private in Cache-Control")
	}
}

func TestETag_SkipsEmptyBody(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("ETag") != "" {
		t.Error("should not set ETag for empty body")
	}
}

func TestETag_SkipsErrorStatus(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("ETag") != "" {
		t.Error("should not set ETag for error status")
	}
}

func TestETag_WildcardIfNoneMatch(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("If-None-Match", "*")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Errorf("expected 304 for wildcard If-None-Match, got %d", w.Code)
	}
}

func TestETag_DifferentPathsHaveDifferentETags(t *testing.T) {
	cfg := DefaultETagConfig()
	handler := ETagMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("same body"))
	}))

	req1 := httptest.NewRequest("GET", "/path/a", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest("GET", "/path/b", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// Same body but different paths — ETags should still be equal
	// since ETag is computed from body only
	etag1 := w1.Header().Get("ETag")
	etag2 := w2.Header().Get("ETag")
	if etag1 != etag2 {
		t.Error("same body should produce same ETag regardless of path")
	}
}

// ─── ResponseCache Tests ────────────────────────────────────────────────

func TestResponseCache_SetAndGet(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())

	entry := &CacheEntry{
		Body:        []byte("test body"),
		ContentType: "application/json",
		ETag:        `"abc123"`,
		StatusCode:  200,
		StoredAt:    time.Now(),
		TTL:         time.Minute,
	}

	cache.Set("/test", entry)
	got, ok := cache.Get("/test")
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got.Body) != "test body" {
		t.Errorf("expected 'test body', got %s", string(got.Body))
	}
}

func TestResponseCache_Expires(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())

	entry := &CacheEntry{
		Body:     []byte("old"),
		StoredAt: time.Now().Add(-2 * time.Minute),
		TTL:      time.Minute,
	}
	cache.Set("/old", entry)

	_, ok := cache.Get("/old")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestResponseCache_LRUEviction(t *testing.T) {
	cfg := ResponseCacheConfig{MaxSize: 3, DefaultTTL: time.Minute}
	cache := NewResponseCache(cfg)

	for i := 0; i < 5; i++ {
		cache.Set("/item/"+string(rune('a'+i)), &CacheEntry{
			Body:     []byte("body"),
			StoredAt: time.Now(),
			TTL:      time.Minute,
		})
	}

	if cache.Len() != 3 {
		t.Errorf("expected 3 entries after eviction, got %d", cache.Len())
	}

	// First two should be evicted
	_, ok1 := cache.Get("/item/a")
	_, ok2 := cache.Get("/item/b")
	if ok1 || ok2 {
		t.Error("oldest entries should be evicted")
	}
}

func TestResponseCache_InvalidatePrefix(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())

	cache.Set("/api/v1/agents/1", &CacheEntry{Body: []byte("a"), StoredAt: time.Now(), TTL: time.Minute})
	cache.Set("/api/v1/agents/2", &CacheEntry{Body: []byte("b"), StoredAt: time.Now(), TTL: time.Minute})
	cache.Set("/api/v1/tasks/1", &CacheEntry{Body: []byte("c"), StoredAt: time.Now(), TTL: time.Minute})

	count := cache.InvalidatePrefix("/api/v1/agents")
	if count != 2 {
		t.Errorf("expected 2 invalidations, got %d", count)
	}
	if cache.Len() != 1 {
		t.Errorf("expected 1 entry remaining, got %d", cache.Len())
	}
}

func TestResponseCache_Stats(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())

	cache.Set("/a", &CacheEntry{Body: []byte("a"), StoredAt: time.Now(), TTL: time.Minute})
	cache.Get("/a") // hit
	cache.Get("/b") // miss

	stats := cache.Stats()
	if stats["hits"] != 1 {
		t.Errorf("expected 1 hit, got %d", stats["hits"])
	}
	if stats["misses"] != 1 {
		t.Errorf("expected 1 miss, got %d", stats["misses"])
	}
}

func TestResponseCache_Delete(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	cache.Set("/x", &CacheEntry{Body: []byte("x"), StoredAt: time.Now(), TTL: time.Minute})

	cache.Delete("/x")
	_, ok := cache.Get("/x")
	if ok {
		t.Error("expected miss after delete")
	}
}

// ─── ResponseCacheMiddleware Tests ──────────────────────────────────────

func TestResponseCacheMiddleware_CachesGET(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	handler := ResponseCacheMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))

	// First request — miss
	req1 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if w1.Header().Get("X-Cache") != "MISS" {
		t.Error("expected X-Cache: MISS on first request")
	}

	// Second request — hit
	req2 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)
	if w2.Header().Get("X-Cache") != "HIT" {
		t.Error("expected X-Cache: HIT on second request")
	}
	if w2.Body.String() != `{"ok":true}` {
		t.Errorf("expected cached body, got %s", w2.Body.String())
	}
}

func TestResponseCacheMiddleware_SkipsPOST(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	called := false
	handler := ResponseCacheMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("POST should pass through")
	}
	if w.Header().Get("X-Cache") != "" {
		t.Error("POST should not set X-Cache header")
	}
}

func TestResponseCacheMiddleware_NoCacheHeader(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	handler := ResponseCacheMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("data"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Cache-Control", "no-cache")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if cache.Len() != 0 {
		t.Error("no-cache header should bypass caching")
	}
}

func TestResponseCacheMiddleware_IfNoneMatch304(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	handler := ResponseCacheMiddleware(cache)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("cached content"))
	}))

	// Populate cache
	req1 := httptest.NewRequest("GET", "/cached", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	etag := w1.Header().Get("ETag")

	// Request with matching ETag
	req2 := httptest.NewRequest("GET", "/cached", nil)
	req2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusNotModified {
		t.Errorf("expected 304, got %d", w2.Code)
	}
}

func TestResponseCacheMiddleware_InvalidatesOnWrite(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			w.Write([]byte(`{"data":"list"}`))
		} else {
			w.Write([]byte(`{"ok":true}`))
		}
	})

	handler := CacheInvalidationMiddleware(cache)(ResponseCacheMiddleware(cache)(inner))

	// Populate cache
	req1 := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, req1)
	if cache.Len() != 1 {
		t.Fatalf("expected 1 cached entry, got %d", cache.Len())
	}

	// Write operation invalidates
	req2 := httptest.NewRequest("POST", "/api/v1/tasks", nil)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	// GET cache should be invalidated
	_, ok := cache.Get("/api/v1/tasks")
	if ok {
		t.Error("cache should be invalidated after POST")
	}
}

// ─── ParseMaxAge Tests ──────────────────────────────────────────────────

func TestParseMaxAge(t *testing.T) {
	tests := []struct {
		header   string
		expected time.Duration
	}{
		{"max-age=30", 30 * time.Second},
		{"public, max-age=3600", 3600 * time.Second},
		{"no-cache", 0},
		{"max-age=0, must-revalidate", 0},
		{"", 0},
	}
	for _, tt := range tests {
		got := ParseMaxAge(tt.header)
		if got != tt.expected {
			t.Errorf("ParseMaxAge(%q) = %v, want %v", tt.header, got, tt.expected)
		}
	}
}

// ─── StaleWhileRevalidate Tests ────────────────────────────────────────

func TestStaleWhileRevalidate_ServesStaleContent(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	swr := NewStaleWhileRevalidate(cache, time.Minute)

	// Manually insert a stale entry
	cache.Set("/stale", &CacheEntry{
		Body:        []byte("stale data"),
		ContentType: "application/json",
		ETag:        `"stale-etag"`,
		StatusCode:  200,
		StoredAt:    time.Now().Add(-2 * time.Minute),
		TTL:         time.Minute,
	})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fresh data"))
	})

	handler := swr.Middleware(inner)
	req := httptest.NewRequest("GET", "/stale", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-Cache") != "STALE" {
		t.Error("expected X-Cache: STALE")
	}
	if w.Body.String() != "stale data" {
		t.Errorf("expected stale data, got %s", w.Body.String())
	}
}

func TestStaleWhileRevalidate_SkipsNonGET(t *testing.T) {
	cache := NewResponseCache(DefaultResponseCacheConfig())
	swr := NewStaleWhileRevalidate(cache, time.Minute)

	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.Write([]byte("ok"))
	})

	handler := swr.Middleware(inner)
	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("POST should pass through")
	}
}

// ─── Helper ─────────────────────────────────────────────────────────────

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
