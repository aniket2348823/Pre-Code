package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/pkg/response"
)

var (
	sqlInjectionPattern  = regexp.MustCompile(`(?i)(\b(SELECT|INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|EXEC|EXECUTE|UNION|DECLARE|CAST|CONVERT|OR)\b\s)`)
	xssPattern           = regexp.MustCompile(`(?i)(<script|<\/script|script\s*>|javascript:|on\w+\s*=|<iframe|<object|<embed|<applet)`)
	pathTraversalPattern = regexp.MustCompile(`(\.\.\/|\.\\.\\|%2e%2e%2f|%2e%2e\/|%2e%2e%5c)`)
)

// SanitizeInput sanitizes user input to prevent injection attacks.
func SanitizeInput(input string) string {
	if input == "" {
		return input
	}
	input = strings.TrimSpace(input)

	input = strings.Map(func(r rune) rune {
		if r == 0 || (r < 32 && r != '\n' && r != '\r' && r != '\t') {
			return -1
		}
		return r
	}, input)
	return input
}

// SanitizeFilename sanitizes a filename to prevent path traversal.
func SanitizeFilename(filename string) string {
	if filename == "" {
		return filename
	}
	filename = pathTraversalPattern.ReplaceAllString(filename, "")
	filename = strings.ReplaceAll(filename, "\x00", "")
	filename = strings.Trim(filename, "/\\")
	return filename
}

// DetectSQLInjection checks if input contains SQL injection patterns.
func DetectSQLInjection(input string) bool {
	return sqlInjectionPattern.MatchString(input)
}

// DetectXSS checks if input contains XSS patterns.
func DetectXSS(input string) bool {
	return xssPattern.MatchString(input)
}

