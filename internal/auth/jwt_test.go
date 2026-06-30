package auth

import (
	"context"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

func newTestJWT() *JWT {
	cfg := &config.AuthConfig{
		JWTSecret:       "test-secret-key-for-unit-tests",
		JWTExpiration:   15 * time.Minute,
	}
	return NewJWT(cfg)
}

func TestGenerateToken(t *testing.T) {
	jwt := newTestJWT()

	t.Run("generates a non-empty token", func(t *testing.T) {
		token, err := jwt.GenerateToken("user-1", "test@example.com", "user", "org-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}
	})

	t.Run("token can be parsed back to valid claims", func(t *testing.T) {
		token, _ := jwt.GenerateToken("user-1", "a@test.com", "user", "org-1")
		claims, err := jwt.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}
		if claims.UserID != "user-1" {
			t.Errorf("expected UserID=user-1, got %s", claims.UserID)
		}
		if claims.Email != "a@test.com" {
			t.Errorf("expected Email=a@test.com, got %s", claims.Email)
		}
	})
}

func TestValidateToken(t *testing.T) {
	jwt := newTestJWT()

	t.Run("valid token returns claims", func(t *testing.T) {
		token, err := jwt.GenerateToken("user-1", "test@example.com", "admin", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		claims, err := jwt.ValidateToken(token)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if claims.UserID != "user-1" {
			t.Errorf("expected UserID=user-1, got %s", claims.UserID)
		}
		if claims.Email != "test@example.com" {
			t.Errorf("expected Email=test@example.com, got %s", claims.Email)
		}
		if claims.Role != "admin" {
			t.Errorf("expected Role=admin, got %s", claims.Role)
		}
		if claims.OrgID != "org-1" {
			t.Errorf("expected OrgID=org-1, got %s", claims.OrgID)
		}
		if claims.Issuer != "vigilagent" {
			t.Errorf("expected Issuer=vigilagent, got %s", claims.Issuer)
		}
		if claims.Subject != "user-1" {
			t.Errorf("expected Subject=user-1, got %s", claims.Subject)
		}
	})

	t.Run("tampered token fails validation", func(t *testing.T) {
		token, _ := jwt.GenerateToken("user-1", "test@example.com", "user", "org-1")
		// Tamper with the token
		tampered := token[:len(token)-5] + "XXXXX"
		_, err := jwt.ValidateToken(tampered)
		if err == nil {
			t.Fatal("expected error for tampered token")
		}
	})

	t.Run("wrong secret fails validation", func(t *testing.T) {
		otherJWT := &JWT{
			secret:     []byte("wrong-secret-key"),
			expiration: 15 * time.Minute,
		}
		token, _ := jwt.GenerateToken("user-1", "test@example.com", "user", "org-1")
		_, err := otherJWT.ValidateToken(token)
		if err == nil {
			t.Fatal("expected error for token signed with wrong secret")
		}
	})

	t.Run("empty token fails validation", func(t *testing.T) {
		_, err := jwt.ValidateToken("")
		if err == nil {
			t.Fatal("expected error for empty token")
		}
	})

	t.Run("expired token fails validation", func(t *testing.T) {
		expiredJWT := &JWT{
			secret:     []byte("test-secret-key-for-unit-tests"),
			expiration: -1 * time.Hour, // already expired
		}
		token, err := expiredJWT.GenerateToken("user-1", "test@example.com", "user", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}
		_, err = jwt.ValidateToken(token)
		if err == nil {
			t.Fatal("expected error for expired token")
		}
	})
}

func TestClaimsFromContext(t *testing.T) {
	t.Run("returns claims when present", func(t *testing.T) {
		claims := &Claims{UserID: "user-1", Email: "test@example.com", Role: "admin"}
		ctx := ContextWithClaims(context.Background(), claims)

		got, ok := ClaimsFromContext(ctx)
		if !ok {
			t.Fatal("expected claims to be found in context")
		}
		if got.UserID != "user-1" {
			t.Errorf("expected UserID=user-1, got %s", got.UserID)
		}
		if got.Email != "test@example.com" {
			t.Errorf("expected Email=test@example.com, got %s", got.Email)
		}
	})

	t.Run("returns false when no claims in context", func(t *testing.T) {
		_, ok := ClaimsFromContext(context.Background())
		if ok {
			t.Fatal("expected false when no claims in context")
		}
	})
}
