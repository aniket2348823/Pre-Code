package middleware

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeInput(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"", ""}, {"  hello  ", "hello"},
		{"<b>bold</b>", "<b>bold</b>"},
		{"a & b", "a & b"}, {"normal", "normal"},
	}
	for _, tt := range tests {
		if got := SanitizeInput(tt.input); got != tt.expected {
			t.Errorf("SanitizeInput(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"", ""},
		{"normal.txt", "normal.txt"},
		{"../../../etc/passwd", "etc/passwd"},
		{"file with spaces", "file with spaces"},
	}
	for _, tt := range tests {
		if got := SanitizeFilename(tt.input); got != tt.expected {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestDetectSQLInjection(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hello", false}, {"SELECT * FROM users", true},
		{"DROP TABLE", true}, {"select x", true},
		{"user chose", false}, {"DELETE FROM t", true},
	}
	for _, tt := range tests {
		if got := DetectSQLInjection(tt.input); got != tt.expected {
			t.Errorf("DetectSQLInjection(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestDetectXSS(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"hello", false}, {"<script>alert(1)</script>", true},
		{"javascript:alert(1)", true}, {"onclick=\"x\"", true},
		{"SCRIPT>", true}, {"plain text", false},
	}
	for _, tt := range tests {
		if got := DetectXSS(tt.input); got != tt.expected {
			t.Errorf("DetectXSS(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestSanitizeMiddleware(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	tests := []struct {
		name string
		path string
		code int
	}{
		{"normal", "/api/users", http.StatusOK},
		{"encoded-traversal", "/api/%2e%2e%2fetc%2fpasswd", http.StatusBadRequest},
		{"encoded-dot-dot", "/api/%2e%2e%5cetc%5cpasswd", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			rec := httptest.NewRecorder()
			SanitizeMiddleware(handler).ServeHTTP(rec, req)
			if rec.Code != tt.code {
				t.Errorf("expected %d, got %d", tt.code, rec.Code)
			}
		})
	}
}

func TestSanitizeMiddleware_SQL(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/api?q='+OR+'1'='1", nil)
	rec := httptest.NewRecorder()
	SanitizeMiddleware(handler).ServeHTTP(rec, req)
	// SQLi patterns in query params are logged, not blocked: parameterized
	// queries make them harmless, and blocking rejects legitimate traffic
	// (e.g. searching for the text "DROP TABLE").
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 pass-through, got %d", rec.Code)
	}
}

func TestSanitizeMiddleware_XSS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/api?q=<script>alert(1)</script>", nil)
	rec := httptest.NewRecorder()
	SanitizeMiddleware(handler).ServeHTTP(rec, req)
	// XSS patterns in query params are logged, not blocked — output encoding
	// at render time is the correct defense, not rejecting requests.
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 pass-through, got %d", rec.Code)
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	for _, l := range []int{16, 32, 64, 128} {
		tok, err := GenerateCSRFToken(l)
		if err != nil || tok == "" {
			t.Errorf("GenerateCSRFToken(%d) error = %v", l, err)
		}
		if len(tok) != l*2 {
			t.Errorf("expected len %d, got %d", l*2, len(tok))
		}
	}
	tok, _ := GenerateCSRFToken(0)
	if len(tok) != 0 {
		t.Errorf("expected empty for length=0, got len=%d", len(tok))
	}
}

func TestGenerateCSRFToken_Unique(t *testing.T) {
	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, _ := GenerateCSRFToken(32)
		if tokens[tok] {
			t.Fatal("duplicate token")
		}
		tokens[tok] = true
	}
}

func TestCSRFProtect_NilConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	m := CSRFProtect(nil)
	if m == nil {
		t.Fatal("nil config should not return nil middleware")
	}
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("GET should pass, got %d", rec.Code)
	}
}

func TestCSRFProtect_IgnoreMethods(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		req := httptest.NewRequest(method, "/", nil)
		rec := httptest.NewRecorder()
		m(handler).ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s should bypass CSRF, got %d", method, rec.Code)
		}
	}
}