// SanitizeMiddleware returns middleware that sanitizes common injection patterns.
func SanitizeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		path := r.URL.Path
		if r.URL.RawPath != "" {
			path = r.URL.RawPath
		}
		if pathTraversalPattern.MatchString(path) {
			// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
			slog.Warn("security: injection attempt blocked", "type", "path_traversal", "remote", r.RemoteAddr, "path", path)
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		// SQLi/XSS patterns in query params are logged but NOT blocked: they are
		// harmless against parameterized queries, and blocking rejects legitimate
		// traffic (e.g. searching for the text "DROP TABLE"). Only path
		// traversal (above) and null bytes are hard-blocked.
		for key, values := range r.URL.Query() {
			for _, v := range values {
				if DetectSQLInjection(v) {
					// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
					slog.Warn("security: potential sql injection in query param", "remote", r.RemoteAddr, "param", key)
				}
				if DetectXSS(v) {
					slog.Warn("security: potential xss in query param", "remote", r.RemoteAddr, "param", key)
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// CSRFConfig holds CSRF protection configuration.
type CSRFConfig struct {
	CookieName     string
	CookieDomain   string
	CookieSecure   bool
	CookieHTTPOnly bool
	HeaderName     string
	TokenLength    int
	MaxAge         time.Duration
	IgnoreMethods  []string
}

// DefaultCSRFConfig returns production-ready CSRF configuration.
func DefaultCSRFConfig() *CSRFConfig {
	return &CSRFConfig{
		CookieName:     "csrf_token",
		CookieSecure:   true,
		CookieHTTPOnly: true, // CSRF tokens are also delivered via X-CSRF-Token header; no JS cookie access needed
		HeaderName:     "X-CSRF-Token",
		TokenLength:    32,
		MaxAge:         1 * time.Hour,
		IgnoreMethods:  []string{"GET", "HEAD", "OPTIONS"},
	}
}

// GenerateCSRFToken creates a cryptographically secure random token.
func GenerateCSRFToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// CSRFProtect returns middleware that validates CSRF tokens.
func CSRFProtect(cfg *CSRFConfig) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultCSRFConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method := strings.ToUpper(r.Method)
			isIgnored := false
			for _, m := range cfg.IgnoreMethods {
				if method == m {
					isIgnored = true
					break
				}
			}

			cookieToken, _ := r.Cookie(cfg.CookieName)
			headerToken := r.Header.Get(cfg.HeaderName)

			if isIgnored {
				if cookieToken != nil && headerToken != "" {
					if !compareTokens(cookieToken.Value, headerToken) {
						http.Error(w, "CSRF token mismatch", http.StatusForbidden)
						return
					}
				}

				// Reuse an existing cookie token instead of regenerating on every
				// GET — regenerating invalidates previously issued header tokens and
				// breaks SPA flows that fetch a token then POST after another GET.
				token := ""
				if cookieToken != nil && cookieToken.Value != "" {
					token = cookieToken.Value
				} else {
					var genErr error
					token, genErr = GenerateCSRFToken(cfg.TokenLength)
					if genErr != nil {
						http.Error(w, "failed to generate CSRF token", http.StatusInternalServerError)
						return
					}
				}
				http.SetCookie(w, &http.Cookie{
					Name:     cfg.CookieName,
					Value:    token,
					Path:     "/",
					Domain:   cfg.CookieDomain,
					Secure:   cfg.CookieSecure,
					HttpOnly: cfg.CookieHTTPOnly,
					MaxAge:   int(cfg.MaxAge.Seconds()),
					SameSite: http.SameSiteLaxMode,
				})
				w.Header().Set(cfg.HeaderName, token)
				next.ServeHTTP(w, r)
				return
			}

			if cookieToken == nil || cookieToken.Value == "" {
				http.Error(w, "missing CSRF token cookie", http.StatusForbidden)
				return
			}

			if headerToken == "" {
				http.Error(w, "missing CSRF token header", http.StatusForbidden)
				return
			}

			if !compareTokens(cookieToken.Value, headerToken) {
				http.Error(w, "CSRF token mismatch", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// compareTokens performs constant-time comparison to prevent timing attacks.
func compareTokens(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// SecurityHeadersConfig holds security header middleware configuration.
type SecurityHeadersConfig struct {
	Enabled               bool
	HSTSMaxAge            int
	HSTSIncludeSubDomains bool
	HSTSPreload           bool
	CSP                   string
	XContentTypeOptions   bool
	XFrameOptions         string
	ReferrerPolicy        string
	PermissionsPolicy     string
	XSSProtection         string
	CacheControlAPI       string
	CacheControlStatic    string
	CustomHeaders         map[string]string
}

// DefaultSecurityHeadersConfig returns production-ready security header configuration.
func DefaultSecurityHeadersConfig() *SecurityHeadersConfig {
	return &SecurityHeadersConfig{
		Enabled:               true,
		HSTSMaxAge:            63072000,
		HSTSIncludeSubDomains: true,
		HSTSPreload:           true,
		CSP:                   "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
		XContentTypeOptions:   true,
		XFrameOptions:         "DENY",
		ReferrerPolicy:        "strict-origin-when-cross-origin",
		PermissionsPolicy:     "camera=(), microphone=(), geolocation=(), payment=()",
		XSSProtection:         "1; mode=block",
		CacheControlAPI:       "no-store, no-cache, must-revalidate",
		CacheControlStatic:    "public, max-age=31536000",
		CustomHeaders:         make(map[string]string),
	}
}

// SecurityHeaders returns middleware that sets security-related HTTP headers.
func SecurityHeaders(cfg *SecurityHeadersConfig) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultSecurityHeadersConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			if cfg.HSTSMaxAge > 0 {
				hsts := fmt.Sprintf("max-age=%d", cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSubDomains {
					hsts += "; includeSubDomains"
				}
				if cfg.HSTSPreload {
					hsts += "; preload"
				}
				w.Header().Set("Strict-Transport-Security", hsts)
			}

			if cfg.CSP != "" {
				w.Header().Set("Content-Security-Policy", cfg.CSP)
			}

			if cfg.XContentTypeOptions {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			if cfg.XFrameOptions != "" {
				w.Header().Set("X-Frame-Options", cfg.XFrameOptions)
			}

			if cfg.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			if cfg.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
			}

			if cfg.XSSProtection != "" {
				w.Header().Set("X-XSS-Protection", cfg.XSSProtection)
			}

			isAPI := strings.HasPrefix(r.URL.Path, "/api/")
			if isAPI && cfg.CacheControlAPI != "" {
				w.Header().Set("Cache-Control", cfg.CacheControlAPI)
			} else if !isAPI && cfg.CacheControlStatic != "" {
				w.Header().Set("Cache-Control", cfg.CacheControlStatic)
			}

			for k, v := range cfg.CustomHeaders {
				w.Header().Set(k, v)
			}

			next.ServeHTTP(w, r)
		})
	}
}

// BodySizeConfig configures the body size limiter middleware.
type BodySizeConfig struct {
	MaxBodySize int64 // Maximum request body size in bytes (default 10MB)
}

// DefaultBodySizeConfig returns a BodySizeConfig with a 10MB default.
func DefaultBodySizeConfig() BodySizeConfig {
	return BodySizeConfig{
		MaxBodySize: 10 << 20,
	}
}

// BodySizeLimiter returns middleware that limits request body size for POST/PUT/PATCH.
// Returns 413 Payload Too Large if the body exceeds the configured max.
func BodySizeLimiter(cfg BodySizeConfig) func(http.Handler) http.Handler {
	if cfg.MaxBodySize <= 0 {
		cfg = DefaultBodySizeConfig()
	}
	maxSize := cfg.MaxBodySize

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				if r.Body != nil {
					r.Body = http.MaxBytesReader(w, r.Body, maxSize)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HandleMaxBytesError checks if an error is from http.MaxBytesReader and writes a 413 response.
// Returns true if the error was handled (caller should return).
func HandleMaxBytesError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "http: request body too large") {
		response.JSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"code":  "INFRA_003",
			"error": "request body too large",
		})
		return true
	}
	return false
}

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

		if isAPIKeyRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		for _, method := range m.safeMethods {
			if strings.EqualFold(r.Method, method) {
				next.ServeHTTP(w, r)
				return
			}
		}

		for _, path := range m.excludedPaths {
			if strings.HasPrefix(r.URL.Path, path) {
				next.ServeHTTP(w, r)
				return
			}
		}

		token := m.getOrCreateToken(r)
		if token == "" {
			http.Error(w, `{"error":"failed to generate CSRF token"}`, http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     m.cookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   true,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   3600,
		})

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
	if token == "" {
		http.Error(w, `{"error":"failed to generate CSRF token"}`, http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     m.cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   3600,
	})
	w.Header().Set(m.headerName, token)
}

// getOrCreateToken returns the existing valid signed token or generates a new one.
func (m *CSRFMiddleware) getOrCreateToken(r *http.Request) string {

	if cookie, err := r.Cookie(m.cookieName); err == nil && cookie.Value != "" && m.verifyToken(cookie.Value) {
		return cookie.Value
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	token := hex.EncodeToString(b)

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

	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(rawToken))
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sigHex), []byte(expected))
}

