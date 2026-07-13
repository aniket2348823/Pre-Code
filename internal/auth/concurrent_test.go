package auth

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

func newTestJWT_Deep() *JWT {
	return NewJWT(&config.AuthConfig{JWTSecret: "test-secret-key", JWTExpiration: 15 * time.Minute})
}

// --- JWT Edge Cases ---

func TestJWT_ZeroExpiry(t *testing.T) {
	j := &JWT{secret: []byte("s"), expiration: 0}
	tok, err := j.GenerateToken("u", "e@e.com", "user", "o")
	if err != nil {
		t.Fatal(err)
	}
	// Token with 0 expiry is immediately expired
	_, err = j.ValidateToken(tok)
	if err == nil {
		t.Error("zero-expiry token should be rejected")
	}
}

func TestJWT_NegativeExpiry(t *testing.T) {
	j := &JWT{secret: []byte("s"), expiration: -1 * time.Hour}
	tok, err := j.GenerateToken("u", "e@e.com", "user", "o")
	if err != nil {
		t.Fatal(err)
	}
	_, err = j.ValidateToken(tok)
	if err == nil {
		t.Error("negative expiry should be rejected")
	}
}

func TestJWT_EmptySecret(t *testing.T) {
	j := NewJWT(&config.AuthConfig{JWTSecret: "", JWTExpiration: 15 * time.Minute})
	_, err := j.GenerateToken("u", "e@e.com", "user", "o")
	if err == nil {
		t.Error("empty secret should fail token generation")
	}
}

func TestJWT_UnicodeEmail(t *testing.T) {
	j := newTestJWT_Deep()
	tok, err := j.GenerateToken("u", "user@example.com", "user", "o")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := j.ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("email mismatch: %q", claims.Email)
	}
}

func TestJWT_VeryLongUserID(t *testing.T) {
	j := newTestJWT_Deep()
	longID := strings.Repeat("a", 10000)
	tok, err := j.GenerateToken(longID, "e@e.com", "user", "o")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := j.ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != longID {
		t.Error("long UserID not preserved")
	}
}

func TestJWT_NilClaimsInContext(t *testing.T) {
	_, ok := ClaimsFromContext(nil)
	if ok {
		t.Error("nil context should return false")
	}
	_, ok = ClaimsFromContext(context.Background())
	if ok {
		t.Error("empty context should return false")
	}
}

func TestJWT_DoubleValidation(t *testing.T) {
	j := newTestJWT_Deep()
	tok, _ := j.GenerateToken("u", "e@e.com", "user", "o")
	for i := 0; i < 10; i++ {
		_, err := j.ValidateToken(tok)
		if err != nil {
			t.Fatalf("validation %d failed: %v", i, err)
		}
	}
}

func TestJWT_EmptyUserID(t *testing.T) {
	j := newTestJWT_Deep()
	tok, err := j.GenerateToken("", "e@e.com", "user", "o")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := j.ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != "" {
		t.Errorf("expected empty UserID, got %q", claims.UserID)
	}
}

func TestJWT_ConcurrentGenerateAndValidate(t *testing.T) {
	j := newTestJWT_Deep()
	var wg sync.WaitGroup
	var genErrs, valErrs int64
	tokens := make(chan string, 100)
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tok, err := j.GenerateToken("user-1", "e@e.com", "user", "org-1")
			if err != nil {
				atomic.AddInt64(&genErrs, 1)
				return
			}
			tokens <- tok
		}(i)
	}
	wg.Wait()
	close(tokens)
	var wg2 sync.WaitGroup
	for tok := range tokens {
		wg2.Add(1)
		go func(t string) {
			defer wg2.Done()
			if _, err := j.ValidateToken(t); err != nil {
				atomic.AddInt64(&valErrs, 1)
			}
		}(tok)
	}
	wg2.Wait()
	if genErrs > 0 {
		t.Errorf("generation errors: %d", genErrs)
	}
	if valErrs > 0 {
		t.Errorf("validation errors: %d", valErrs)
	}
}

func TestJWT_ContextClaims_Concurrent(t *testing.T) {
	ctx := context.Background()
	var wg sync.WaitGroup
	var errs int64
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			claims := &Claims{UserID: "u", Email: "e@e.com", Role: "user"}
			newCtx := ContextWithClaims(ctx, claims)
			got, ok := ClaimsFromContext(newCtx)
			if !ok || got == nil {
				atomic.AddInt64(&errs, 1)
			}
		}(i)
	}
	wg.Wait()
	if errs > 0 {
		t.Errorf("context errors: %d", errs)
	}
}

// --- API Key Edge Cases ---

func newTestAPIKeySvc() *APIKeyService { return NewAPIKeyService("va_") }

func TestAPIKey_NullBytesInPlaintext(t *testing.T) {
	svc := newTestAPIKeySvc()
	plaintext, hashed, _, err := svc.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	// Verify with correct key
	if !svc.VerifyKey(plaintext, hashed) {
		t.Error("valid key should verify")
	}
	// Verify with null-byte injected key
	bad := plaintext[:len(plaintext)/2] + "\x00" + plaintext[len(plaintext)/2:]
	if svc.VerifyKey(bad, hashed) {
		t.Error("null-byte key should not verify")
	}
}

