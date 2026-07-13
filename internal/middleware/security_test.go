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
	req := httptest.NewRequest("POST", "/", nil)
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
