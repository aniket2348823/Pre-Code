package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// JWTBlacklist provides Redis-backed JWT token revocation.
type JWTBlacklist struct {
	rdb    *redis.Client
	prefix string
}

// NewJWTBlacklist creates a new JWT blacklist backed by Redis.
func NewJWTBlacklist(rdb *redis.Client) *JWTBlacklist {
	return &JWTBlacklist{
		rdb:    rdb,
		prefix: "jwt:blacklist:",
	}
}

// Revoke adds a token to the blacklist with the given TTL.
func (b *JWTBlacklist) Revoke(ctx context.Context, tokenStr string, ttl time.Duration) error {
	key := b.prefix + tokenStr
	return b.rdb.Set(ctx, key, "1", ttl).Err()
}

// IsRevoked checks if a token is in the blacklist.
func (b *JWTBlacklist) IsRevoked(ctx context.Context, tokenStr string) bool {
	key := b.prefix + tokenStr
	val, err := b.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return false
	}
	if err != nil {
		slog.Warn("blacklist: failed to check revocation", "error", err)
		return false // fail open — don't block if Redis is down
	}
	return val == "1"
}

// ExtractBearerToken extracts the JWT token from the Authorization header.
// Skips API keys (contain underscore pattern like va_xxx).
// Exported for use by the router package.
func ExtractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	token := parts[1]
	// Skip API keys (contain underscore, no dots)
	if !strings.Contains(token, ".") && strings.Contains(token, "_") {
		return ""
	}
	return token
}

// Middleware returns a chi middleware that checks the JWT blacklist.
func (b *JWTBlacklist) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := ExtractBearerToken(r)
		if tokenStr == "" {
			next.ServeHTTP(w, r)
			return
		}

		if b.IsRevoked(r.Context(), tokenStr) {
			response.Unauthorized(w, "token has been revoked")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RevokeAllForUser revokes all tokens for a user by storing a user-level revocation.
// This is used on password change / account lockout.
func (b *JWTBlacklist) RevokeAllForUser(ctx context.Context, userID string) error {
	key := fmt.Sprintf("jwt:blacklist:user:%s", userID)
	return b.rdb.Set(ctx, key, time.Now().Unix(), 24*time.Hour).Err()
}

// IsUserRevoked checks if all tokens for a user have been revoked.
func (b *JWTBlacklist) IsUserRevoked(ctx context.Context, userID string) bool {
	key := fmt.Sprintf("jwt:blacklist:user:%s", userID)
	_, err := b.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return false
	}
	if err != nil {
		return false // fail open
	}
	return true
}

// MiddlewareWithUserRevocation checks both token-level and user-level revocation.
func (b *JWTBlacklist) MiddlewareWithUserRevocation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.ClaimsFromContext(r.Context())
		if ok && b.IsUserRevoked(r.Context(), claims.UserID) {
			response.Unauthorized(w, "all tokens for this user have been revoked")
			return
		}
		next.ServeHTTP(w, r)
	})
}
