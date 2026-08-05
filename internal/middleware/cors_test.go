package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/config"
)

func handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestCORS_Preflight(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("wrong ACAO header: %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Methods") != "GET, POST" {
		t.Fatalf("wrong methods: %s", rr.Header().Get("Access-Control-Allow-Methods"))
	}
	if rr.Header().Get("Access-Control-Max-Age") != "3600" {
		t.Fatalf("wrong max-age: %s", rr.Header().Get("Access-Control-Max-Age"))
	}
}

func TestCORS_Preflight_Credentials(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET"},
		AllowedHeaders:   []string{"Authorization"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatal("expected credentials header")
	}
}

func TestCORS_SimpleRequest(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("wrong ACAO: %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Vary") != "Origin" {
		t.Fatalf("expected Vary: Origin, got %s", rr.Header().Get("Vary"))
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCORS_WildcardOrigin(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://anything.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected *, got %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Vary") != "" {
		t.Fatal("Vary should be empty for wildcard")
	}
}

func TestCORS_SubdomainPattern(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"*.example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://app.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
		t.Fatalf("expected subdomain origin, got %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO header, got %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCORS_NoOriginHeader(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("expected no ACAO header for no-origin request, got %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCORS_CaseInsensitiveOrigin(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"HTTPS://Example.COM"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Fatalf("expected case-insensitive match, got %s", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORS_MultipleOrigins(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://a.com", "https://b.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	origins := []string{"https://a.com", "https://b.com", "https://c.com"}
	for _, o := range origins {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Origin", o)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)

		if o == "https://c.com" {
			if rr.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("expected no ACAO for %s", o)
			}
		} else {
			if rr.Header().Get("Access-Control-Allow-Origin") != o {
				t.Fatalf("expected ACAO=%s, got %s", o, rr.Header().Get("Access-Control-Allow-Origin"))
			}
		}
	}
}

func TestCORS_Preflight_DisallowedOrigin(t *testing.T) {
	cfg := config.CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET"},
		AllowedHeaders: []string{"Content-Type"},
		MaxAge:         3600,
	}
	h := CORS(cfg)(handler())

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	req.Header.Set("Origin", "https://evil.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin should not get preflight headers")
	}
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 passthrough, got %d", rr.Code)
	}
}
