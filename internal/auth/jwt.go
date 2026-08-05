package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/vigilagent/vigilagent/internal/config"
)

var (
	ErrInvalidToken        = errors.New("invalid or expired token")
	ErrMissingToken        = errors.New("missing authorization token")
	ErrAudienceMismatch    = errors.New("token audience mismatch")
	ErrFingerprintMismatch = errors.New("token fingerprint mismatch")
	ErrTokenRevoked        = errors.New("token revoked by password change")
)

// Claims represents the JWT claims structure.
type Claims struct {
	UserID      string   `json:"user_id"`
	Email       string   `json:"email"`
	Role        string   `json:"role"`
	OrgID       string   `json:"org_id"`
	Scopes      []string `json:"scopes,omitempty"`
	IsAPIKey    bool     `json:"is_api_key,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	jwt.RegisteredClaims
}

// JWT handles token generation and validation.
type JWT struct {
	secret               []byte
	expiration           time.Duration
	audience             string
	bindToIP             bool
	bindToUserAgent      bool
	revocationTimestamps map[string]time.Time
	mu                   sync.RWMutex
}

// NewJWT creates a new JWT service from config.
func NewJWT(cfg *config.AuthConfig) *JWT {
	if len(cfg.JWTSecret) < 32 {
		panic("jwt secret must be at least 32 bytes")
	}
	audience := cfg.JWTAudience
	if audience == "" {
		audience = "vigilagent-api"
	}
	return &JWT{
		secret:               []byte(cfg.JWTSecret),
		expiration:           cfg.JWTExpiration,
		audience:             audience,
		bindToIP:             cfg.JWTBindToIP,
		bindToUserAgent:      cfg.JWTBindToUserAgent,
		revocationTimestamps: make(map[string]time.Time),
	}
}

// ComputeFingerprint computes SHA-256(ip + "|" + userAgent).
func ComputeFingerprint(ip, userAgent string) string {
	h := sha256.Sum256([]byte(ip + "|" + userAgent))
	return hex.EncodeToString(h[:])
}

// GenerateToken creates a new signed JWT token for the given user.
func (j *JWT) GenerateToken(userID, email, role, orgID string) (string, error) {
	return j.GenerateTokenWithFingerprint(userID, email, role, orgID, "")
}

// GenerateTokenWithFingerprint creates a signed JWT with an optional fingerprint claim.
func (j *JWT) GenerateTokenWithFingerprint(userID, email, role, orgID, fingerprint string) (string, error) {
	if len(j.secret) < 32 {
		return "", fmt.Errorf("jwt secret must be at least 32 bytes")
	}
	now := time.Now()
	claims := &Claims{
		UserID:      userID,
		Email:       email,
		Role:        role,
		OrgID:       orgID,
		Fingerprint: fingerprint,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "vigilagent",
			Subject:   userID,
			Audience:  jwt.ClaimStrings{j.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.expiration)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(j.secret)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	slog.Debug("generated jwt token", "user_id", userID, "expires_at", claims.ExpiresAt)
	return signed, nil
}

// ValidateToken parses and validates a JWT token string.
func (j *JWT) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return j.secret, nil
	},
		jwt.WithIssuer("vigilagent"),
		jwt.WithAudience(j.audience),
	)
	if err != nil {
		return nil, ErrInvalidToken
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	// Check user-level revocation (password change)
	j.mu.RLock()
	revokedAt, exists := j.revocationTimestamps[claims.UserID]
	j.mu.RUnlock()
	if exists {
		if claims.IssuedAt != nil && claims.IssuedAt.Time.Before(revokedAt) {
			slog.Warn("jwt: token rejected — issued before revocation",
				"user_id", claims.UserID,
				"issued_at", claims.IssuedAt.Time,
				"revoked_at", revokedAt,
			)
			return nil, ErrTokenRevoked
		}
	}

	return claims, nil
}

// ValidateTokenWithFingerprint validates a token and verifies the fingerprint matches.
func (j *JWT) ValidateTokenWithFingerprint(tokenStr, ip, userAgent string) (*Claims, error) {
	claims, err := j.ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}

	if !j.bindToIP && !j.bindToUserAgent {
		return claims, nil
	}

	if claims.Fingerprint == "" {
		return claims, nil
	}

	var expectedFingerprint string
	switch {
	case j.bindToIP && j.bindToUserAgent:
		expectedFingerprint = ComputeFingerprint(ip, userAgent)
	case j.bindToIP:
		expectedFingerprint = ComputeFingerprint(ip, "")
	case j.bindToUserAgent:
		expectedFingerprint = ComputeFingerprint("", userAgent)
	}

	if claims.Fingerprint != expectedFingerprint {
		slog.Warn("jwt: fingerprint mismatch",
			"user_id", claims.UserID,
			"expected", expectedFingerprint,
			"got", claims.Fingerprint,
		)
		return nil, ErrFingerprintMismatch
	}

	return claims, nil
}

// RevokeAllUserTokens revokes all tokens for a user by recording the revocation timestamp.
// Tokens issued before this timestamp will be rejected.
func (j *JWT) RevokeAllUserTokens(userID string) {
	j.mu.Lock()
	j.revocationTimestamps[userID] = time.Now()
	j.mu.Unlock()
	slog.Info("jwt: revoked all tokens for user", "user_id", userID)
}

// GetRevocationTime returns the revocation timestamp for a user, if any.
func (j *JWT) GetRevocationTime(userID string) (time.Time, bool) {
	j.mu.RLock()
	t, ok := j.revocationTimestamps[userID]
	j.mu.RUnlock()
	return t, ok
}

// BindToIP returns whether IP binding is enabled.
func (j *JWT) BindToIP() bool {
	return j.bindToIP
}

// BindToUserAgent returns whether User-Agent binding is enabled.
func (j *JWT) BindToUserAgent() bool {
	return j.bindToUserAgent
}

// RevocationMiddleware returns an HTTP middleware that rejects tokens revoked via RevokeAllUserTokens.
// This works alongside the Redis-backed JWTBlacklist middleware for password-change revocation.
func (j *JWT) RevocationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenStr := extractBearerToken(r)
		if tokenStr == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Parse without full validation to get user_id and iat quickly
		parser := jwt.NewParser(jwt.WithoutClaimsValidation())
		token, _, err := parser.ParseUnverified(tokenStr, &Claims{})
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		claims, ok := token.Claims.(*Claims)
		if !ok || claims.UserID == "" {
			next.ServeHTTP(w, r)
			return
		}

		j.mu.RLock()
		revokedAt, exists := j.revocationTimestamps[claims.UserID]
		j.mu.RUnlock()

		if exists && claims.IssuedAt != nil && claims.IssuedAt.Time.Before(revokedAt) {
			slog.Warn("jwt-revocation: token rejected — issued before revocation",
				"user_id", claims.UserID,
				"issued_at", claims.IssuedAt.Time,
				"revoked_at", revokedAt,
			)
			http.Error(w, `{"error":"token revoked by password change"}`, http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// extractBearerToken extracts the JWT from the Authorization header.
func extractBearerToken(r *http.Request) string {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return ""
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

// Context key for storing claims.
type contextKey string

const claimsKey contextKey = "claims"

// ContextWithClaims stores claims in context.
func ContextWithClaims(ctx context.Context, claims *Claims) context.Context {
	return context.WithValue(ctx, claimsKey, claims)
}

// ClaimsFromContext retrieves claims from context.
func ClaimsFromContext(ctx context.Context) (*Claims, bool) {
	if ctx == nil {
		return nil, false
	}
	claims, ok := ctx.Value(claimsKey).(*Claims)
	return claims, ok
}
