package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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

	// First POST: no cookie → generates token, but no submitted token → 403
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

	// Second POST: cookie + header token → validates
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

	// Get a valid token
	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	validToken := extractCSRFToken(w)

	// Tamper with the signature portion
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

	// Get a valid token
	req := csrfRequest("POST", "/test")
	w := httptest.NewRecorder()
	m.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(w, req)
	token := extractCSRFToken(w)

	// Submit via form value instead of header
	req2 := csrfRequest("POST", "/test")
	setCSRFCookie(req2, token)
	req2.Form = nil
	req2.Header.Del("X-CSRF-Token")

	// Parse form with csrf_token field
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