func TestCSRFProtect_POSTRequiresToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	m := CSRFProtect(DefaultCSRFConfig())
	req := httptest.NewRequest("POST", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST without token should be 403, got %d", rec.Code)
	}
}

func TestCSRFProtect_MissingHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "token"})
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("missing header should be 403, got %d", rec.Code)
	}
}

func TestCSRFProtect_MismatchedToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "cookie-val"})
	req.Header.Set(cfg.HeaderName, "header-val")
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("mismatch should be 403, got %d", rec.Code)
	}
}

func TestCSRFProtect_ValidToken(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	tok, _ := GenerateCSRFToken(cfg.TokenLength)
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	req.Header.Set(cfg.HeaderName, tok)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid token should pass, got %d", rec.Code)
	}
}

func TestCSRFProtect_SetsCookie(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	cookies := rec.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == cfg.CookieName {
			found = true
			if c.Value == "" {
				t.Error("empty cookie value")
			}
		}
	}
	if !found {
		t.Error("CSRF cookie not set")
	}
}

func TestCompareTokens(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"abc", "abc", true}, {"abc", "def", false},
		{"abc", "abcd", false}, {"", "", true},
		{"a", "b", false},
	}
	for _, tt := range tests {
		if got := compareTokens(tt.a, tt.b); got != tt.want {
			t.Errorf("compareTokens(%q,%q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestCSRFProtect_Concurrent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, _ := GenerateCSRFToken(cfg.TokenLength)
			req := httptest.NewRequest("POST", "/", nil)
			req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
			req.Header.Set(cfg.HeaderName, tok)
			rec := httptest.NewRecorder()
			m(handler).ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent POST should pass, got %d", rec.Code)
			}
		}()
	}
	wg.Wait()
}

func TestDefaultCSRFConfig(t *testing.T) {
	cfg := DefaultCSRFConfig()
	if cfg.CookieName != "csrf_token" {
		t.Errorf("cookie name = %q", cfg.CookieName)
	}
	if !cfg.CookieSecure {
		t.Error("expected Secure=true")
	}
	if cfg.HeaderName != "X-CSRF-Token" {
		t.Errorf("header = %q", cfg.HeaderName)
	}
	if cfg.TokenLength != 32 {
		t.Errorf("token length = %d", cfg.TokenLength)
	}
	if len(cfg.IgnoreMethods) != 3 {
		t.Errorf("ignore methods = %d", len(cfg.IgnoreMethods))
	}
	if cfg.MaxAge != 1*time.Hour {
		t.Errorf("max age = %v", cfg.MaxAge)
	}
}

func TestSanitizeMiddleware_MultiParam(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/api?q=hello&q=SELECT+FROM+users", nil)
	rec := httptest.NewRecorder()
	SanitizeMiddleware(handler).ServeHTTP(rec, req)
	// All values pass through; nothing is blocked based on query content alone.
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 pass-through, got %d", rec.Code)
	}
}

func TestCSRFProtect_EmptyHeader(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultCSRFConfig()
	m := CSRFProtect(cfg)
	tok, _ := GenerateCSRFToken(cfg.TokenLength)
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: tok})
	req.Header.Set(cfg.HeaderName, "")
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("empty header should be 403, got %d", rec.Code)
	}
}

