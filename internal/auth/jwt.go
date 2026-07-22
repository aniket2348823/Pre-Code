package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/vigilagent/vigilagent/internal/config"
)

var (
	ErrInvalidToken = errors.New("invalid or expired token")
	ErrMissingToken = errors.New("missing authorization token")
)

// Claims represents the JWT claims structure.
type Claims struct {
	UserID    string   `json:"user_id"`
	Email     string   `json:"email"`
	Role      string   `json:"role"`
	OrgID     string   `json:"org_id"`
	Scopes    []string `json:"scopes,omitempty"`
	IsAPIKey  bool     `json:"is_api_key,omitempty"`
	jwt.RegisteredClaims
}

// JWT handles token generation and validation.
type JWT struct {
	secret     []byte
	expiration time.Duration
}

// NewJWT creates a new JWT service from config.
func NewJWT(cfg *config.AuthConfig) *JWT {
	return &JWT{
		secret:     []byte(cfg.JWTSecret),
		expiration: cfg.JWTExpiration,
	}
}

// GenerateToken creates a new signed JWT token for the given user.
func (j *JWT) GenerateToken(userID, email, role, orgID string) (string, error) {
	if len(j.secret) == 0 {
		return "", fmt.Errorf("jwt secret must not be empty")
	}
	now := time.Now()
	claims := &Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		OrgID:  orgID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "vigilagent",
			Subject:   userID,
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
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return j.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, ErrInvalidToken
	}

	return claims, nil
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
