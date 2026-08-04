package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// --- Input Sanitization ---

var (
	sqlInjectionPattern = regexp.MustCompile(`(?i)(\b(SELECT|INSERT|UPDATE|DELETE|DROP|CREATE|ALTER|EXEC|EXECUTE|UNION|DECLARE|CAST|CONVERT|OR)\b\s)`)
	xssPattern          = regexp.MustCompile(`(?i)(<script|<\/script|script\s*>|javascript:|on\w+\s*=|<iframe|<object|<embed|<applet)`)
	pathTraversalPattern = regexp.MustCompile(`(\.\.\/|\.\\.\\|%2e%2e%2f|%2e%2e\/|%2e%2e%5c)`)
)

// SanitizeInput sanitizes user input to prevent injection attacks.
func SanitizeInput(input string) string {
	if input == "" {
		return input
	}
	input = strings.TrimSpace(input)
	// Strip null bytes and control characters (except \n, \r, \t)
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
		// Use RawPath to detect encoded attacks that Go's URL decoding has already
		// resolved in r.URL.Path. This catches payloads like %2e%2e%2f or %2e%2e%5c.
		path := r.URL.Path
		if r.URL.RawPath != "" {
			path = r.URL.RawPath
		}
		if pathTraversalPattern.MatchString(path) {
			slog.Warn("security: injection attempt blocked", "type", "path_traversal", "remote", r.RemoteAddr, "path", path)
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		for key, values := range r.URL.Query() {
			for _, v := range values {
				if DetectSQLInjection(v) {
					slog.Warn("security: injection attempt blocked", "type", "sql_injection", "remote", r.RemoteAddr, "param", key, "value", v)
					http.Error(w, "invalid query parameter: "+key, http.StatusBadRequest)
					return
				}
				if DetectXSS(v) {
					slog.Warn("security: injection attempt blocked", "type", "xss", "remote", r.RemoteAddr, "param", key, "value", v)
					http.Error(w, "invalid query parameter: "+key, http.StatusBadRequest)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// --- CSRF Protection ---

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
		CookieHTTPOnly: false,
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

				token, genErr := GenerateCSRFToken(cfg.TokenLength)
				if genErr != nil {
					http.Error(w, "failed to generate CSRF token", http.StatusInternalServerError)
					return
				}
				http.SetCookie(w, &http.Cookie{
					Name:     cfg.CookieName,
					Value:    token,
					Path:     "/",
					Domain:   cfg.CookieDomain,
					Secure:   cfg.CookieSecure || true,
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

// --- Security Headers ---

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

			// HSTS
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

			// Content-Security-Policy
			if cfg.CSP != "" {
				w.Header().Set("Content-Security-Policy", cfg.CSP)
			}

			// X-Content-Type-Options
			if cfg.XContentTypeOptions {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}

			// X-Frame-Options
			if cfg.XFrameOptions != "" {
				w.Header().Set("X-Frame-Options", cfg.XFrameOptions)
			}

			// Referrer-Policy
			if cfg.ReferrerPolicy != "" {
				w.Header().Set("Referrer-Policy", cfg.ReferrerPolicy)
			}

			// Permissions-Policy
			if cfg.PermissionsPolicy != "" {
				w.Header().Set("Permissions-Policy", cfg.PermissionsPolicy)
			}

			// X-XSS-Protection
			if cfg.XSSProtection != "" {
				w.Header().Set("X-XSS-Protection", cfg.XSSProtection)
			}

			// Cache-Control: use API policy for /api/ paths, static for everything else
			isAPI := strings.HasPrefix(r.URL.Path, "/api/")
			if isAPI && cfg.CacheControlAPI != "" {
				w.Header().Set("Cache-Control", cfg.CacheControlAPI)
			} else if !isAPI && cfg.CacheControlStatic != "" {
				w.Header().Set("Cache-Control", cfg.CacheControlStatic)
			}

			// Custom headers
			for k, v := range cfg.CustomHeaders {
				w.Header().Set(k, v)
			}

			next.ServeHTTP(w, r)
		})
	}
}
