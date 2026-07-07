package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
	"github.com/vigilagent/vigilagent/internal/database"
)

// APIKeyAuth provides real DB-backed API key authentication.
// It hashes the presented key, looks it up in the api_keys table,
// and populates the request context with real user claims.
type APIKeyAuth struct {
	pool *database.Conn
}

// NewAPIKeyAuth creates a new API key auth middleware.
func NewAPIKeyAuth(pool *database.Conn) *APIKeyAuth {
	return &APIKeyAuth{pool: pool}
}

// hashKey returns the SHA-256 hex digest of the plaintext key.
func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// Authenticate validates an API key from the X-API-Key header or
// Bearer vga_... authorization, and returns user claims.
// Returns nil, nil if no API key was presented (caller should try JWT).
func (a *APIKeyAuth) Authenticate(r *http.Request) (*auth.Claims, error) {
	plaintext := extractAPIKey(r)
	if plaintext == "" {
		return nil, nil
	}

	keyHash := hashKey(plaintext)

	// Lookup by hash in the database
	var (
		id       string
		userID   string
		isActive bool
		expires  *time.Time
	)

	query := `
		SELECT id, user_id, is_active, expires_at
		FROM api_keys
		WHERE key_hash = $1
	`
	err := a.pool.QueryRow(r.Context(), query, keyHash).Scan(
		&id, &userID, &isActive, &expires,
	)
	if err != nil {
		return nil, ErrInvalidAPIKey
	}

	if !isActive {
		return nil, ErrInvalidAPIKey
	}

	if expires != nil && expires.Before(time.Now()) {
		return nil, ErrExpiredAPIKey
	}

	// Best-effort: update last_used_at in background
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(bgCtx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	}()

	// Build real claims from the API key's associated user
	// The role comes from the users table
	var role string
	err = a.pool.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	if err != nil {
		role = "user" // fallback
	}

	return &auth.Claims{
		UserID: userID,
		Email:  "",
		Role:   role,
	}, nil
}

var (
	ErrInvalidAPIKey = &APIKeyAuthError{"invalid or unknown API key"}
	ErrExpiredAPIKey = &APIKeyAuthError{"API key has expired"}
)

type APIKeyAuthError struct {
	msg string
}

func (e *APIKeyAuthError) Error() string { return e.msg }

// extractAPIKey pulls the API key from the request.
// Checks X-API-Key header first, then falls back to Bearer vga_... tokens.
func extractAPIKey(r *http.Request) string {
	// Check X-API-Key header
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	// Check Authorization header for Bearer token starting with a key prefix
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := parts[1]
			// Heuristic: if it looks like an API key (contains underscores, no dots),
			// treat it as an API key. JWT tokens have dots.
			if !strings.Contains(token, ".") && strings.Contains(token, "_") {
				return token
			}
		}
	}

	return ""
}

// WrapIntoChiMiddleware wraps APIKeyAuth into a chi-compatible middleware
// that can be composed with the existing router.
func (a *APIKeyAuth) WrapIntoChiMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := a.Authenticate(r)
		if err != nil {
			response.Unauthorized(w, err.Error())
			return
		}
		if claims != nil {
			ctx := auth.ContextWithClaims(r.Context(), claims)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// No API key presented, pass through (JWT middleware will handle)
		next.ServeHTTP(w, r)
	})
}
