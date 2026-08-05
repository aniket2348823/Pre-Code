//go:build go1.18

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
)

func FuzzExtractBearerToken(f *testing.F) {
	f.Add("Bearer eyJhbGciOiJIUzI1NiJ9.test.signature")
	f.Add("")
	f.Add("Bearer ")
	f.Add("Basic dXNlcjpwYXNz")
	f.Add("Bearer va_abc123")
	f.Add("Bearer eyJhbGciOiJIUzI1NiJ9.")
	f.Add(strings.Repeat("a", 10000))

	f.Fuzz(func(t *testing.T, input string) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", input)
		_ = ExtractBearerToken(req)
	})
}

func FuzzHashKey(f *testing.F) {
	f.Add("test-key")
	f.Add("")
	f.Add(strings.Repeat("x", 10000))

	f.Fuzz(func(t *testing.T, key string) {
		h := hashKey(key)
		if h == "" {
			t.Fatal("hashKey should never return empty")
		}
		if len(h) != 64 {
			t.Fatalf("expected 64-char hex, got %d", len(h))
		}
		h2 := hashKey(key)
		if h != h2 {
			t.Fatal("hashKey is not deterministic")
		}
	})
}

func FuzzExtractAPIKey(f *testing.F) {
	f.Add("va_abc123def456")
	f.Add("")
	f.Add("Bearer va_test123")
	f.Add("X-API-Key: va_key123")

	f.Fuzz(func(t *testing.T, key string) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", key)
		_ = extractAPIKey(req)
	})
}

func FuzzExtractAPIKey_AuthHeader(f *testing.F) {
	f.Add("Bearer va_abc123")
	f.Add("Bearer eyJhbGciOiJIUzI1NiJ9.test.sig")
	f.Add("Bearer ")
	f.Add("")

	f.Fuzz(func(t *testing.T, authHeader string) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", authHeader)
		_ = extractAPIKey(req)
	})
}

func FuzzAPIKeyAuthError(f *testing.F) {
	f.Add("invalid or unknown API key")
	f.Add("API key has expired")
	f.Add("")

	f.Fuzz(func(t *testing.T, msg string) {
		err := &APIKeyAuthError{msg: msg}
		if err.Error() != msg {
			t.Fatalf("expected %q, got %q", msg, err.Error())
		}
	})
}

func FuzzFingerprintBindingMiddleware(f *testing.F) {
	f.Add(true, true, "10.0.0.1", "Mozilla/5.0", "10.0.0.1", "Mozilla/5.0")
	f.Add(true, false, "10.0.0.1", "", "10.0.0.1", "Mozilla/5.0")
	f.Add(false, true, "", "Mozilla/5.0", "10.0.0.1", "Mozilla/5.0")
	f.Add(false, false, "", "", "", "")
	f.Add(true, true, "192.168.1.1", "Chrome/90", "10.0.0.1", "Firefox/1.0")

	f.Fuzz(func(t *testing.T, bindIP, bindUA bool, tokenIP, tokenUA, reqIP, reqUA string) {
		cfg := &FingerprintBindingConfig{
			BindToIP:        bindIP,
			BindToUserAgent: bindUA,
		}

		fp := auth.ComputeFingerprint(tokenIP, tokenUA)
		jwtSvc := auth.NewJWT(&config.AuthConfig{
			JWTSecret:          "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration:      time.Hour,
			JWTBindToIP:        bindIP,
			JWTBindToUserAgent: bindUA,
		})

		token, err := jwtSvc.GenerateTokenWithFingerprint("user-1", "test@example.com", "user", "org-1", fp)
		if err != nil {
			return
		}
		_, _ = jwtSvc.ValidateToken(token)

		handler := FingerprintBindingMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/api/data", nil)
		req.RemoteAddr = reqIP + ":9090"
		req.Header.Set("User-Agent", reqUA)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	})
}

func FuzzJWTRotationMiddleware(f *testing.F) {
	f.Add("user-123", "test@example.com", "admin", "org-456", "/auth/refresh")
	f.Add("user-456", "", "user", "", "/users/me")
	f.Add("", "", "", "", "/api/data")

	f.Fuzz(func(t *testing.T, userID, email, role, orgID, path string) {
		jwtSvc := auth.NewJWT(&config.AuthConfig{
			JWTSecret:     "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration: time.Hour,
		})

		rotationCfg := &JWTRotationConfig{
			MaxTokenAge:       15 * time.Minute,
			RotateOnEndpoints: []string{"/auth/refresh"},
		}

		claims := &auth.Claims{
			UserID: userID,
			Email:  email,
			Role:   role,
			OrgID:  orgID,
		}

		handler := JWTRotationMiddleware(rotationCfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", path, nil)
		req.RemoteAddr = "10.0.0.1:9090"
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	})
}
