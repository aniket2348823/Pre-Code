package auth

import (
	"context"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/vigilagent/vigilagent/internal/config"
)

func newTestJWT() *JWT {
	cfg := &config.AuthConfig{
		JWTSecret:     "test-secret-key-for-unit-tests-32+",
		JWTExpiration: 15 * time.Minute,
		JWTAudience:   "test-audience",
	}
	return NewJWT(cfg)
}

func newTestJWTWithBinding(bindIP, bindUA bool) *JWT {
	cfg := &config.AuthConfig{
		JWTSecret:          "test-secret-key-for-unit-tests-32+",
		JWTExpiration:      15 * time.Minute,
		JWTAudience:        "test-audience",
		JWTBindToIP:        bindIP,
		JWTBindToUserAgent: bindUA,
	}
	return NewJWT(cfg)
}

func TestGenerateToken(t *testing.T) {
	svc := newTestJWT()

	t.Run("generates a non-empty token", func(t *testing.T) {
		token, err := svc.GenerateToken("user-1", "test@example.com", "user", "org-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if token == "" {
			t.Fatal("expected non-empty token")
		}
	})

	t.Run("token can be parsed back to valid claims", func(t *testing.T) {
		token, _ := svc.GenerateToken("user-1", "a@test.com", "user", "org-1")
		claims, err := svc.ValidateToken(token)
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

	t.Run("token contains correct audience", func(t *testing.T) {
		token, _ := svc.GenerateToken("user-1", "a@test.com", "user", "org-1")
		claims, err := svc.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}
		if len(claims.Audience) != 1 || claims.Audience[0] != "test-audience" {
			t.Errorf("expected Audience=[test-audience], got %v", claims.Audience)
		}
	})
}

func TestValidateToken(t *testing.T) {
	svc := newTestJWT()

	t.Run("valid token returns claims", func(t *testing.T) {
		token, err := svc.GenerateToken("user-1", "test@example.com", "admin", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		claims, err := svc.ValidateToken(token)
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
		token, _ := svc.GenerateToken("user-1", "test@example.com", "user", "org-1")
		tampered := token[:len(token)-5] + "XXXXX"
		_, err := svc.ValidateToken(tampered)
		if err == nil {
			t.Fatal("expected error for tampered token")
		}
	})

	t.Run("wrong secret fails validation", func(t *testing.T) {
		otherJWT := &JWT{
			secret:     []byte("wrong-secret-key-for-testing-32!"),
			expiration: 15 * time.Minute,
		}
		token, _ := svc.GenerateToken("user-1", "test@example.com", "user", "org-1")
		_, err := otherJWT.ValidateToken(token)
		if err == nil {
			t.Fatal("expected error for token signed with wrong secret")
		}
	})

	t.Run("empty token fails validation", func(t *testing.T) {
		_, err := svc.ValidateToken("")
		if err == nil {
			t.Fatal("expected error for empty token")
		}
	})

	t.Run("expired token fails validation", func(t *testing.T) {
		expiredJWT := &JWT{
			secret:     []byte("test-secret-key-for-unit-tests-32+"),
			expiration: -1 * time.Hour,
		}
		token, err := expiredJWT.GenerateToken("user-1", "test@example.com", "user", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}
		_, err = svc.ValidateToken(token)
		if err == nil {
			t.Fatal("expected error for expired token")
		}
	})

	t.Run("wrong audience fails validation", func(t *testing.T) {
		wrongAudJWT := &JWT{
			secret:     []byte("test-secret-key-for-unit-tests-32+"),
			expiration: 15 * time.Minute,
			audience:   "wrong-audience",
		}
		token, _ := svc.GenerateToken("user-1", "test@example.com", "user", "org-1")
		_, err := wrongAudJWT.ValidateToken(token)
		if err == nil {
			t.Fatal("expected error for token with wrong audience")
		}
	})

	t.Run("missing audience fails validation", func(t *testing.T) {
		noAudJWT := &JWT{
			secret:     []byte("test-secret-key-for-unit-tests-32+"),
			expiration: 15 * time.Minute,
			audience:   "required-audience",
		}
		claims := &Claims{
			UserID: "user-1",
			Email:  "test@example.com",
			Role:   "user",
			OrgID:  "org-1",
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "vigilagent",
				Subject:   "user-1",
				IssuedAt:  jwt.NewNumericDate(time.Now()),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, err := token.SignedString(noAudJWT.secret)
		if err != nil {
			t.Fatalf("failed to sign token: %v", err)
		}
		_, err = noAudJWT.ValidateToken(tokenStr)
		if err == nil {
			t.Fatal("expected error for token with missing audience")
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

func TestValidateToken_NonHMACMethod(t *testing.T) {
	svc := newTestJWT()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"u"}`))
	tokenStr := header + "." + payload + ".dummysignature"
	_, err := svc.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for non-HMAC token")
	}
}

func TestValidateToken_WrongIssuer(t *testing.T) {
	svc := newTestJWT()

	claims := &Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "wrong-issuer",
			Subject:   "user-1",
			Audience:  jwt.ClaimStrings{"test-audience"},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(svc.secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = svc.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for token with wrong issuer")
	}
}

func TestValidateToken_NoIssuer(t *testing.T) {
	svc := newTestJWT()

	claims := &Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-1",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Audience:  jwt.ClaimStrings{"test-audience"},
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString(svc.secret)
	if err != nil {
		t.Fatalf("failed to sign token: %v", err)
	}

	_, err = svc.ValidateToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for token with no issuer")
	}
}

func TestFingerprintBinding(t *testing.T) {
	t.Run("token with fingerprint validates when fingerprint matches", func(t *testing.T) {
		j := newTestJWTWithBinding(true, true)
		fp := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
		token, err := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}
		claims, err := j.ValidateTokenWithFingerprint(token, "192.168.1.1", "Mozilla/5.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if claims.UserID != "user-1" {
			t.Errorf("expected UserID=user-1, got %s", claims.UserID)
		}
	})

	t.Run("token with fingerprint fails when IP changes", func(t *testing.T) {
		j := newTestJWTWithBinding(true, true)
		fp := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
		token, _ := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		_, err := j.ValidateTokenWithFingerprint(token, "10.0.0.1", "Mozilla/5.0")
		if err != ErrFingerprintMismatch {
			t.Fatalf("expected ErrFingerprintMismatch, got %v", err)
		}
	})

	t.Run("token with fingerprint fails when User-Agent changes", func(t *testing.T) {
		j := newTestJWTWithBinding(true, true)
		fp := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
		token, _ := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		_, err := j.ValidateTokenWithFingerprint(token, "192.168.1.1", "Chrome/90.0")
		if err != ErrFingerprintMismatch {
			t.Fatalf("expected ErrFingerprintMismatch, got %v", err)
		}
	})

	t.Run("binding disabled — fingerprint mismatch ignored", func(t *testing.T) {
		j := newTestJWTWithBinding(false, false)
		fp := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
		token, _ := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		_, err := j.ValidateTokenWithFingerprint(token, "10.0.0.1", "Changed-Agent")
		if err != nil {
			t.Fatalf("expected no error when binding disabled, got %v", err)
		}
	})

	t.Run("no fingerprint in token — binding skipped", func(t *testing.T) {
		j := newTestJWTWithBinding(true, true)
		token, _ := j.GenerateToken("user-1", "a@test.com", "user", "org-1")
		_, err := j.ValidateTokenWithFingerprint(token, "10.0.0.1", "Changed-Agent")
		if err != nil {
			t.Fatalf("expected no error when no fingerprint in token, got %v", err)
		}
	})

	t.Run("only IP binding — UA change ignored", func(t *testing.T) {
		j := newTestJWTWithBinding(true, false)
		fp := ComputeFingerprint("192.168.1.1", "")
		token, _ := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		_, err := j.ValidateTokenWithFingerprint(token, "192.168.1.1", "Changed-Agent")
		if err != nil {
			t.Fatalf("expected no error when only IP binding and IP matches, got %v", err)
		}
	})

	t.Run("only IP binding — IP change fails", func(t *testing.T) {
		j := newTestJWTWithBinding(true, false)
		fp := ComputeFingerprint("192.168.1.1", "")
		token, _ := j.GenerateTokenWithFingerprint("user-1", "a@test.com", "user", "org-1", fp)
		_, err := j.ValidateTokenWithFingerprint(token, "10.0.0.1", "Mozilla/5.0")
		if err != ErrFingerprintMismatch {
			t.Fatalf("expected ErrFingerprintMismatch, got %v", err)
		}
	})
}

func TestComputeFingerprint(t *testing.T) {
	fp1 := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
	fp2 := ComputeFingerprint("192.168.1.1", "Mozilla/5.0")
	if fp1 != fp2 {
		t.Errorf("same inputs should produce same fingerprint")
	}

	fp3 := ComputeFingerprint("10.0.0.1", "Mozilla/5.0")
	if fp1 == fp3 {
		t.Errorf("different IPs should produce different fingerprints")
	}

	fp4 := ComputeFingerprint("192.168.1.1", "Chrome/90.0")
	if fp1 == fp4 {
		t.Errorf("different UAs should produce different fingerprints")
	}

	if len(fp1) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars", len(fp1))
	}
}

func TestTokenRevocation(t *testing.T) {
	j := newTestJWT()

	t.Run("token issued after revocation is accepted", func(t *testing.T) {
		j.RevokeAllUserTokens("user-1")
		time.Sleep(time.Second)

		token, err := j.GenerateToken("user-1", "a@test.com", "user", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}
		_, err = j.ValidateToken(token)
		if err != nil {
			t.Fatalf("token issued after revocation should be accepted, got: %v", err)
		}
	})

	t.Run("token issued before revocation is rejected", func(t *testing.T) {
		token, err := j.GenerateToken("user-2", "b@test.com", "user", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		j.RevokeAllUserTokens("user-2")
		time.Sleep(10 * time.Millisecond)

		_, err = j.ValidateToken(token)
		if err != ErrTokenRevoked {
			t.Fatalf("expected ErrTokenRevoked, got %v", err)
		}
	})

	t.Run("revocation for one user does not affect another", func(t *testing.T) {
		token, err := j.GenerateToken("user-3", "c@test.com", "user", "org-1")
		if err != nil {
			t.Fatalf("failed to generate token: %v", err)
		}

		j.RevokeAllUserTokens("user-4")

		_, err = j.ValidateToken(token)
		if err != nil {
			t.Fatalf("token for non-revoked user should be accepted, got: %v", err)
		}
	})

	t.Run("GetRevocationTime returns correct time", func(t *testing.T) {
		before := time.Now()
		j.RevokeAllUserTokens("user-5")
		after := time.Now()

		rt, ok := j.GetRevocationTime("user-5")
		if !ok {
			t.Fatal("expected revocation time to exist")
		}
		if rt.Before(before) || rt.After(after) {
			t.Errorf("revocation time %v not between %v and %v", rt, before, after)
		}
	})

	t.Run("GetRevocationTime returns false for non-revoked user", func(t *testing.T) {
		_, ok := j.GetRevocationTime("user-nonexistent")
		if ok {
			t.Fatal("expected no revocation time for non-revoked user")
		}
	})
}

func TestJWTConfig(t *testing.T) {
	t.Run("default audience used when empty", func(t *testing.T) {
		cfg := &config.AuthConfig{
			JWTSecret:     "test-secret-key-for-unit-tests-32+",
			JWTExpiration: 15 * time.Minute,
			JWTAudience:   "",
		}
		j := NewJWT(cfg)
		if j.audience != "vigilagent-api" {
			t.Errorf("expected default audience 'vigilagent-api', got %q", j.audience)
		}
	})

	t.Run("custom audience from config", func(t *testing.T) {
		cfg := &config.AuthConfig{
			JWTSecret:     "test-secret-key-for-unit-tests-32+",
			JWTExpiration: 15 * time.Minute,
			JWTAudience:   "custom-audience",
		}
		j := NewJWT(cfg)
		if j.audience != "custom-audience" {
			t.Errorf("expected audience 'custom-audience', got %q", j.audience)
		}
	})

	t.Run("binding flags propagated from config", func(t *testing.T) {
		cfg := &config.AuthConfig{
			JWTSecret:          "test-secret-key-for-unit-tests-32+",
			JWTExpiration:      15 * time.Minute,
			JWTBindToIP:        true,
			JWTBindToUserAgent: true,
		}
		j := NewJWT(cfg)
		if !j.BindToIP() {
			t.Error("expected BindToIP=true")
		}
		if !j.BindToUserAgent() {
			t.Error("expected BindToUserAgent=true")
		}
	})

	t.Run("secret too short panics", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for short secret")
			}
		}()
		NewJWT(&config.AuthConfig{
			JWTSecret:     "short",
			JWTExpiration: 15 * time.Minute,
		})
	})
}