// isAPIKeyRequest checks if the request uses API key authentication.
// API key consumers (VS Code extension, MCP server, CLI) can't have CSRF cookies,
// so CSRF validation should be skipped for them.
func isAPIKeyRequest(r *http.Request) bool {

	if r.Header.Get("X-API-Key") != "" {
		return true
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := parts[1]

			// The literal "local-dev" development key is an API key, not a JWT
			// (matches extractAPIKey in auth.go). API key consumers cannot hold
			// CSRF cookies, so CSRF validation is skipped for them.
			if token == "local-dev" || (!strings.Contains(token, ".") && strings.Contains(token, "_")) {
				return true
			}
		}
	}
	return false
}

// sensitiveFields are keys whose values should be redacted in logs.
var sensitiveFields = map[string]bool{
	"password":      true,
	"password_hash": true,
	"api_key":       true,
	"api-key":       true,
	"x-api-key":     true,
	"authorization": true,
	"token":         true,
	"access_token":  true,
	"refresh_token": true,
	"secret":        true,
	"secret_key":    true,
	"jwt_secret":    true,
	"credit_card":   true,
	"ssn":           true,
	"pin":           true,
}

// RedactLogger returns middleware that logs requests with sensitive fields redacted.
func RedactLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
			"content_type", r.Header.Get("Content-Type"),

			"has_auth", r.Header.Get("Authorization") != "",
			"has_api_key", r.Header.Get("X-API-Key") != "",
		)
		next.ServeHTTP(w, r)
	})
}

// RedactValue returns "***REDACTED***" for sensitive fields, or the original value.
func RedactValue(key, value string) string {
	lower := strings.ToLower(key)
	if sensitiveFields[lower] {
		if value != "" {
			return "***REDACTED***"
		}
	}
	return value
}

// RedactHeaders returns a map of HTTP headers with sensitive values redacted.
func RedactHeaders(headers map[string][]string) map[string]string {
	result := make(map[string]string, len(headers))
	for k, vals := range headers {
		if len(vals) == 0 {
			continue
		}
		result[k] = RedactValue(k, vals[0])
	}
	return result
}
