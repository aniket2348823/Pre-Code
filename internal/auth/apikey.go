package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var bcryptCost = bcrypt.DefaultCost

var apiLookupPepper = func() []byte {
	if p := os.Getenv("VIGILAGENT_AUTH_API_KEY_PEPPER"); p != "" {
		return []byte(p)
	}
	return []byte("vigilagent-api-key-lookup")
}()

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

	// Hash with SHA-256 first to avoid bcrypt 72-byte truncation
	keyHash := sha256.Sum256([]byte(plaintext))
	hashBytes, err := bcrypt.GenerateFromPassword(keyHash[:], bcryptCost)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to hash api key: %w", err)
	}
	hashed = string(hashBytes)

	return plaintext, hashed, prefix, nil
}

// VerifyKey checks if a plaintext key matches a bcrypt hash.
func (s *APIKeyService) VerifyKey(plaintext, hash string) bool {
	keyHash := sha256.Sum256([]byte(plaintext))
	err := bcrypt.CompareHashAndPassword([]byte(hash), keyHash[:])
	return err == nil
}

// ExtractPrefix returns the prefix portion of an API key string.
func (s *APIKeyService) ExtractPrefix(plaintext string) string {
	if len(plaintext) < len(s.prefix)+8 {
		return plaintext
	}
	return plaintext[:min(len(s.prefix)+8, len(plaintext))]
}

// SHA256Hash returns the HMAC-SHA256 hex digest of a string (for indexing).
// Uses a server-side pepper to prevent rainbow table attacks on API key lookups.
// API keys are high-entropy secrets, so HMAC with a pepper is appropriate here
// rather than a slow password hashing function which would be too slow for
// per-request lookups.
func SHA256Hash(s string) string {
	mac := hmac.New(sha256.New, apiLookupPepper)
	mac.Write([]byte(s))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidatePrefix checks if a key string starts with the expected prefix.
func (s *APIKeyService) ValidatePrefix(plaintext string) bool {
	return strings.HasPrefix(plaintext, s.prefix)
}

// ErrAPIKeyExpired is returned when an API key has passed its expiration time.
var ErrAPIKeyExpired = fmt.Errorf("API key has expired")

// RotationResult holds the output of a key rotation operation.
type RotationResult struct {
	NewPlaintext      string
	NewHash           string
	NewPrefix         string
	OldKeyID          string
	RotationTokenHash string
}

// GenerateRotationToken creates a new rotation token and returns its hash.
// The plaintext rotation token is returned once to the caller for secure storage.
func (s *APIKeyService) GenerateRotationToken() (plaintext string, hash string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("failed to generate rotation token: %w", err)
	}
	plaintext = hex.EncodeToString(bytes)
	hash = SHA256Hash(plaintext)
	return plaintext, hash, nil
}

// RotateKey performs a full key rotation: generates new key + rotation token, returns all artifacts.
// Caller is responsible for persisting via repository.RotateAPIKey.
func (s *APIKeyService) RotateKey() (*RotationResult, error) {
	plaintext, hash, prefix, err := s.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("failed to generate new key during rotation: %w", err)
	}

	_, rotHash, err := s.GenerateRotationToken()
	if err != nil {
		return nil, err
	}

	return &RotationResult{
		NewPlaintext:      plaintext,
		NewHash:           hash,
		NewPrefix:         prefix,
		RotationTokenHash: rotHash,
	}, nil
}
