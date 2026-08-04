package cors

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestMiddlewareAddsOriginHeader(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("expected https://example.com, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestPreflightReturns204(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight should not reach handler")
	})

	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api/data", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("expected Allow-Methods header on preflight")
	}
}

func TestProductionConfigRestrictsOrigin(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cfg.Middleware(inner)

	// Allowed origin
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatal("expected matching origin allowed")
	}

	// Disallowed origin
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Origin", "https://evil.com")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("expected no CORS header for disallowed origin")
	}
}

func TestCredentialsHeader(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowCredentials = true
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials header")
	}
}

func TestNoOriginNoCORSHeader(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	// No Origin header
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("without Origin header, wildcard should still be set for AllowOrigins=[*]")
	}
}

func TestExposeHeaders(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	expose := rec.Header().Get("Access-Control-Expose-Headers")
	if expose == "" {
		t.Fatal("expected Expose-Headers header")
	}
}

func TestPreflight_DisallowedMethod(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin should not get CORS header")
	}
}

func TestConcurrentPreflight(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach handler") })
	handler := cfg.Middleware(inner)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("OPTIONS", "/api", nil)
			req.Header.Set("Origin", "https://example.com")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusNoContent {
				t.Errorf("expected 204, got %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

func TestMaxAge(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach") })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	maxAge := rec.Header().Get("Access-Control-Max-Age")
	if maxAge == "" {
		t.Error("expected Max-Age header")
	}
}

func TestPreflight_WithCredentials(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach") })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("preflight with credentials should set header")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

func TestPreflight_NoOrigin(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach") })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
}

func TestRegularRequest_AllowedOrigin(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Error("expected origin header for allowed origin")
	}
}

func TestProductionConfig_NoCredentials(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	cfg.AllowCredentials = false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Error("should not have credentials header when disabled")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if len(cfg.AllowOrigins) == 0 {
		t.Error("default config should have origins")
	}
	if len(cfg.AllowMethods) == 0 {
		t.Error("default config should have methods")
	}
}

func TestProductionConfig_Credentials(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	if !cfg.AllowCredentials {
		t.Error("production config should allow credentials")
	}
}

// --- Subdomain pattern matching tests ---

func TestMatchOrigin_Exact(t *testing.T) {
	if !matchOrigin("https://app.example.com", "https://app.example.com") {
		t.Error("exact match should succeed")
	}
	if matchOrigin("https://app.example.com", "https://other.example.com") {
		t.Error("different host should fail")
	}
}

func TestMatchOrigin_SubdomainWildcard(t *testing.T) {
	tests := []struct {
		pattern string
		origin  string
		want    bool
	}{
		{"*.example.com", "https://sub.example.com", true},
		{"*.example.com", "https://deep.sub.example.com", true},
		{"*.example.com", "https://example.com", false},
		{"*.example.com", "https://evil-example.com", false},
		{"*.example.com", "https://sub.example.com:8080", true},
		{"*.example.com", "http://sub.example.com", true},
		{"*.example.com", "https://sub.other.com", false},
	}
	for _, tt := range tests {
		got := matchOrigin(tt.pattern, tt.origin)
		if got != tt.want {
			t.Errorf("matchOrigin(%q, %q) = %v, want %v", tt.pattern, tt.origin, got, tt.want)
		}
	}
}

func TestIsOriginAllowed_WithSubdomainPattern(t *testing.T) {
	cfg := Config{
		AllowOrigins: []string{"https://app.example.com", "*.internal.dev"},
	}

	tests := []struct {
		origin string
		want   bool
	}{
		{"https://app.example.com", true},
		{"https://other.example.com", false},
		{"https://api.internal.dev", true},
		{"https://deep.api.internal.dev", true},
		{"https://evil.com", false},
	}
	for _, tt := range tests {
		got := cfg.isOriginAllowed(tt.origin)
		if got != tt.want {
			t.Errorf("isOriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestMiddleware_VaryOrigin(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)

	// Regular request
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("expected Vary: Origin, got %q", rec.Header().Get("Vary"))
	}
}

func TestMiddleware_VaryOrigin_Preflight(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach") })
	handler := cfg.Middleware(inner)

	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Vary") != "Origin" {
		t.Errorf("preflight expected Vary: Origin, got %q", rec.Header().Get("Vary"))
	}
}

func TestMiddleware_RejectedOrigin_NoHeader(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("rejected origin should not get CORS headers")
	}
	if rec.Header().Get("Vary") != "" {
		t.Error("rejected origin should not set Vary header")
	}
}

func TestMiddleware_SubdomainPattern_Allowed(t *testing.T) {
	cfg := Config{
		AllowOrigins:     []string{"*.example.com"},
		AllowMethods:     []string{"GET", "POST"},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://api.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "https://api.example.com" {
		t.Errorf("subdomain should be allowed, got %q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("credentials should be set for allowed subdomain")
	}
}

func TestMiddleware_SubdomainPattern_Rejected(t *testing.T) {
	cfg := Config{
		AllowOrigins: []string{"*.example.com"},
		AllowMethods: []string{"GET"},
	}
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("non-matching subdomain should be rejected")
	}
}

func TestMiddleware_Preflight_RejectedOrigin(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("should not reach") })
	handler := cfg.Middleware(inner)

	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://evil.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("rejected preflight should not get CORS header")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("rejected preflight should return 204, got %d", rec.Code)
	}
}

func TestMultipleOrigins_WithSubdomain(t *testing.T) {
	cfg := Config{
		AllowOrigins: []string{"https://app.example.com", "*.internal.dev", "https://specific.other.com"},
	}
	if !cfg.isOriginAllowed("https://app.example.com") {
		t.Error("first exact origin should match")
	}
	if !cfg.isOriginAllowed("https://staging.internal.dev") {
		t.Error("subdomain pattern should match")
	}
	if !cfg.isOriginAllowed("https://specific.other.com") {
		t.Error("third exact origin should match")
	}
	if cfg.isOriginAllowed("https://evil.com") {
		t.Error("unknown origin should not match")
	}
}
