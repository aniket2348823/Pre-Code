package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

// CSRFMiddleware protects state-changing endpoints from cross-site request forgery.
// It uses HMAC-signed tokens: the server signs a random token, stores it in a cookie,
// and verifies the signature when the client sends it back in a header.
type CSRFMiddleware struct {
	cookieName    string
	headerName    string
	secret        []byte
	safeMethods   []string
	excludedPaths []string
}

// NewCSRFMiddleware creates a new CSRF middleware with HMAC-signed tokens.
func NewCSRFMiddleware(secret []byte) *CSRFMiddleware {
	return &CSRFMiddleware{
		cookieName:    "_csrf",
		headerName:    "X-CSRF-Token",
		secret:        secret,
		safeMethods:   []string{"GET", "HEAD", "OPTIONS", "TRACE"},
		excludedPaths: []string{"/api/v1/health", "/api/v1/ready", "/api/v1/metrics"},
	}
}

// Middleware returns a chi-compatible CSRF middleware.
// Skips validation for API key consumers (they can't have cookies).
func (m *CSRFMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip API key consumers — they can't have CSRF cookies
		if isAPIKeyRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		// Skip safe methods
		for _, method := range m.safeMethods {
			if strings.EqualFold(r.Method, method) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Skip excluded paths
		for _, path := range m.excludedPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		// Always set CSRF cookie on state-changing requests
		token := m.getOrCreateToken(r)
		http.SetCookie(w, &http.Cookie{
			Name:     m.cookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: false, // JS needs to read this for the header
			SameSite: http.SameSiteStrictMode,
			MaxAge:   3600,
		})

		// Validate token from header or form field
		submitted := r.Header.Get(m.headerName)
		if submitted == "" {
			submitted = r.FormValue("csrf_token")
		}

		if submitted == "" || !m.verifyToken(submitted) {
			slog.Warn("CSRF validation failed",
				"path", r.URL.Path,
				"method", r.Method,
				"remote", r.RemoteAddr,
			)
			http.Error(w, `{"error":"CSRF token missing or invalid"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SetToken sets a CSRF token on the response for SPA clients.
func (m *CSRFMiddleware) SetToken(w http.ResponseWriter, r *http.Request) {
	token := m.getOrCreateToken(r)
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})
	w.Header().Set(m.headerName, token)
}

// getOrCreateToken returns the existing valid signed token or generates a new one.
func (m *CSRFMiddleware) getOrCreateToken(r *http.Request) string {
	// Check if client already has a valid CSRF cookie
	if cookie, err := r.Cookie(m.cookieName); err == nil && cookie.Value != "" && m.verifyToken(cookie.Value) {
		return cookie.Value
	}
	// Generate new random token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		slog.Error("CSRF: failed to generate token", "error", err)
		return ""
	}
	token := hex.EncodeToString(b)

	// Sign the token with HMAC so we can verify it server-side
	sig := m.signToken(token)
	return token + "." + sig
}

// signToken computes HMAC-SHA256 signature for the token.
func (m *CSRFMiddleware) signToken(token string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyToken checks that the submitted token has a valid HMAC signature.
func (m *CSRFMiddleware) verifyToken(token string) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	rawToken, sigHex := parts[0], parts[1]

	// Compute expected HMAC signature
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(rawToken))
	expected := hex.EncodeToString(mac.Sum(nil))

	// Constant-time comparison to prevent timing attacks
	return hmac.Equal([]byte(sigHex), []byte(expected))
}

// isAPIKeyRequest checks if the request uses API key authentication.
// API key consumers (VS Code extension, MCP server, CLI) can't have CSRF cookies,
// so CSRF validation should be skipped for them.
func isAPIKeyRequest(r *http.Request) bool {
	// Check X-API-Key header
	if r.Header.Get("X-API-Key") != "" {
		return true
	}
	// Check Authorization header for API key pattern (Bearer va_xxx or Bearer xxx_yyy)
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := parts[1]
			// API keys contain underscore and no dots; JWTs contain dots
			if !strings.Contains(token, ".") && strings.Contains(token, "_") {
				return true
			}
		}
	}
	return false
}
