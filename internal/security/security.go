// Package security provides security hardening utilities for VigilAgent:
// input sanitization, output encoding, secrets management, and security headers.
package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// SanitizeInput removes potentially dangerous content from user input.
func SanitizeInput(input string) string {
	// Trim whitespace
	input = strings.TrimSpace(input)
	// Remove null bytes
	input = strings.ReplaceAll(input, "\x00", "")
	// Remove control characters except newline and tab
	var sb strings.Builder
	for _, r := range input {
		if r >= 32 || r == '\n' || r == '\t' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// SanitizeFilename ensures a filename is safe for filesystem operations.
func SanitizeFilename(name string) string {
	// Remove path separators and dangerous chars
	reg := regexp.MustCompile(`[^\w\-.]`)
	name = reg.ReplaceAllString(name, "_")
	// Prevent directory traversal
	name = strings.ReplaceAll(name, "..", "_")
	name = strings.TrimLeft(name, "_.")
	if name == "" {
		name = "unnamed"
	}
	return name
}

// EscapeHTML encodes HTML special characters to prevent XSS.
func EscapeHTML(input string) string {
	input = strings.ReplaceAll(input, "&", "&amp;")
	input = strings.ReplaceAll(input, "<", "&lt;")
	input = strings.ReplaceAll(input, ">", "&gt;")
	input = strings.ReplaceAll(input, "\"", "&quot;")
	input = strings.ReplaceAll(input, "'", "&#x27;")
	return input
}

// StripSQLInjection is a basic pattern stripper that removes common SQL injection
// keywords from input. This is NOT a security defense — it is a best-effort
// heuristic for sanitizing user-provided text before display. Use parameterized
// queries for actual SQL injection prevention.
func StripSQLInjection(input string) string {
	dangerous := []string{
		"--", ";--", "/*", "*/", "@@", "@",
		"char", "nchar", "varchar", "nvarchar",
		"alter", "begin", "cast", "create", "cursor",
		"declare", "delete", "drop", "end", "exec",
		"execute", "fetch", "insert", "kill", "select",
		"sys", "sysobjects", "syscolumns", "table", "update",
	}
	lower := strings.ToLower(input)
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			// Remove the dangerous pattern
			reg := regexp.MustCompile("(?i)" + regexp.QuoteMeta(d))
			input = reg.ReplaceAllString(input, "")
		}
	}
	return input
}

// MaskSecret replaces sensitive values with masked versions.
func MaskSecret(secret string, visibleChars int) string {
	if len(secret) <= visibleChars {
		return strings.Repeat("*", len(secret))
	}
	masked := len(secret) - visibleChars
	return strings.Repeat("*", masked) + secret[masked:]
}

// EncryptAES encrypts data with AES-GCM using a passphrase.
func EncryptAES(passphrase string, plaintext []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// DecryptAES decrypts AES-GCM encrypted data.
func DecryptAES(passphrase string, ciphertext []byte) ([]byte, error) {
	key := sha256.Sum256([]byte(passphrase))

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ciphertext, nil)
}

// EncodeBase64 encodes data to base64.
func EncodeBase64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeBase64 decodes base64 data.
func DecodeBase64(encoded string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(encoded)
}

// SecurityHeaders returns recommended HTTP security headers.
func SecurityHeaders() map[string]string {
	return map[string]string{
		"X-Content-Type-Options":    "nosniff",
		"X-Frame-Options":           "DENY",
		"X-XSS-Protection":          "1; mode=block",
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"Content-Security-Policy":   "default-src 'self'",
		"Referrer-Policy":           "strict-origin-when-cross-origin",
		"Permissions-Policy":        "camera=(), microphone=(), geolocation=()",
	}
}

// ValidateAPIKey checks if an API key matches expected format.
func ValidateAPIKey(key, prefix string) bool {
	if !strings.HasPrefix(key, prefix+"_") {
		return false
	}
	body := strings.TrimPrefix(key, prefix+"_")
	return len(body) >= 32 && len(body) <= 128
}
