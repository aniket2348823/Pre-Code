package rateguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDefaultEndpointConfig(t *testing.T) {
	cfg := DefaultEndpointConfig()
	if cfg.DefaultLimit != 100 {
		t.Fatalf("expected default limit 100, got %d", cfg.DefaultLimit)
	}
	if len(cfg.Rules) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(cfg.Rules))
	}
}

func TestAllowWithinLimit(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  5,
		DefaultWindow: time.Minute,
	}
	limiter := NewEndpointLimiter(cfg)

	req := httptest.NewRequest("GET", "/api/test", nil)
	for i := 0; i < 5; i++ {
		if !limiter.Allow(req) {
			t.Fatalf("request %d should be allowed", i)
		}
	}
}

func TestDenyOverLimit(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  2,
		DefaultWindow: time.Minute,
	}
	limiter := NewEndpointLimiter(cfg)

	req := httptest.NewRequest("GET", "/api/test", nil)
	limiter.Allow(req)
	limiter.Allow(req)

	if limiter.Allow(req) {
		t.Fatal("third request should be denied")
	}
}

func TestPerEndpointLimits(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  100,
		DefaultWindow: time.Minute,
		Rules: []EndpointRule{
			{Pattern: "/api/v1/scan", Limit: 2, Window: time.Minute},
		},
	}
	limiter := NewEndpointLimiter(cfg)

	scanReq := httptest.NewRequest("GET", "/api/v1/scan", nil)
	otherReq := httptest.NewRequest("GET", "/api/v1/other", nil)

	limiter.Allow(scanReq)
	limiter.Allow(scanReq)
	if limiter.Allow(scanReq) {
		t.Fatal("scan endpoint should be limited to 2")
	}

	// Other endpoint should still be fine
	if !limiter.Allow(otherReq) {
		t.Fatal("other endpoint should not be affected by scan limit")
	}
}

func TestBurstAllowance(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  2,
		DefaultWindow: time.Minute,
		Rules: []EndpointRule{
			{Pattern: "/api/fast", Limit: 2, Window: time.Minute, Burst: 1},
		},
	}
	limiter := NewEndpointLimiter(cfg)

	req := httptest.NewRequest("GET", "/api/fast", nil)
	limiter.Allow(req)
	limiter.Allow(req)
	// Burst allows 1 extra
	if !limiter.Allow(req) {
		t.Fatal("burst should allow one extra request")
	}
	// Now at limit + burst
	if limiter.Allow(req) {
		t.Fatal("should deny after burst exhausted")
	}
}

func TestMiddlewareReturns429(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  1,
		DefaultWindow: time.Minute,
	}
	limiter := NewEndpointLimiter(cfg)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := limiter.Middleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	// First request passes
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Second request rate limited
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec2.Code)
	}
}

func TestStats(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  10,
		DefaultWindow: time.Minute,
	}
	limiter := NewEndpointLimiter(cfg)

	req := httptest.NewRequest("GET", "/api/a", nil)
	limiter.Allow(req)
	limiter.Allow(req)

	stats := limiter.Stats()
	if stats["GET /api/a"] != 2 {
		t.Fatalf("expected 2 requests tracked, got %d", stats["GET /api/a"])
	}
}

func TestCustomKeyFunc(t *testing.T) {
	cfg := EndpointConfig{
		DefaultLimit:  1,
		DefaultWindow: time.Minute,
	}
	limiter := NewEndpointLimiter(cfg)
	limiter.SetKeyFunc(func(r *http.Request) string {
		return r.Header.Get("X-User-Id")
	})

	req1 := httptest.NewRequest("GET", "/api", nil)
	req1.Header.Set("X-User-Id", "user-a")
	req2 := httptest.NewRequest("GET", "/api", nil)
	req2.Header.Set("X-User-Id", "user-b")

	limiter.Allow(req1)
	// user-b should still be allowed (separate key)
	if !limiter.Allow(req2) {
		t.Fatal("different users should have separate rate limits")
	}
	// user-a should be denied
	if limiter.Allow(req1) {
		t.Fatal("user-a should be rate limited")
	}
}
