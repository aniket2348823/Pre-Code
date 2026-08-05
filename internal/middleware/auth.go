package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// --- API Key Authentication ---

// APIKeyAuth provides DB-backed API key authentication.
type APIKeyAuth struct {
	pool *database.Conn
}

// NewAPIKeyAuth creates a new API key auth middleware.
func NewAPIKeyAuth(pool *database.Conn) *APIKeyAuth {
	return &APIKeyAuth{pool: pool}
}

func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

// Authenticate validates an API key and returns user claims.
// Returns nil, nil if no API key was presented.
// During rotation grace period, also accepts the old key via rotation_token_hash lookup.
func (a *APIKeyAuth) Authenticate(r *http.Request) (*auth.Claims, error) {
	plaintext := extractAPIKey(r)
	if plaintext == "" {
		return nil, nil
	}

	keyHash := hashKey(plaintext)

	var (
		id       string
		userID   string
		isActive bool
		expires  *time.Time
		scopes   []string
	)

	// Primary lookup: match by key_hash (normal path)
	query := `
		SELECT id, user_id, is_active, expires_at, scopes
		FROM api_keys
		WHERE key_hash = $1
	`
	err := a.pool.QueryRow(r.Context(), query, keyHash).Scan(
		&id, &userID, &isActive, &expires, &scopes,
	)
	if err != nil {
		// Key hash not found — could be an old rotated key still valid during grace period.
		// Try rotation_token_hash lookup: the client may be sending the rotation token.
		rotHash := auth.SHA256Hash(plaintext)
		rotQuery := `
			SELECT id, user_id, is_active, expires_at, scopes
			FROM api_keys
			WHERE rotation_token_hash = $1 AND rotated_at IS NOT NULL
		`
		err2 := a.pool.QueryRow(r.Context(), rotQuery, rotHash).Scan(
			&id, &userID, &isActive, &expires, &scopes,
		)
		if err2 != nil {
			return nil, ErrInvalidAPIKey
		}
	}

	if !isActive {
		return nil, ErrInvalidAPIKey
	}

	if expires != nil && expires.Before(time.Now()) {
		return nil, ErrExpiredAPIKey
	}

	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = a.pool.Exec(bgCtx, `UPDATE api_keys SET last_used_at = NOW() WHERE id = $1`, id)
	}()

	var role string
	err = a.pool.QueryRow(r.Context(), `SELECT role FROM users WHERE id = $1`, userID).Scan(&role)
	if err != nil {
		// Fail closed: a DB error while resolving the role must not silently
		// downgrade (or widen) access. Return the error to reject the request.
		slog.Warn("api-key: failed to resolve user role", "error", err, "user_id", userID)
		return nil, err
	}

	return &auth.Claims{
		UserID:   userID,
		Email:    "",
		Role:     role,
		Scopes:   scopes,
		IsAPIKey: true,
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

func extractAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-API-Key"); key != "" {
		return key
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			token := parts[1]
			if strings.HasPrefix(token, "va_") {
				return token
			}
		}
	}

	return ""
}

// WrapIntoChiMiddleware wraps APIKeyAuth into a chi-compatible middleware.
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
		next.ServeHTTP(w, r)
	})
}

// --- Auth Session Middleware ---

// AuthSessionMiddleware sets the PostgreSQL session variable app.current_user_id.
type AuthSessionMiddleware struct {
	conn *database.Conn
}

// NewAuthSessionMiddleware creates a middleware that sets the DB session user.
func NewAuthSessionMiddleware(conn *database.Conn) *AuthSessionMiddleware {
	return &AuthSessionMiddleware{conn: conn}
}

