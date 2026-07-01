package auth

import (
	"strings"
	"testing"
)

func TestHashPassword(t *testing.T) {
	t.Run("returns non-empty hash", func(t *testing.T) {
		hash, err := HashPassword("mypassword")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash == "" {
			t.Fatal("expected non-empty hash")
		}
		if hash == "mypassword" {
			t.Fatal("hash should not equal plaintext")
		}
	})

	t.Run("different calls produce different hashes", func(t *testing.T) {
		hash1, _ := HashPassword("samepassword")
		hash2, _ := HashPassword("samepassword")
		if hash1 == hash2 {
			t.Fatal("bcrypt should produce different hashes for same input (random salt)")
		}
	})

	t.Run("hash starts with bcrypt prefix", func(t *testing.T) {
		hash, _ := HashPassword("test")
		if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") {
			t.Fatalf("hash should start with bcrypt prefix, got: %s", hash[:4])
		}
	})
}

func TestCheckPassword(t *testing.T) {
	t.Run("correct password returns true", func(t *testing.T) {
		hash, _ := HashPassword("correctpass")
		if !CheckPassword("correctpass", hash) {
			t.Fatal("expected CheckPassword to return true for correct password")
		}
	})

	t.Run("wrong password returns false", func(t *testing.T) {
		hash, _ := HashPassword("correctpass")
		if CheckPassword("wrongpass", hash) {
			t.Fatal("expected CheckPassword to return false for wrong password")
		}
	})

	t.Run("empty password against empty hash returns false", func(t *testing.T) {
		if CheckPassword("", "") {
			t.Fatal("expected CheckPassword to return false for empty hash")
		}
	})

	t.Run("empty password against valid hash returns false", func(t *testing.T) {
		hash, _ := HashPassword("nonempty")
		if CheckPassword("", hash) {
			t.Fatal("expected CheckPassword to return false for empty password")
		}
	})
}

func BenchmarkHashPassword(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, _ = HashPassword("benchmark")
	}
}
