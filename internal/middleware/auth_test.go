package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
)

func TestDefaultJWTRotationConfig(t *testing.T) {
	cfg := DefaultJWTRotationConfig()
	assert.NotNil(t, cfg)
	assert.Equal(t, 15*time.Minute, cfg.MaxTokenAge)
	assert.Contains(t, cfg.RotateOnEndpoints, "/auth/refresh")
	assert.Contains(t, cfg.RotateOnEndpoints, "/users/me")
}

func TestJWTRotationMiddleware_NoClaimsPassesThrough(t *testing.T) {
	cfg := DefaultJWTRotationConfig()
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	nextCalled := false
	handler := JWTRotationMiddleware(cfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Empty(t, w.Header().Get("X-New-Token"))
}

func TestJWTRotationMiddleware_NilConfigUsesDefaults(t *testing.T) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	nextCalled := false
	handler := JWTRotationMiddleware(nil, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
}

func TestJWTRotationMiddleware_RotatesOnRefreshEndpoint(t *testing.T) {
	cfg := DefaultJWTRotationConfig()
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	handler := JWTRotationMiddleware(cfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "admin",
		OrgID:  "org-1",
	}
	req := httptest.NewRequest("GET", "/auth/refresh", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-New-Token"))
	assert.Equal(t, "true", w.Header().Get("X-Token-Rotated"))
}

func TestJWTRotationMiddleware_NoRotationOnOtherEndpoint(t *testing.T) {
	cfg := DefaultJWTRotationConfig()
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	handler := JWTRotationMiddleware(cfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "admin",
		OrgID:  "org-1",
	}
	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("X-New-Token"))
	assert.Empty(t, w.Header().Get("X-Token-Rotated"))
}

func TestJWTRotationMiddleware_RotatesOnUsersMeEndpoint(t *testing.T) {
	cfg := DefaultJWTRotationConfig()
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	handler := JWTRotationMiddleware(cfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
	}
	req := httptest.NewRequest("GET", "/users/me", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.NotEmpty(t, w.Header().Get("X-New-Token"))
}

func TestRequireJWTRefresh_NoClaimsReturns401(t *testing.T) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	handler := RequireJWTRefresh(jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/auth/refresh", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireJWTRefresh_RotatesToken(t *testing.T) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "test-secret-key-that-is-at-least-32-bytes!",
		JWTExpiration: time.Hour,
	})

	handler := RequireJWTRefresh(jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "admin",
		OrgID:  "org-1",
	}
	req := httptest.NewRequest("POST", "/auth/refresh", nil)
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Header().Get("X-New-Token"))
}

func TestWrapIntoChiMiddleware_NoAPIKeyPassesThrough(t *testing.T) {
	aka := NewAPIKeyAuth(nil)

	nextCalled := false
	handler := aka.WrapIntoChiMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "no API key should pass through")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestExtractBearerToken_ExtraSpaces(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer my.jwt.token.here")
	got := ExtractBearerToken(req)
	assert.Equal(t, "my.jwt.token.here", got)
}

func TestFingerprintBindingMiddleware_BindingMatch(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-bytes!"
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:          secret,
		JWTExpiration:      time.Hour,
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	bindingCfg := NewFingerprintBindingConfig(&config.AuthConfig{
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	handler := FingerprintBindingMiddleware(bindingCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create token with matching fingerprint
	fp := auth.ComputeFingerprint("10.0.0.1", "TestAgent/1.0")
	token, err := jwtSvc.GenerateTokenWithFingerprint("user-1", "test@example.com", "admin", "org-1", fp)
	require.NoError(t, err)

	claims, err := jwtSvc.ValidateTokenWithFingerprint(token, "10.0.0.1", "TestAgent/1.0")
	require.NoError(t, err)

	req := httptest.NewRequest("GET", "/api/data", nil)
	req.RemoteAddr = "10.0.0.1:9090"
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestFingerprintBindingMiddleware_BindingMismatch(t *testing.T) {
	secret := "test-secret-key-that-is-at-least-32-bytes!"
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:          secret,
		JWTExpiration:      time.Hour,
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	bindingCfg := NewFingerprintBindingConfig(&config.AuthConfig{
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	handler := FingerprintBindingMiddleware(bindingCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Create token with different fingerprint
	fp := auth.ComputeFingerprint("192.168.1.1", "OldAgent/1.0")
	token, err := jwtSvc.GenerateTokenWithFingerprint("user-1", "test@example.com", "admin", "org-1", fp)
	require.NoError(t, err)

	claims, err := jwtSvc.ValidateTokenWithFingerprint(token, "192.168.1.1", "OldAgent/1.0")
	require.NoError(t, err)

	// Request comes from different IP
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.RemoteAddr = "10.0.0.99:9090"
	req.Header.Set("User-Agent", "NewAgent/2.0")
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestFingerprintBindingMiddleware_APIKeySkipped(t *testing.T) {
	bindingCfg := NewFingerprintBindingConfig(&config.AuthConfig{
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	handler := FingerprintBindingMiddleware(bindingCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// API key auth — no fingerprint
	claims := &auth.Claims{
		UserID:   "user-1",
		IsAPIKey: true,
	}
	req := httptest.NewRequest("GET", "/api/data", nil)
	req.RemoteAddr = "10.0.0.1:9090"
	req.Header.Set("User-Agent", "TestAgent/1.0")
	req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestFingerprintBindingMiddleware_NilConfigPassThrough(t *testing.T) {
	handler := FingerprintBindingMiddleware(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestFingerprintBindingMiddleware_DisabledPassThrough(t *testing.T) {
	bindingCfg := &FingerprintBindingConfig{
		BindToIP:        false,
		BindToUserAgent: false,
	}

	handler := FingerprintBindingMiddleware(bindingCfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestNewFingerprintBindingConfig(t *testing.T) {
	cfg := NewFingerprintBindingConfig(&config.AuthConfig{
		JWTBindToIP:        true,
		JWTBindToUserAgent: false,
	})
	assert.True(t, cfg.BindToIP)
	assert.False(t, cfg.BindToUserAgent)
}