// Middleware wraps an http.Handler and sets the DB session user after auth.
func (m *AuthSessionMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.ClaimsFromContext(r.Context())
		if !ok {
			next.ServeHTTP(w, r)
			return
		}			poolConn, err := m.conn.Pool().Acquire(r.Context())
			if err != nil {
				slog.Warn("auth-session: failed to acquire connection", "error", err)
				next.ServeHTTP(w, r)
				return
			}

			_, err = poolConn.Exec(r.Context(), "SELECT app_auth.set_current_user_id($1)", claims.UserID)
			if err != nil {
				poolConn.Release()
				slog.Warn("auth-session: failed to set user ID", "error", err, "user_id", claims.UserID)
				next.ServeHTTP(w, r)
				return
			}

			// Keep the dedicated connection in the request context for the whole
			// request so RLS sees app.current_user_id on every query (the Conn
			// wrapper routes through ConnFromContext). Release it afterwards — a
			// session variable set on a released pooled connection is a no-op.
			ctx := database.WithConn(r.Context(), poolConn)
			defer poolConn.Release()

			slog.Debug("auth-session: set user ID", "user_id", claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
}

// AuthSessionCheckHandler checks if the session variable is set correctly.
func (m *AuthSessionMiddleware) AuthSessionCheckHandler(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	poolConn, err := m.conn.Pool().Acquire(r.Context())
	if err != nil {
		response.InternalError(w, "failed to acquire connection")
		return
	}
	defer poolConn.Release()

	_, err = poolConn.Exec(r.Context(), "SELECT app_auth.set_current_user_id($1)", claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to set session: "+err.Error())
		return
	}

	var sessionUser string
	err = poolConn.QueryRow(r.Context(), "SELECT app_auth.current_user_id()::text").Scan(&sessionUser)
	if err != nil {
		response.InternalError(w, "failed to read session: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"session_valid": sessionUser == claims.UserID,
	})
}

// --- JWT Rotation ---

// JWTRotationConfig configures JWT rotation behavior.
type JWTRotationConfig struct {
	MaxTokenAge       time.Duration
	RotateOnEndpoints []string
}

// DefaultJWTRotationConfig returns sensible defaults.
func DefaultJWTRotationConfig() *JWTRotationConfig {
	return &JWTRotationConfig{
		MaxTokenAge:       15 * time.Minute,
		RotateOnEndpoints: []string{"/auth/refresh", "/users/me"},
	}
}

// JWTRotationMiddleware issues a new token when the current one is near expiry.
func JWTRotationMiddleware(cfg *JWTRotationConfig, jwtSvc *auth.JWT) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultJWTRotationConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			shouldRotate := false
			path := r.URL.Path
			for _, endpoint := range cfg.RotateOnEndpoints {
				if strings.HasPrefix(path, endpoint) {
					shouldRotate = true
					break
				}
			}

			if shouldRotate {
				newToken, err := jwtSvc.GenerateToken(claims.UserID, claims.Email, claims.Role, claims.OrgID)
				if err == nil {
					w.Header().Set("X-New-Token", newToken)
					w.Header().Set("X-Token-Rotated", "true")
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireJWTRefresh forces token refresh on specific operations.
func RequireJWTRefresh(jwtSvc *auth.JWT) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				response.Unauthorized(w, "missing authentication")
				return
			}

			newToken, err := jwtSvc.GenerateToken(claims.UserID, claims.Email, claims.Role, claims.OrgID)
			if err != nil {
				response.InternalError(w, "failed to rotate token")
				return
			}

			w.Header().Set("X-New-Token", newToken)
			next.ServeHTTP(w, r)
		})
	}
}

// FingerprintBindingConfig configures IP + User-Agent binding for JWT tokens.
type FingerprintBindingConfig struct {
	BindToIP        bool
	BindToUserAgent bool
}

// NewFingerprintBindingConfig creates config from AuthConfig.
func NewFingerprintBindingConfig(cfg *config.AuthConfig) *FingerprintBindingConfig {
	return &FingerprintBindingConfig{
		BindToIP:        cfg.JWTBindToIP,
		BindToUserAgent: cfg.JWTBindToUserAgent,
	}
}

// FingerprintBindingMiddleware verifies that the JWT fingerprint matches the current request's IP + User-Agent.
// Must run AFTER auth middleware has placed claims in context.
func FingerprintBindingMiddleware(cfg *FingerprintBindingConfig) func(http.Handler) http.Handler {
	if cfg == nil || (!cfg.BindToIP && !cfg.BindToUserAgent) {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// Skip fingerprint check for API keys (they don't have fingerprints)
			if claims.IsAPIKey {
				next.ServeHTTP(w, r)
				return
			}

			if claims.Fingerprint == "" {
				next.ServeHTTP(w, r)
				return
			}

			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx > 0 {
				ip = ip[:idx]
			}
			ua := r.UserAgent()

			var expected string
			switch {
			case cfg.BindToIP && cfg.BindToUserAgent:
				expected = auth.ComputeFingerprint(ip, ua)
			case cfg.BindToIP:
				expected = auth.ComputeFingerprint(ip, "")
			case cfg.BindToUserAgent:
				expected = auth.ComputeFingerprint("", ua)
			}

			if claims.Fingerprint != expected {
				slog.Warn("fingerprint-binding: mismatch",
					"user_id", claims.UserID,
					"expected", expected,
					"got", claims.Fingerprint,
				)
				response.Unauthorized(w, "token fingerprint mismatch — IP or User-Agent changed")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
