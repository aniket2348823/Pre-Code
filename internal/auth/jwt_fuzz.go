//go:build go1.18

package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

func FuzzJWTGenerateAndValidate_Roundtrip(f *testing.F) {
	f.Add("user-123", "test@example.com", "user", "org-456", "")
	f.Add("", "", "", "", "")
	f.Add("very-long-user-id", "email@domain.co.uk", "admin", "org-789", "fingerprint-hash")
	f.Add("user-with-unicode", "test+tag@domain.com", "super-admin", "org-000", "fp-abc123")
	f.Add("x", "a@b.c", "u", "o", "f")

	f.Fuzz(func(t *testing.T, userID, email, role, orgID, fingerprint string) {
		cfg := &config.AuthConfig{
			JWTSecret:     "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration: time.Hour,
		}
		j := NewJWT(cfg)

		var token string
		var err error
		if fingerprint != "" {
			token, err = j.GenerateTokenWithFingerprint(userID, email, role, orgID, fingerprint)
		} else {
			token, err = j.GenerateToken(userID, email, role, orgID)
		}
		if err != nil {
			return
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}

		claims, err := j.ValidateToken(token)
		if err != nil {
			t.Fatalf("valid token failed validation: %v", err)
		}
		if claims.UserID != userID {
			t.Errorf("userID mismatch: got %q, want %q", claims.UserID, userID)
		}
		if claims.Email != email {
			t.Errorf("email mismatch: got %q, want %q", claims.Email, email)
		}
		if claims.Role != role {
			t.Errorf("role mismatch: got %q, want %q", claims.Role, role)
		}
	})
}

func FuzzJWTValidateToken_ArbitraryInput(f *testing.F) {
	f.Add("")
	f.Add("invalid")
	f.Add("eyJhbGciOiJIUzI1NiJ9.test")
	f.Add("eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyX2lkIjoidXNlci0xIn0.")
	f.Add(".")
	f.Add("...")

	f.Fuzz(func(t *testing.T, tokenStr string) {
		cfg := &config.AuthConfig{
			JWTSecret:     "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration: time.Hour,
		}
		j := NewJWT(cfg)
		_, _ = j.ValidateToken(tokenStr)
	})
}

func FuzzComputeFingerprint(f *testing.F) {
	f.Add("192.168.1.1", "Mozilla/5.0")
	f.Add("", "")
	f.Add("10.0.0.1", "")
	f.Add("", "Chrome/90.0")
	f.Add(strings.Repeat("x", 10000), strings.Repeat("y", 10000))

	f.Fuzz(func(t *testing.T, ip, ua string) {
		fp := ComputeFingerprint(ip, ua)
		if len(fp) != 64 {
			t.Fatalf("expected 64-char hex, got %d", len(fp))
		}
		fp2 := ComputeFingerprint(ip, ua)
		if fp != fp2 {
			t.Fatal("ComputeFingerprint is not deterministic")
		}
	})
}

func FuzzJWTWithFingerprintBinding(f *testing.F) {
	f.Add("user-1", "a@test.com", "admin", "org-1", "192.168.1.1", "Mozilla/5.0", "192.168.1.1", "Mozilla/5.0")
	f.Add("user-1", "a@test.com", "admin", "org-1", "192.168.1.1", "Mozilla/5.0", "10.0.0.1", "Chrome/90")
	f.Add("u", "", "", "", "", "", "", "")

	f.Fuzz(func(t *testing.T, userID, email, role, orgID, tokenIP, tokenUA, reqIP, reqUA string) {
		cfg := &config.AuthConfig{
			JWTSecret:          "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration:      time.Hour,
			JWTBindToIP:        true,
			JWTBindToUserAgent: true,
		}
		j := NewJWT(cfg)

		fp := ComputeFingerprint(tokenIP, tokenUA)
		token, err := j.GenerateTokenWithFingerprint(userID, email, role, orgID, fp)
		if err != nil {
			return
		}
		_, _ = j.ValidateTokenWithFingerprint(token, reqIP, reqUA)
	})
}

func FuzzJWTTamperedClaims(f *testing.F) {
	f.Add("user-1", "test@example.com", "admin")
	f.Add("", "", "")
	f.Add("admin", "admin@admin.com", "superadmin")

	f.Fuzz(func(t *testing.T, origUser, origEmail, tamperUser string) {
		cfg := &config.AuthConfig{
			JWTSecret:     "test-secret-key-for-fuzzing-32bytes!",
			JWTExpiration: time.Hour,
		}
		j := NewJWT(cfg)

		token, err := j.GenerateToken(origUser, origEmail, "user", "org-1")
		if err != nil {
			return
		}
		_ = tamperUser

		claims, err := j.ValidateToken(token)
		if err != nil {
			return
		}
		if claims.UserID != origUser {
			t.Errorf("userID mismatch after roundtrip: got %q, want %q", claims.UserID, origUser)
		}
	})
}