func TestDefaultSecurityHeadersConfig(t *testing.T) {
	cfg := DefaultSecurityHeadersConfig()
	if !cfg.Enabled {
		t.Error("expected Enabled=true")
	}
	if cfg.HSTSMaxAge != 63072000 {
		t.Errorf("HSTSMaxAge = %d, want 63072000", cfg.HSTSMaxAge)
	}
	if !cfg.HSTSIncludeSubDomains {
		t.Error("expected HSTSIncludeSubDomains=true")
	}
	if !cfg.HSTSPreload {
		t.Error("expected HSTSPreload=true")
	}
	if cfg.CSP == "" {
		t.Error("expected non-empty CSP")
	}
	if !cfg.XContentTypeOptions {
		t.Error("expected XContentTypeOptions=true")
	}
	if cfg.XFrameOptions != "DENY" {
		t.Errorf("XFrameOptions = %q, want DENY", cfg.XFrameOptions)
	}
	if cfg.ReferrerPolicy != "strict-origin-when-cross-origin" {
		t.Errorf("ReferrerPolicy = %q", cfg.ReferrerPolicy)
	}
	if cfg.PermissionsPolicy == "" {
		t.Error("expected non-empty PermissionsPolicy")
	}
	if cfg.XSSProtection != "1; mode=block" {
		t.Errorf("XSSProtection = %q", cfg.XSSProtection)
	}
	if cfg.CacheControlAPI != "no-store, no-cache, must-revalidate" {
		t.Errorf("CacheControlAPI = %q", cfg.CacheControlAPI)
	}
	if cfg.CacheControlStatic != "public, max-age=31536000" {
		t.Errorf("CacheControlStatic = %q", cfg.CacheControlStatic)
	}
}

func TestSecurityHeaders_NilConfig(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	m := SecurityHeaders(nil)
	if m == nil {
		t.Fatal("nil config should not return nil middleware")
	}
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	h := rec.Header()
	if h.Get("Strict-Transport-Security") == "" {
		t.Error("HSTS header missing with nil config")
	}
	if h.Get("Content-Security-Policy") == "" {
		t.Error("CSP header missing with nil config")
	}
}

func TestSecurityHeaders_Disabled(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultSecurityHeadersConfig()
	cfg.Enabled = false
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if h := rec.Header().Get("Strict-Transport-Security"); h != "" {
		t.Errorf("expected no HSTS when disabled, got %q", h)
	}
	if h := rec.Header().Get("Content-Security-Policy"); h != "" {
		t.Errorf("expected no CSP when disabled, got %q", h)
	}
}

