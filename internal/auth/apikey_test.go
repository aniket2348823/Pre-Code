package auth

import (
	"strings"
	"testing"
)

func newTestAPIKeyService() *APIKeyService {
	return NewAPIKeyService("va_")
}

func TestGenerateKey(t *testing.T) {
	svc := newTestAPIKeyService()

	t.Run("returns non-empty values", func(t *testing.T) {
		plaintext, hashed, prefix, err := svc.GenerateKey()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if plaintext == "" {
			t.Fatal("expected non-empty plaintext")
		}
		if hashed == "" {
			t.Fatal("expected non-empty hash")
		}
		if prefix == "" {
			t.Fatal("expected non-empty prefix")
		}
	})

	t.Run("plaintext starts with prefix", func(t *testing.T) {
		plaintext, _, _, _ := svc.GenerateKey()
		if !strings.HasPrefix(plaintext, "va_") {
			t.Fatalf("expected plaintext to start with 'va_', got: %s", plaintext[:6])
		}
	})

	t.Run("prefix is a subset of plaintext", func(t *testing.T) {
		plaintext, _, prefix, _ := svc.GenerateKey()
		if !strings.HasPrefix(plaintext, prefix) {
			t.Fatalf("plaintext %s should start with prefix %s", plaintext, prefix)
		}
	})

	t.Run("different calls produce different keys", func(t *testing.T) {
		p1, _, _, _ := svc.GenerateKey()
		p2, _, _, _ := svc.GenerateKey()
		if p1 == p2 {
			t.Fatal("expected unique keys on each generation")
		}
	})
}

func TestVerifyKey(t *testing.T) {
	svc := newTestAPIKeyService()

	t.Run("correct key returns true", func(t *testing.T) {
		plaintext, hashed, _, _ := svc.GenerateKey()
		if !svc.VerifyKey(plaintext, hashed) {
			t.Fatal("expected VerifyKey to return true for correct key")
		}
	})

	t.Run("wrong key returns false", func(t *testing.T) {
		_, hashed, _, _ := svc.GenerateKey()
		if svc.VerifyKey("va_wrongkey12345678901234567890", hashed) {
			t.Fatal("expected VerifyKey to return false for wrong key")
		}
	})
}

func TestExtractPrefix(t *testing.T) {
	svc := newTestAPIKeyService()

	t.Run("returns expected prefix length", func(t *testing.T) {
		plaintext, _, _, _ := svc.GenerateKey()
		prefix := svc.ExtractPrefix(plaintext)
		expectedLen := len("va_") + 8 // prefix + 8 chars
		if len(prefix) != expectedLen {
			t.Fatalf("expected prefix length %d, got %d", expectedLen, len(prefix))
		}
	})

	t.Run("short input returns full string", func(t *testing.T) {
		short := "va_abc"
		prefix := svc.ExtractPrefix(short)
		if prefix != short {
			t.Fatalf("expected full short string returned, got %s", prefix)
		}
	})
}

func TestSHA256Hash(t *testing.T) {
	t.Run("returns 64 char hex string", func(t *testing.T) {
		hash := SHA256Hash("hello world")
		if len(hash) != 64 {
			t.Fatalf("expected 64 char hex string, got %d chars", len(hash))
		}
	})

	t.Run("deterministic output", func(t *testing.T) {
		h1 := SHA256Hash("test input")
		h2 := SHA256Hash("test input")
		if h1 != h2 {
			t.Fatal("SHA256Hash should be deterministic")
		}
	})

	t.Run("different inputs produce different hashes", func(t *testing.T) {
		h1 := SHA256Hash("input1")
		h2 := SHA256Hash("input2")
		if h1 == h2 {
			t.Fatal("different inputs should produce different hashes")
		}
	})
}

func TestValidatePrefix(t *testing.T) {
	svc := newTestAPIKeyService()

	t.Run("valid prefix returns true", func(t *testing.T) {
		if !svc.ValidatePrefix("va_abc123def456") {
			t.Fatal("expected true for valid prefix")
		}
	})

	t.Run("wrong prefix returns false", func(t *testing.T) {
		if svc.ValidatePrefix("sk_abc123def456") {
			t.Fatal("expected false for wrong prefix")
		}
	})

	t.Run("empty string returns false", func(t *testing.T) {
		if svc.ValidatePrefix("") {
			t.Fatal("expected false for empty string")
		}
	})
}
