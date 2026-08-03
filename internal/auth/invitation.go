package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// GenerateInvitationToken creates a cryptographically secure random token.
func GenerateInvitationToken(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate invitation token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
