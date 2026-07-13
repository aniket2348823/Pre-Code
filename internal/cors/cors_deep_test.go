package cors

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestPreflight_204(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("preflight should not reach handler") })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("OPTIONS", "/api", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
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

func TestProductionConfig_RestrictsOrigin(t *testing.T) {
	cfg := ProductionConfig([]string{"https://app.example.com"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	// Allowed
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Error("allowed origin should get header")
	}
	// Disallowed
	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Origin", "https://evil.com")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("disallowed origin should not get header")
	}
}

func TestNoOrigin_NoCORSHeader(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("without Origin, wildcard should be set")
	}
}

func TestExposeHeaders_Deep(t *testing.T) {
	cfg := DefaultConfig()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	expose := rec.Header().Get("Access-Control-Expose-Headers")
	if expose == "" {
		t.Error("expected Expose-Headers")
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

func TestCredentialsHeader_Deep(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AllowCredentials = true
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	handler := cfg.Middleware(inner)
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected credentials header")
	}
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
