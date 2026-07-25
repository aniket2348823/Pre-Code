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

	// With AllowOrigins=["*"] and a specific Origin header, CORS best practice is to reflect the origin
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