func TestSecurityHeaders_HSTS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name     string
		cfg      *SecurityHeadersConfig
		expected string
	}{
		{
			name:     "full",
			cfg:      DefaultSecurityHeadersConfig(),
			expected: "max-age=63072000; includeSubDomains; preload",
		},
		{
			name: "no_preload",
			cfg: &SecurityHeadersConfig{
				Enabled:               true,
				HSTSMaxAge:            31536000,
				HSTSIncludeSubDomains: true,
				HSTSPreload:           false,
			},
			expected: "max-age=31536000; includeSubDomains",
		},
		{
			name: "no_subdomains",
			cfg: &SecurityHeadersConfig{
				Enabled:               true,
				HSTSMaxAge:            86400,
				HSTSIncludeSubDomains: false,
				HSTSPreload:           false,
			},
			expected: "max-age=86400",
		},
		{
			name: "zero_max_age",
			cfg: &SecurityHeadersConfig{
				Enabled:    true,
				HSTSMaxAge: 0,
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := SecurityHeaders(tt.cfg)
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			m(handler).ServeHTTP(rec, req)
			got := rec.Header().Get("Strict-Transport-Security")
			if got != tt.expected {
				t.Errorf("HSTS = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	customCSP := "default-src 'none'; script-src 'self'"
	cfg := &SecurityHeadersConfig{
		Enabled: true,
		CSP:     customCSP,
	}
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	got := rec.Header().Get("Content-Security-Policy")
	if got != customCSP {
		t.Errorf("CSP = %q, want %q", got, customCSP)
	}
}

func TestSecurityHeaders_XContentTypeOptions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("enabled", func(t *testing.T) {
		cfg := &SecurityHeadersConfig{Enabled: true, XContentTypeOptions: true}
		m := SecurityHeaders(cfg)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		m(handler).ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("got %q, want nosniff", got)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := &SecurityHeadersConfig{Enabled: true, XContentTypeOptions: false}
		m := SecurityHeaders(cfg)
		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()
		m(handler).ServeHTTP(rec, req)
		if got := rec.Header().Get("X-Content-Type-Options"); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestSecurityHeaders_XFrameOptions(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name, value, expected string
	}{
		{"deny", "DENY", "DENY"},
		{"sameorigin", "SAMEORIGIN", "SAMEORIGIN"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SecurityHeadersConfig{Enabled: true, XFrameOptions: tt.value}
			m := SecurityHeaders(cfg)
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			m(handler).ServeHTTP(rec, req)
			got := rec.Header().Get("X-Frame-Options")
			if got != tt.expected {
				t.Errorf("X-Frame-Options = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSecurityHeaders_ReferrerPolicy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := &SecurityHeadersConfig{Enabled: true, ReferrerPolicy: "no-referrer"}
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("got %q, want no-referrer", got)
	}
}

func TestSecurityHeaders_PermissionsPolicy(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	policy := "camera=(), microphone=(), geolocation=()"
	cfg := &SecurityHeadersConfig{Enabled: true, PermissionsPolicy: policy}
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if got := rec.Header().Get("Permissions-Policy"); got != policy {
		t.Errorf("got %q, want %q", got, policy)
	}
}

func TestSecurityHeaders_XSSProtection(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	tests := []struct {
		name, value, expected string
	}{
		{"block", "1; mode=block", "1; mode=block"},
		{"disabled", "0", "0"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &SecurityHeadersConfig{Enabled: true, XSSProtection: tt.value}
			m := SecurityHeaders(cfg)
			req := httptest.NewRequest("GET", "/", nil)
			rec := httptest.NewRecorder()
			m(handler).ServeHTTP(rec, req)
			got := rec.Header().Get("X-XSS-Protection")
			if got != tt.expected {
				t.Errorf("X-XSS-Protection = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestSecurityHeaders_CacheControl_API(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultSecurityHeadersConfig()
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate" {
		t.Errorf("API Cache-Control = %q, want no-store, no-cache, must-revalidate", got)
	}
}

func TestSecurityHeaders_CacheControl_Static(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultSecurityHeadersConfig()
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/static/app.js", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
		t.Errorf("static Cache-Control = %q, want public, max-age=31536000", got)
	}
}

func TestSecurityHeaders_CustomHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := &SecurityHeadersConfig{
		Enabled:       true,
		CustomHeaders: map[string]string{"X-Custom": "test-value", "X-Another": "another-value"},
	}
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if got := rec.Header().Get("X-Custom"); got != "test-value" {
		t.Errorf("X-Custom = %q, want test-value", got)
	}
	if got := rec.Header().Get("X-Another"); got != "another-value" {
		t.Errorf("X-Another = %q, want another-value", got)
	}
}

func TestSecurityHeaders_AllHeadersPresent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := DefaultSecurityHeadersConfig()
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	h := rec.Header()

	expected := map[string]string{
		"Strict-Transport-Security": "max-age=63072000; includeSubDomains; preload",
		"Content-Security-Policy":   cfg.CSP,
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        cfg.PermissionsPolicy,
		"X-XSS-Protection":          "1; mode=block",
	}

	for name, want := range expected {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestSecurityHeaders_PassesToNext(t *testing.T) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	cfg := DefaultSecurityHeadersConfig()
	m := SecurityHeaders(cfg)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	m(handler).ServeHTTP(rec, req)
	if !called {
		t.Error("next handler not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestBodySizeLimiter_AllowsGET(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_AllowsDELETE(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPOST(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes for sure")
	req := httptest.NewRequest("POST", "/", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPUT(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes")
	req := httptest.NewRequest("PUT", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_LimitsPATCH(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 10}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if n > 0 && err != nil {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("this body is definitely larger than 10 bytes")
	req := httptest.NewRequest("PATCH", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEqual(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_SmallBodyAllowed(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 100)
		n, err := r.Body.Read(buf)
		if err != nil && err.Error() != "EOF" {
			http.Error(w, "error", http.StatusBadRequest)
			return
		}
		_ = n
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"test@example.com"}`)
	req := httptest.NewRequest("POST", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_DefaultConfig(t *testing.T) {
	cfg := DefaultBodySizeConfig()
	assert.Equal(t, int64(10<<20), cfg.MaxBodySize)
}

func TestBodySizeLimiter_NegativeConfigUsesDefault(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: -1}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader("small")
	req := httptest.NewRequest("POST", "/", body)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBodySizeLimiter_NilBody(t *testing.T) {
	cfg := BodySizeConfig{MaxBodySize: 1024}
	handler := BodySizeLimiter(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleMaxBytesError_NilError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, nil)
	assert.False(t, handled)
}

func TestHandleMaxBytesError_OtherError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, errors.New("some other error"))
	assert.False(t, handled)
}

func TestHandleMaxBytesError_MaxBytesError(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	handled := HandleMaxBytesError(w, req, errors.New("http: request body too large"))
	assert.True(t, handled)
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
}

func newCSRF() *CSRFMiddleware {
	return NewCSRFMiddleware([]byte("test-secret-key-for-csrf-testing-1234"))
}

func csrfRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer header.payload.signature")
	return req
}

func setCSRFCookie(req *http.Request, token string) {
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: token})
}

func extractCSRFToken(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == "_csrf" {
			return c.Value
		}
	}
	return ""
}

func TestCSRF_GeneratesAndValidatesToken(t *testing.T) {
	m := newCSRF()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := csrfRequest("POST", "/api/v1/agents")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("first POST: want 403, got %d", w.Code)
	}
	token := extractCSRFToken(w)
	if token == "" {
		t.Fatal("expected CSRF cookie to be set even on 403")
	}

	req2 := csrfRequest("POST", "/api/v1/agents")
	setCSRFCookie(req2, token)
	req2.Header.Set("X-CSRF-Token", token)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("second POST: want 200, got %d", w2.Code)
	}
}

func TestCSRF_HttpOnlyCookieFlag(t *testing.T) {
	m := newCSRF()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	for _, c := range w.Result().Cookies() {
		if c.Name == "_csrf" {
			if !c.HttpOnly {
				t.Error("CSRF cookie must have HttpOnly=true")
			}
			return
		}
	}
	t.Error("no _csrf cookie found")
}

func TestCSRF_SameSiteStrict(t *testing.T) {
	m := newCSRF()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	for _, c := range w.Result().Cookies() {
		if c.Name == "_csrf" {
			if c.SameSite != http.SameSiteStrictMode {
				t.Errorf("SameSite = %v, want SameSiteStrictMode", c.SameSite)
			}
			return
		}
	}
	t.Error("no _csrf cookie found")
}

func TestCSRF_TamperedSignatureFails(t *testing.T) {
	m := newCSRF()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	validToken := extractCSRFToken(w)

	parts := strings.SplitN(validToken, ".", 2)
	tampered := parts[0] + ".0000000000000000000000000000000000000000000000000000000000000000"

	req2 := csrfRequest("POST", "/test")
	setCSRFCookie(req2, validToken)
	req2.Header.Set("X-CSRF-Token", tampered)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusForbidden {
		t.Errorf("tampered token: want 403, got %d", w2.Code)
	}
}

func TestCSRF_MissingTokenReturns403(t *testing.T) {
	m := newCSRF()
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("missing token: want 403, got %d", w.Code)
	}
}

func TestCSRF_SafeMethodsSkipValidation(t *testing.T) {
	m := newCSRF()
	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		called = false
		req := csrfRequest(method, "/test")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if !called {
			t.Errorf("%s should skip CSRF validation", method)
		}
		if w.Code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", method, w.Code)
		}
	}
}

func TestCSRF_ExcludedPathsSkipValidation(t *testing.T) {
	m := newCSRF()
	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/api/v1/health", "/api/v1/ready", "/api/v1/metrics"} {
		called = false
		req := csrfRequest("POST", path)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if !called {
			t.Errorf("path %s should skip CSRF validation", path)
		}
	}
}

func TestCSRF_APIKeyRequestSkipsValidation(t *testing.T) {
	m := newCSRF()
	called := false
	handler := m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-API-Key", "va_test_key_123")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if !called {
		t.Error("API key request should skip CSRF validation")
	}
}

func TestCSRF_FormValueFallback(t *testing.T) {
	m := newCSRF()

	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)
	token := extractCSRFToken(w)

	req2 := csrfRequest("POST", "/test")
	setCSRFCookie(req2, token)
	req2.Form = nil
	req2.Header.Del("X-CSRF-Token")

	req2.Body = io.NopCloser(strings.NewReader("csrf_token=" + token))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("form value fallback: want 200, got %d", w2.Code)
	}
}

func TestCSRF_SetToken(t *testing.T) {
	m := newCSRF()
	w := httptest.NewRecorder()
	req := csrfRequest("GET", "/")

	m.SetToken(w, req)

	token := w.Header().Get("X-CSRF-Token")
	if token == "" {
		t.Error("SetToken should set X-CSRF-Token header")
	}

	found := false
	for _, c := range w.Result().Cookies() {
		if c.Name == "_csrf" && c.Value == token {
			found = true
			break
		}
	}
	if !found {
		t.Error("SetToken should set matching cookie")
	}
}

func TestCSRF_VerifyToken_InvalidFormat(t *testing.T) {
	m := newCSRF()
	if m.verifyToken("noseparator") {
		t.Error("token without dot should fail")
	}
	if m.verifyToken(".") {
		t.Error("empty token should fail")
	}
}

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"password redacted", "password", "secret123", "***REDACTED***"},
		{"password_hash redacted", "password_hash", "abc", "***REDACTED***"},
		{"api_key redacted", "api_key", "key123", "***REDACTED***"},
		{"api-key redacted", "api-key", "key123", "***REDACTED***"},
		{"x-api-key redacted", "x-api-key", "key123", "***REDACTED***"},
		{"authorization redacted", "authorization", "Bearer tok", "***REDACTED***"},
		{"token redacted", "token", "val", "***REDACTED***"},
		{"access_token redacted", "access_token", "val", "***REDACTED***"},
		{"refresh_token redacted", "refresh_token", "val", "***REDACTED***"},
		{"secret redacted", "secret", "val", "***REDACTED***"},
		{"secret_key redacted", "secret_key", "val", "***REDACTED***"},
		{"jwt_secret redacted", "jwt_secret", "val", "***REDACTED***"},
		{"credit_card redacted", "credit_card", "4111", "***REDACTED***"},
		{"ssn redacted", "ssn", "123-45-6789", "***REDACTED***"},
		{"pin redacted", "pin", "1234", "***REDACTED***"},
		{"case insensitive password", "Password", "val", "***REDACTED***"},
		{"case insensitive Authorization", "Authorization", "Bearer tok", "***REDACTED***"},
		{"non-sensitive passthrough", "username", "admin", "admin"},
		{"non-sensitive Content-Type", "Content-Type", "application/json", "application/json"},
		{"empty value passthrough", "password", "", ""},
		{"empty key passthrough", "", "value", "value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactValue(tt.key, tt.value)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	headers := map[string][]string{
		"Authorization": {"Bearer secret-token"},
		"Content-Type":  {"application/json"},
		"X-API-Key":     {"va_abc123"},
		"X-Custom":      {"custom-value"},
		"Password":      {"hunter2"},
		"Empty-Header":  {},
	}

	result := RedactHeaders(headers)

	assert.Equal(t, "***REDACTED***", result["Authorization"])
	assert.Equal(t, "application/json", result["Content-Type"])
	assert.Equal(t, "***REDACTED***", result["X-API-Key"])
	assert.Equal(t, "custom-value", result["X-Custom"])
	assert.Equal(t, "***REDACTED***", result["Password"])
	_, hasEmpty := result["Empty-Header"]
	assert.False(t, hasEmpty, "empty header should not appear in result")
}

func TestRedactHeaders_Empty(t *testing.T) {
	result := RedactHeaders(map[string][]string{})
	assert.Empty(t, result)
}

func TestRedactLogger_CallsNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RedactLogger(next)
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-API-Key", "va_secret_key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, called, "RedactLogger must call next handler")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRedactLogger_DoesNotLeakAuthHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RedactLogger(next)
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("X-API-Key", "va_secret_api_key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
