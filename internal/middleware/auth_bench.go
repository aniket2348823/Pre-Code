package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
)

func BenchmarkJWTGenerateToken(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "bench-secret-key-for-benchmarking-32+",
		JWTExpiration: time.Hour,
	})

	b.Run("basic", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.GenerateToken("user-1", "bench@test.com", "user", "org-1")
		}
	})

	b.Run("with_fingerprint", func(b *testing.B) {
		fp := auth.ComputeFingerprint("192.168.1.1", "BenchmarkAgent/1.0")
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.GenerateTokenWithFingerprint("user-1", "bench@test.com", "user", "org-1", fp)
		}
	})
}

func BenchmarkJWTValidateToken(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "bench-secret-key-for-benchmarking-32+",
		JWTExpiration: time.Hour,
	})
	token, _ := jwtSvc.GenerateToken("user-1", "bench@test.com", "admin", "org-1")

	b.Run("valid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateToken(token)
		}
	})

	b.Run("invalid", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateToken("invalid.token.string")
		}
	})

	b.Run("tampered", func(b *testing.B) {
		tampered := token[:len(token)-5] + "XXXXX"
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateToken(tampered)
		}
	})
}

func BenchmarkJWTValidateTokenWithFingerprint(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:          "bench-secret-key-for-benchmarking-32+",
		JWTExpiration:      time.Hour,
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})
	fp := auth.ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
	token, _ := jwtSvc.GenerateTokenWithFingerprint("user-1", "bench@test.com", "user", "org-1", fp)

	b.Run("match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateTokenWithFingerprint(token, "192.168.1.1", "Mozilla/5.0")
		}
	})

	b.Run("mismatch_ip", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateTokenWithFingerprint(token, "10.0.0.1", "Mozilla/5.0")
		}
	})
}

func BenchmarkComputeFingerprint(b *testing.B) {
	b.Run("short_strings", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			auth.ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
		}
	})

	b.Run("long_strings", func(b *testing.B) {
		ip := "192.168.100.200.300.400.500"
		ua := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			auth.ComputeFingerprint(ip, ua)
		}
	})

	b.Run("empty", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			auth.ComputeFingerprint("", "")
		}
	})
}

func BenchmarkExtractBearerToken(b *testing.B) {
	b.Run("valid", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.test.signature")
		for i := 0; i < b.N; i++ {
			ExtractBearerToken(req)
		}
	})

	b.Run("no_auth_header", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		for i := 0; i < b.N; i++ {
			ExtractBearerToken(req)
		}
	})

	b.Run("api_key", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer va_abc123def456")
		for i := 0; i < b.N; i++ {
			ExtractBearerToken(req)
		}
	})
}

func BenchmarkHashKey(b *testing.B) {
	b.Run("short_key", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			hashKey("va_test_key_123")
		}
	})

	b.Run("long_key", func(b *testing.B) {
		longKey := make([]byte, 1024)
		for i := range longKey {
			longKey[i] = 'a'
		}
		s := string(longKey)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			hashKey(s)
		}
	})
}

func BenchmarkExtractAPIKey(b *testing.B) {
	b.Run("x_api_key_header", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", "va_abc123def456ghi789")
		for i := 0; i < b.N; i++ {
			extractAPIKey(req)
		}
	})

	b.Run("bearer_va_prefix", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer va_abc123def456ghi789")
		for i := 0; i < b.N; i++ {
			extractAPIKey(req)
		}
	})

	b.Run("no_key", func(b *testing.B) {
		req := httptest.NewRequest("GET", "/test", nil)
		for i := 0; i < b.N; i++ {
			extractAPIKey(req)
		}
	})
}

func BenchmarkFingerprintBindingMiddleware(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:          "bench-secret-key-for-benchmarking-32+",
		JWTExpiration:      time.Hour,
		JWTBindToIP:        true,
		JWTBindToUserAgent: true,
	})

	fp := auth.ComputeFingerprint("10.0.0.1", "BenchmarkAgent/1.0")
	token, _ := jwtSvc.GenerateTokenWithFingerprint("user-1", "bench@test.com", "admin", "org-1", fp)
	claims, _ := jwtSvc.ValidateToken(token)

	cfg := &FingerprintBindingConfig{
		BindToIP:        true,
		BindToUserAgent: true,
	}

	handler := FingerprintBindingMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.Run("match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/api/data", nil)
			req.RemoteAddr = "10.0.0.1:9090"
			req.Header.Set("User-Agent", "BenchmarkAgent/1.0")
			req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("no_claims", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/api/data", nil)
			req.RemoteAddr = "10.0.0.1:9090"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkJWTRotationMiddleware(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "bench-secret-key-for-benchmarking-32+",
		JWTExpiration: time.Hour,
	})

	rotationCfg := &JWTRotationConfig{
		MaxTokenAge:       15 * time.Minute,
		RotateOnEndpoints: []string{"/auth/refresh"},
	}

	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "bench@test.com",
		Role:   "admin",
		OrgID:  "org-1",
	}

	handler := JWTRotationMiddleware(rotationCfg, jwtSvc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	b.Run("no_rotation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/api/data", nil)
			req.RemoteAddr = "10.0.0.1:9090"
			req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})

	b.Run("with_rotation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := httptest.NewRequest("GET", "/auth/refresh", nil)
			req.RemoteAddr = "10.0.0.1:9090"
			req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}
	})
}

func BenchmarkTokenRevocation(b *testing.B) {
	jwtSvc := auth.NewJWT(&config.AuthConfig{
		JWTSecret:     "bench-secret-key-for-benchmarking-32+",
		JWTExpiration: time.Hour,
	})

	b.Run("revoke", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			jwtSvc.RevokeAllUserTokens("user-revoke")
		}
	})

	b.Run("validate_after_revoke", func(b *testing.B) {
		token, _ := jwtSvc.GenerateToken("user-revoked", "r@test.com", "user", "org-1")
		jwtSvc.RevokeAllUserTokens("user-revoked")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateToken(token)
		}
	})

	b.Run("validate_unrevoked", func(b *testing.B) {
		token, _ := jwtSvc.GenerateToken("user-not-revoked", "nr@test.com", "user", "org-1")
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = jwtSvc.ValidateToken(token)
		}
	})
}
