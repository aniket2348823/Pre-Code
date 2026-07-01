package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// APIKey represents an API key with its metadata.
type APIKey struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix"`
	Hash   string `json:"hash"` // bcrypt hash, never stored in plaintext
}

// APIKeyService handles API key generation and verification.
type APIKeyService struct {
	prefix string
}

// NewAPIKeyService creates a new API key service.
func NewAPIKeyService(prefix string) *APIKeyService {
	return &APIKeyService{prefix: prefix}
}

// GenerateKey creates a new API key and returns the plaintext key (to show once) and its hash.
func (s *APIKeyService) GenerateKey() (plaintext string, hashed string, prefix string, err error) {
	// Generate 32 random bytes
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", "", fmt.Errorf("failed to generate random bytes: %w", err)
	}

	// Create the plaintext key: prefix + hex(random bytes)
	plaintext = s.prefix + hex.EncodeToString(bytes)

	// Create a short prefix for identification (first 8 chars after the prefix)
	prefix = plaintext[:min(len(s.prefix)+8, len(plaintext))]

	// Hash with bcrypt for storage
	hashBytes, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to hash api key: %w", err)
	}
	hashed = string(hashBytes)

	return plaintext, hashed, prefix, nil
}

// VerifyKey checks if a plaintext key matches a bcrypt hash.
func (s *APIKeyService) VerifyKey(plaintext, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
	return err == nil
}

// ExtractPrefix returns the prefix portion of an API key string.
func (s *APIKeyService) ExtractPrefix(plaintext string) string {
	if len(plaintext) < len(s.prefix)+8 {
		return plaintext
	}
	return plaintext[:min(len(s.prefix)+8, len(plaintext))]
}

// SHA256Hash returns the SHA-256 hex digest of a string (for indexing).
func SHA256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ValidatePrefix checks if a key string starts with the expected prefix.
func (s *APIKeyService) ValidatePrefix(plaintext string) bool {
	return strings.HasPrefix(plaintext, s.prefix)
}

