package middleware

import (
	"crypto/rand"
	"encoding/hex"
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
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		for key, values := range r.URL.Query() {
			for _, v := range values {
				if DetectSQLInjection(v) {
					http.Error(w, "invalid query parameter: "+key, http.StatusBadRequest)
					return
				}
				if DetectXSS(v) {
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
		return "", err
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
			for _, m := range cfg.IgnoreMethods {
				if method == m {
					next.ServeHTTP(w, r)
					return
				}
			}

			cookieToken, err := r.Cookie(cfg.CookieName)
			if err != nil || cookieToken.Value == "" {
				token, genErr := GenerateCSRFToken(cfg.TokenLength)
				if genErr != nil {
					http.Error(w, "failed to generate CSRF token", http.StatusInternalServerError)
					return
				}
				cookieToken = &http.Cookie{
					Name:     cfg.CookieName,
					Value:    token,
					Path:     "/",
					Domain:   cfg.CookieDomain,
					Secure:   cfg.CookieSecure,
					HttpOnly: cfg.CookieHTTPOnly,
					MaxAge:   int(cfg.MaxAge.Seconds()),
					SameSite: http.SameSiteLaxMode,
				}
				http.SetCookie(w, cookieToken)
				w.Header().Set(cfg.HeaderName, token)
			}

			headerToken := r.Header.Get(cfg.HeaderName)
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
	if len(a) != len(b) {
		return false
	}
	result := 0
	for i := 0; i < len(a); i++ {
		result |= int(a[i]) ^ int(b[i])
	}
	return result == 0
}
