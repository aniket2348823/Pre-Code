package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
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
		name string; path string; code int
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
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestSanitizeMiddleware_XSS(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest("GET", "/api?q=<script>alert(1)</script>", nil)
	rec := httptest.NewRecorder()
	SanitizeMiddleware(handler).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
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
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for SQL in second param, got %d", rec.Code)
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

// --- Security Headers Tests ---

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
		"X-XSS-Protection":         "1; mode=block",
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
