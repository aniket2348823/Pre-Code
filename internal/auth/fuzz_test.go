package auth

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vigilagent/vigilagent/internal/config"
)

func FuzzJWTGenerateValidateRoundtrip(f *testing.F) {
	f.Add("user-1", "test@example.com", "admin", "org-1")
	f.Add("", "", "", "")
	f.Add("a", "b@c.d", "role", "o")
	f.Add(strings.Repeat("x", 10000), "long@user.com", strings.Repeat("r", 5000), "org")
	f.Add("\x00\x01\x02", "user@exam ple.com", "rôle-ïnïtial", "örç-1")

	f.Fuzz(func(t *testing.T, userID, email, role, orgID string) {
		// JWT uses JSON serialization which replaces invalid UTF-8 — skip those
		if !utf8.ValidString(userID) || !utf8.ValidString(email) || !utf8.ValidString(role) || !utf8.ValidString(orgID) {
			return
		}
		j := newTestJWT()
		token, err := j.GenerateToken(userID, email, role, orgID)
		if err != nil {
			return
		}
		claims, err := j.ValidateToken(token)
		if err != nil {
			t.Fatalf("valid token failed validation: %v", err)
		}
		if claims.UserID != userID {
			t.Errorf("UserID mismatch: got %q, want %q", claims.UserID, userID)
		}
		if claims.Email != email {
			t.Errorf("Email mismatch: got %q, want %q", claims.Email, email)
		}
		if claims.Role != role {
			t.Errorf("Role mismatch: got %q, want %q", claims.Role, role)
		}
		if claims.OrgID != orgID {
			t.Errorf("OrgID mismatch: got %q, want %q", claims.OrgID, orgID)
		}
	})
}

func FuzzPasswordHashRoundtrip(f *testing.F) {
	f.Add("password123")
	f.Add("")
	f.Add("a")
	f.Add("very-long-password-that-exceeds-normal-length-expectations-for-security-testing")
	f.Add("\x00\x01\x02\x03")
	f.Add(strings.Repeat("p", 72))     // bcrypt max input
	f.Add(strings.Repeat("p", 73))     // bcrypt truncation boundary
	f.Add(strings.Repeat("p", 100000)) // massive input

	f.Fuzz(func(t *testing.T, password string) {
		hash, err := HashPassword(password)
		if err != nil {
			return
		}
		if !CheckPassword(password, hash) {
			t.Errorf("password should match its hash")
		}
		// bcrypt truncates at 72 bytes — only test "wrong password" when under that limit
		if len([]byte(password)) < 72 {
			if CheckPassword(password+"x", hash) {
				t.Errorf("wrong password should not match")
			}
		}
	})
}

func FuzzJWTValidateGarbage(f *testing.F) {
	f.Add("")
	f.Add("invalid")
	f.Add("eyJhbGciOiJIUzI1NiJ9.test")
	f.Add("not.a.jwt.at.all")
	f.Add(strings.Repeat("a", 10000))
	f.Add("<script>alert(1)</script>")
	f.Add("'; DROP TABLE users;--")

	f.Fuzz(func(t *testing.T, input string) {
		j := newTestJWT()
		_, _ = j.ValidateToken(input)
	})
}

func FuzzPasswordHashConsistency(f *testing.F) {
	f.Add("hello")
	f.Add("")
	f.Add(strings.Repeat("x", 50))

	f.Fuzz(func(t *testing.T, password string) {
		hash1, err1 := HashPassword(password)
		hash2, err2 := HashPassword(password)
		if err1 != nil || err2 != nil {
			return
		}
		// Same password should produce different hashes (random salt)
		if hash1 == hash2 {
			t.Errorf("bcrypt should produce different hashes for same input")
		}
		// Both should verify correctly
		if !CheckPassword(password, hash1) {
			t.Errorf("first hash should verify")
		}
		if !CheckPassword(password, hash2) {
			t.Errorf("second hash should verify")
		}
	})
}

func FuzzAPIKeyGenerateVerify(f *testing.F) {
	f.Add("va_")
	f.Add("")
	f.Add("custom-prefix-")

	f.Fuzz(func(t *testing.T, prefix string) {
		svc := NewAPIKeyService(prefix)
		plaintext, hashed, returnedPrefix, err := svc.GenerateKey()
		if err != nil {
			return
		}
		if plaintext == "" {
			t.Fatal("plaintext should not be empty")
		}
		if hashed == "" {
			t.Fatal("hash should not be empty")
		}
		if !svc.ValidatePrefix(plaintext) {
			t.Errorf("plaintext should have correct prefix")
		}
		if returnedPrefix == "" {
			t.Fatal("prefix should not be empty")
		}
		if !svc.VerifyKey(plaintext, hashed) {
			t.Errorf("generated key should verify against its hash")
		}
		if svc.VerifyKey(plaintext+"x", hashed) {
			t.Errorf("modified key should not verify")
		}
		if svc.VerifyKey("", hashed) {
			t.Errorf("empty key should not verify")
		}
	})
}

func FuzzNewJWTSecretLength(f *testing.F) {
	f.Add(32)
	f.Add(0)
	f.Add(1)
	f.Add(31)
	f.Add(100)
	f.Add(10000)

	f.Fuzz(func(t *testing.T, secretLen int) {
		if secretLen < 0 || secretLen > 10000 {
			return
		}
		secret := strings.Repeat("a", secretLen)
		defer func() {
			if r := recover(); r != nil {
				// NewJWT panics for short secrets — expected behavior
				if secretLen >= 32 {
					t.Errorf("NewJWT panicked with valid secret length %d: %v", secretLen, r)
				}
			}
		}()
		cfg := &config.AuthConfig{
			JWTSecret:     secret,
			JWTExpiration: time.Hour,
		}
		j := NewJWT(cfg)
		token, err := j.GenerateToken("u", "e@e.com", "user", "o")
		if err != nil {
			return
		}
		claims, err := j.ValidateToken(token)
		if err != nil {
			t.Fatalf("valid token failed validation: %v", err)
		}
		if claims.UserID != "u" {
			t.Errorf("UserID mismatch")
		}
	})
}
