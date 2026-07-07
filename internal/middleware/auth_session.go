package middleware

import (
	"log/slog"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/database"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// AuthSessionMiddleware sets the PostgreSQL session variable app.current_user_id
// after successful JWT/API key authentication. This enables RLS policies to
// identify the current user via app_auth.current_user_id().
type AuthSessionMiddleware struct {
	conn *database.Conn
}

// NewAuthSessionMiddleware creates a middleware that sets the DB session user.
func NewAuthSessionMiddleware(conn *database.Conn) *AuthSessionMiddleware {
	return &AuthSessionMiddleware{conn: conn}
}

// Middleware wraps an http.Handler and sets the DB session user after auth.
// Must be placed AFTER the auth middleware so claims are in context.
func (m *AuthSessionMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := auth.ClaimsFromContext(r.Context())
		if !ok {
			// No auth claims — skip session setup (public route)
			next.ServeHTTP(w, r)
			return
		}

		// Set the session variable on a dedicated connection from the pool.
		poolConn, err := m.conn.Pool().Acquire(r.Context())
		if err != nil {
			slog.Warn("auth-session: failed to acquire connection", "error", err)
			next.ServeHTTP(w, r)
			return
		}
		defer poolConn.Release()

		_, err = poolConn.Exec(r.Context(), "SELECT app_auth.set_current_user_id($1)", claims.UserID)
		if err != nil {
			slog.Warn("auth-session: failed to set user ID", "error", err, "user_id", claims.UserID)
			// Don't fail the request — RLS may be disabled or function may not exist yet
			next.ServeHTTP(w, r)
			return
		}

		slog.Debug("auth-session: set user ID", "user_id", claims.UserID)
		next.ServeHTTP(w, r)
	})
}

// AuthSessionCheckHandler checks if the session variable is set correctly.
// Useful for debugging RLS issues.
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
		"authenticated_user": claims.UserID,
		"session_user":       sessionUser,
		"match":              sessionUser == claims.UserID,
	})
}