func TestAPIKey_TruncatedPlaintext(t *testing.T) {
	svc := newTestAPIKeySvc()
	plaintext, hashed, _, _ := svc.GenerateKey()
	if svc.VerifyKey(plaintext[:len(plaintext)/2], hashed) {
		t.Error("truncated key should not verify")
	}
}

func TestAPIKey_ExtraPadding(t *testing.T) {
	svc := newTestAPIKeySvc()
	plaintext, hashed, _, _ := svc.GenerateKey()
	if svc.VerifyKey(plaintext+"extra", hashed) {
		t.Error("padded key should not verify")
	}
}

func TestAPIKey_SHA256EmptyString(t *testing.T) {
	h := SHA256Hash("")
	if len(h) != 64 {
		t.Errorf("expected 64 chars, got %d", len(h))
	}
}

func TestAPIKey_ValidatePrefix1000Chars(t *testing.T) {
	svc := newTestAPIKeySvc()
	long := strings.Repeat("a", 1000)
	if svc.ValidatePrefix(long) {
		t.Error("1000-char prefix should not match")
	}
}

func TestAPIKey_NoCollision(t *testing.T) {
	svc := newTestAPIKeySvc()
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		p, _, _, _ := svc.GenerateKey()
		if seen[p] {
			t.Fatalf("collision at %d", i)
		}
		seen[p] = true
	}
}

func TestAPIKey_ConcurrentGenerate(t *testing.T) {
	svc := newTestAPIKeySvc()
	var wg sync.WaitGroup
	var errs int64
	keys := make(chan string, 200)
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, h, _, err := svc.GenerateKey()
			if err != nil {
				atomic.AddInt64(&errs, 1)
				return
			}
			if !svc.VerifyKey(p, h) {
				atomic.AddInt64(&errs, 1)
			}
			keys <- p
		}()
	}
	wg.Wait()
	close(keys)
	if errs > 0 {
		t.Errorf("concurrent errors: %d", errs)
	}
}

// --- Password Edge Cases ---

func TestPassword_12Chars(t *testing.T) {
	pw := "Abcdefghijk1"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(pw, hash) {
		t.Error("12-char password should verify")
	}
}

func TestPassword_11Chars(t *testing.T) {
	pw := "Abcdefghijk"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(pw, hash) {
		t.Error("11-char password should verify")
	}
}

func TestPassword_SpacesOnly(t *testing.T) {
	pw := "            "
	hash, _ := HashPassword(pw)
	if !CheckPassword(pw, hash) {
		t.Error("spaces-only password should verify")
	}
}

func TestPassword_Unicode(t *testing.T) {
	passwords := []string{"P@$$w0rd!#%", "Unicode密码123", "🔑🗝️🔐", "line\nnew"}
	for _, pw := range passwords {
		hash, err := HashPassword(pw)
		if err != nil {
			t.Errorf("hash %q: %v", pw, err)
			continue
		}
		if !CheckPassword(pw, hash) {
			t.Errorf("verify %q failed", pw)
		}
	}
}

func TestPassword_SQLInjection(t *testing.T) {
	pw := "' OR '1'='1"
	hash, _ := HashPassword(pw)
	if CheckPassword("anything", hash) {
		t.Error("SQL injection password should not match")
	}
}

func TestPassword_Concurrent(t *testing.T) {
	pw := "ConcurrentTest123!"
	var wg sync.WaitGroup
	var errs int64
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hash, err := HashPassword(pw)
			if err != nil {
				atomic.AddInt64(&errs, 1)
				return
			}
			if !CheckPassword(pw, hash) {
				atomic.AddInt64(&errs, 1)
			}
		}()
	}
	wg.Wait()
	if errs > 0 {
		t.Errorf("concurrent errors: %d", errs)
	}
}

func TestPassword_LargeInput(t *testing.T) {
	pw := strings.Repeat("a", 10000)
	_, err := HashPassword(pw)
	if err == nil {
		t.Error("expected error for password exceeding bcrypt 72-byte limit")
	}
}

func TestSHA256Hash_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	var errs int64
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if len(SHA256Hash("test")) != 64 {
				atomic.AddInt64(&errs, 1)
			}
		}()
	}
	wg.Wait()
	if errs > 0 {
		t.Errorf("errors: %d", errs)
	}
}

// --- Benchmarks ---

func BenchmarkJWTGenerate(b *testing.B) {
	j := newTestJWT_Deep()
	for i := 0; i < b.N; i++ {
		_, _ = j.GenerateToken("u", "e@e.com", "user", "o")
	}
}

func BenchmarkJWTValidate(b *testing.B) {
	j := newTestJWT_Deep()
	tok, _ := j.GenerateToken("u", "e@e.com", "user", "o")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = j.ValidateToken(tok)
	}
}

func BenchmarkAPIKeyGenerate(b *testing.B) {
	svc := newTestAPIKeySvc()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = svc.GenerateKey()
	}
}

func BenchmarkAPIKeyVerify(b *testing.B) {
	svc := newTestAPIKeySvc()
	p, h, _, _ := svc.GenerateKey()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = svc.VerifyKey(p, h)
	}
}

func BenchmarkPasswordHash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword("benchmark")
	}
}

func BenchmarkPasswordCheck(b *testing.B) {
	hash, _ := HashPassword("benchmark")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CheckPassword("benchmark", hash)
	}
}
