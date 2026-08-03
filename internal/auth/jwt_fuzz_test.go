package auth

import (
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

func FuzzJWTGenerateAndValidate(f *testing.F) {
	f.Add("user-123", "test@example.com", "user", "org-456")
	f.Add("", "", "", "")
	f.Add("very-long-user-id", "email@domain.co.uk", "admin", "org-789")

	f.Fuzz(func(t *testing.T, userID, email, role, orgID string) {
		cfg := &config.AuthConfig{JWTSecret: "test-secret-key-for-fuzzing-32bytes!", JWTExpiration: time.Hour}
		jwt := NewJWT(cfg)
		token, err := jwt.GenerateToken(userID, email, role, orgID)
		if err != nil {
			return
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}

		claims, err := jwt.ValidateToken(token)
		if err != nil {
			t.Fatalf("valid token failed validation: %v", err)
		}
		if claims.UserID != userID {
			t.Errorf("userID mismatch: got %q, want %q", claims.UserID, userID)
		}
	})
}

func FuzzHashPasswordFuzz(f *testing.F) {
	f.Add("password1234567890")
	f.Add("")
	f.Add("a")
	f.Add("very-long-password-with-special-chars")

	f.Fuzz(func(t *testing.T, password string) {
		hash, err := HashPassword(password)
		if err != nil {
			return
		}
		if !CheckPassword(password, hash) {
			t.Fatal("password should match its hash")
		}
	})
}

func FuzzValidateTokenFuzz(f *testing.F) {
	f.Add("")
	f.Add("invalid")
	f.Add("eyJhbGciOiJIUzI1NiJ9.test")

	f.Fuzz(func(t *testing.T, input string) {
		cfg := &config.AuthConfig{JWTSecret: "test-secret-key-for-fuzzing-32bytes!", JWTExpiration: time.Hour}
		jwt := NewJWT(cfg)
		_, _ = jwt.ValidateToken(input)
	})
}
