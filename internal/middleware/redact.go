package middleware

import (
	"log/slog"
	"net/http"
	"strings"
)

// sensitiveFields are keys whose values should be redacted in logs.
var sensitiveFields = map[string]bool{
	"password":       true,
	"password_hash":  true,
	"api_key":        true,
	"api-key":        true,
	"x-api-key":      true,
	"authorization":  true,
	"token":          true,
	"access_token":   true,
	"refresh_token":  true,
	"secret":         true,
	"secret_key":     true,
	"jwt_secret":     true,
	"credit_card":    true,
	"ssn":            true,
	"pin":            true,
}

// RedactLogger returns middleware that logs requests with sensitive fields redacted.
func RedactLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Log request with sensitive headers redacted.
		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr,
			"user_agent", r.UserAgent(),
			"content_type", r.Header.Get("Content-Type"),
			// Never log Authorization or X-API-Key headers.
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
