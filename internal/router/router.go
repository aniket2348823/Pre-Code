package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/compression"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/database"
	apperrors "github.com/vigilagent/vigilagent/internal/errors"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/requestid"
	"github.com/vigilagent/vigilagent/internal/slogger"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/internal/webhook"

	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
	"github.com/vigilagent/vigilagent/pkg/validation"
)

func (r *Router) setupMiddleware() {
	r.Use(requestid.Middleware)
	r.Use(mw.TracingMiddleware)
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx > 0 {
				ip = ip[:idx]
			}
			r.RemoteAddr = ip
			next.ServeHTTP(w, r)
		})
	})
	r.Use(middleware.Logger)
	r.Use(slogger.Middleware)
	r.Use(middleware.Recoverer)
	r.Use(compression.Middleware)
	// Enforce the 2 MiB request-body cap globally (scan_handlers documents it).
	r.Use(limitBodySize)
	r.Use(r.securityHeadersMiddleware)
	r.Use(mw.CacheControl(mw.DefaultAPICache()))

	// ETag + in-memory response cache for read-only endpoints
	r.responseCache = mw.NewResponseCache(mw.DefaultResponseCacheConfig())
	r.Use(mw.ResponseCacheMiddleware(r.responseCache))
	r.Use(mw.CacheInvalidationMiddleware(r.responseCache))

	r.useCORSFromConfig()
	r.Use(middleware.Heartbeat("/health"))

	// JWT blacklist — reject revoked tokens on all requests
	if r.blacklist != nil {
		r.Use(r.blacklist.Middleware)
		r.Use(r.blacklist.MiddlewareWithUserRevocation)
	}

	// Audit logging — log all state-changing requests
	if r.auditLogger != nil {
		r.Use(mw.AuditMiddleware(r.auditLogger))
	}

	timeout := 30 * time.Second
	r.Use(func(next http.Handler) http.Handler {
		th := http.TimeoutHandler(next, timeout, `{"error":"request timeout"}`)
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// http.TimeoutHandler's internal writer does not implement
			// http.Hijacker, which breaks WebSocket upgrades. Exempt upgrade
			// requests so /ws still works through the middleware stack.
			if strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
				next.ServeHTTP(w, req)
				return
			}
			th.ServeHTTP(w, req)
		})
	})

	// Rate limit headers on ALL responses (in-memory, informational)
	rlRateLimit := 10000
	if r.cfg != nil && r.cfg.Server.RateLimitPerMin > 0 {
		rlRateLimit = r.cfg.Server.RateLimitPerMin
	}
	r.rlHeaders = mw.NewRateLimitHeadersMiddleware(rlRateLimit, time.Minute)
	r.Use(r.rlHeaders.Middleware(func(req *http.Request) string {
		if claims, ok := auth.ClaimsFromContext(req.Context()); ok {
			return "user:" + claims.UserID
		}
		return mw.RateLimitByIPKey(req)
	}))
}

func (r *Router) getBaseURLFromConfig() string {
	if r.cfg != nil && r.cfg.Server.BaseURL != "" {
		return r.cfg.Server.BaseURL
	}
	return "http://localhost:8080"
}

func (r *Router) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; "+
				"frame-ancestors 'none'; "+
				"form-action 'none'; "+
				"base-uri 'self'; "+
				"object-src 'none'")
		if r.cfg != nil && r.cfg.Server.Env == "production" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) useCORSFromConfig() {
	var cfg cors.Config
	if r.cfg != nil && r.cfg.Server.Env == "production" {
		cfg = cors.ProductionConfig(r.cfg.CORS.AllowedOrigins)
		// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
		slog.Info("CORS configured for production", "origins", r.cfg.CORS.AllowedOrigins)
	} else if r.cfg != nil && corsAllExplicit(r.cfg.CORS.AllowedOrigins) {
		cfg = cors.Config{
			AllowOrigins:     r.cfg.CORS.AllowedOrigins,
			AllowMethods:     r.cfg.CORS.AllowedMethods,
			AllowHeaders:     r.cfg.CORS.AllowedHeaders,
			AllowCredentials: r.cfg.CORS.AllowCredentials,
		}
	} else {
		cfg = cors.DefaultConfig()
		slog.Warn("using permissive CORS (AllowOrigins=[*]) — restrict in production")
	}
	if len(cfg.AllowMethods) == 0 {
		cfg.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(cfg.AllowHeaders) == 0 {
		cfg.AllowHeaders = []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Request-ID"}
	}
	r.Use(cfg.Middleware)
}

func corsAllExplicit(origins []string) bool {
	if len(origins) == 0 {
		return false
	}
	for _, o := range origins {
		if o == "*" {
			return false
		}
	}
	return true
}

// --- Email Handlers ---
func (r *Router) forgotPasswordHandler(w http.ResponseWriter, req *http.Request) {
	// Rate limit this endpoint to prevent email bombing
	lockoutKey := "forgot-password:" + req.RemoteAddr
	if r.lockout != nil && r.lockout.IsLocked(req.Context(), lockoutKey) {
		response.ErrorR(w, req, http.StatusTooManyRequests, "INFRA_001", "too many requests, please try again later")
		return
	}

	var input struct {
		Email string `json:"email"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	if input.Email == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "email is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	user, err := r.users.FindByEmail(req.Context(), input.Email)
	if err != nil {
		// Always return success to prevent email enumeration
		response.JSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
		return
	}

	if r.email != nil {
		baseURL := r.getBaseURLFromConfig()
		if err := r.email.SendPasswordResetEmail(req.Context(), user.ID, user.Email, baseURL); err != nil {
			// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
			slog.Error("failed to send password reset email", "error", err, "user_id", user.ID)
		}
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
}

func (r *Router) resetPasswordHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	v.Required("token", input.Token)
	v.Required("new_password", input.NewPassword)
	v.MinLength("new_password", input.NewPassword, 12)

	if v.WriteResponse(w, req) {
		return
	}

	if r.email == nil {
		apiErr := apperrors.New(apperrors.ErrServiceDown, "email service not configured")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	vt, ok := r.email.ValidateToken(req.Context(), input.Token)
	if !ok || vt.Purpose != "reset" {
		apiErr := apperrors.New(apperrors.ErrTokenInvalid, "invalid or expired reset token")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrHashFailed, "failed to hash password")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	user, err := r.users.FindByID(req.Context(), vt.UserID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrNotFound, "user not found")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if err := r.users.UpdatePassword(req.Context(), user.ID, hash); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update password")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	r.email.InvalidateToken(req.Context(), input.Token)

	response.JSON(w, http.StatusOK, map[string]string{"message": "password has been reset"})
}

func (r *Router) verifyEmailHandler(w http.ResponseWriter, req *http.Request) {
	token := req.URL.Query().Get("token")
	if token == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "token query parameter is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if r.email == nil {
		apiErr := apperrors.New(apperrors.ErrServiceDown, "email service not configured")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	vt, ok := r.email.ValidateToken(req.Context(), token)
	if !ok || vt.Purpose != "verify" {
		apiErr := apperrors.New(apperrors.ErrTokenInvalid, "invalid or expired verification token")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	// Mark user's email as verified in the database
	if err := r.users.UpdateEmailVerified(req.Context(), vt.UserID); err != nil {
		slog.Error("failed to mark email as verified", "error", err, "user_id", vt.UserID)
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to verify email")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	r.email.InvalidateToken(req.Context(), token)

	response.JSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// --- Health + Readiness ---

func (r *Router) healthHandler(w http.ResponseWriter, req *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

// csrfHandler issues a valid CSRF token for non-browser clients (curl, CLI,
// scripts) that authenticate with a bearer JWT. CSRF is enforced on all
// state-changing protected routes, but there was previously no public way to
// obtain a token, which made the documented API-key bootstrap flow impossible.
//
// Response: {"csrf_token": "<token>"} — send it back in the X-CSRF-Token
// header on subsequent state-changing requests (e.g. POST /api/v1/api-keys).
func (r *Router) csrfHandler(w http.ResponseWriter, req *http.Request) {
	if r.csrf == nil {
		response.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "CSRF protection not configured"})
		return
	}
	r.csrf.SetToken(w, req)
	token := w.Header().Get("X-CSRF-Token")
	if token == "" {
		response.JSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to generate CSRF token"})
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"csrf_token": token})
}

func (r *Router) readinessHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	checks := map[string]string{}
	allHealthy := true

	if r.db != nil {
		if err := r.db.HealthCheck(ctx); err != nil {
			checks["postgres"] = "unhealthy"
			allHealthy = false
		} else {
			// Include pool stats for operational monitoring
			if r.db.Pool != nil {
				stats := r.db.Pool.Stat()
				checks["postgres"] = fmt.Sprintf("healthy (acquired=%d idle=%d conns=%d)",
					stats.AcquiredConns(), stats.IdleConns(), stats.TotalConns())
			} else {
				checks["postgres"] = "healthy"
			}
		}
	} else {
		checks["postgres"] = "not configured"
		allHealthy = false
	}

	if r.rds != nil {
		if err := r.rds.HealthCheck(ctx); err != nil {
			checks["redis"] = "unhealthy"
			allHealthy = false
		} else {
			checks["redis"] = "healthy"
		}
	} else {
		checks["redis"] = "not configured"
		allHealthy = false
	}

	if r.nats != nil {
		if err := r.nats.HealthCheck(); err != nil {
			checks["nats"] = "unhealthy"
			allHealthy = false
		} else {
			checks["nats"] = "healthy"
		}
	} else {
		checks["nats"] = "not configured"
		allHealthy = false
	}

	status := http.StatusOK
	if !allHealthy {
		status = http.StatusServiceUnavailable
	}

	response.JSON(w, status, map[string]interface{}{
		"status": map[string]bool{"ready": allHealthy},
		"checks": checks,
	})
}

// --- Auth Handlers ---

// logoutHandler revokes the current JWT token.
func (r *Router) logoutHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	// Revoke the current token via blacklist if available
	if r.blacklist != nil {
		tokenStr := mw.ExtractBearerToken(req)
		if tokenStr != "" {
			if err := r.blacklist.Revoke(req.Context(), tokenStr, 24*time.Hour); err != nil {
				slog.Warn("failed to revoke token", "error", err, "user_id", claims.UserID)
			}
		}
	}

	// Log the logout event
	if r.auditLogger != nil {
		r.auditLogger.LogAuthEvent(req.Context(), claims.UserID, "logout", req.RemoteAddr, "user logged out")
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "logged out successfully"})
}

// changePasswordHandler changes the user's password and revokes all tokens.
func (r *Router) changePasswordHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.CurrentPassword == "" || input.NewPassword == "" {
		response.BadRequest(w, "current_password and new_password are required")
		return
	}
	if len(input.NewPassword) < 12 {
		response.BadRequest(w, "new password must be at least 12 characters")
		return
	}

	// Verify current password
	user, err := r.users.FindByID(req.Context(), claims.UserID)
	if err != nil {
		response.NotFound(w, "user not found")
		return
	}
	if !auth.CheckPassword(input.CurrentPassword, user.PasswordHash) {
		response.Unauthorized(w, "current password is incorrect")
		return
	}

	// Hash new password
	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		response.InternalError(w, "failed to hash password")
		return
	}
	if err := r.users.UpdatePassword(req.Context(), user.ID, hash); err != nil {
		response.InternalError(w, "failed to update password")
		return
	}

	// Revoke ALL tokens for this user (force re-login)
	if r.blacklist != nil {
		if err := r.blacklist.RevokeAllForUser(req.Context(), user.ID); err != nil {
			// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
			slog.Warn("failed to revoke all user tokens", "error", err, "user_id", user.ID)
		}
	}

	// Log the password change
	if r.auditLogger != nil {
		r.auditLogger.LogAuthEvent(req.Context(), user.ID, "password_changed", req.RemoteAddr, "user changed password")
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "password changed. All sessions have been invalidated."})
}

func (r *Router) registerHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)

	v.Required("email", input.Email)
	v.Required("password", input.Password)
	v.Required("name", input.Name)
	v.Email("email", input.Email)
	v.MinLength("password", input.Password, 12)

	if v.WriteResponse(w, req) {
		return
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrHashFailed, "failed to hash password")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	user := &repository.User{
		Email:        input.Email,
		PasswordHash: hash,
		Name:         input.Name,
		Role:         "user",
	}
	if err := r.users.Create(req.Context(), user); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique") {
			apiErr := apperrors.New(apperrors.ErrDuplicateEmail, "email already registered")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create user")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	token, err := r.auth.GenerateToken(user.ID, user.Email, user.Role, "")
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrScanFailed, "failed to generate token")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	// Send verification email (best-effort)
	if r.email != nil {
		baseURL := r.getBaseURLFromConfig()
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
					slog.Error("panic in email verification goroutine", "panic", rec, "user_id", user.ID)
				}
			}()
			// Use timeout context since request context is canceled after response
			// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := r.email.SendVerificationEmail(ctx, user.ID, user.Email, baseURL); err != nil {
				slog.Error("failed to send verification email", "error", err, "user_id", user.ID)
			}
		}()
	}

	response.Created(w, map[string]string{"token": token, "user_id": user.ID})
}

func (r *Router) loginHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if r.lockout.IsLocked(req.Context(), input.Email) {
		remaining := r.lockout.GetRemainingLockout(req.Context(), input.Email)
		w.Header().Set("Retry-After", fmt.Sprintf("%.0f", remaining.Seconds()))
		apiErr := apperrors.New(apperrors.ErrAccountLocked, "account locked due to too many failed attempts")
		response.JSON(w, apiErr.HTTPStatus(), map[string]interface{}{
			"code":        apiErr.Code,
			"error":       apiErr.Message,
			"retry_after": remaining.Seconds(),
		})
		return
	}

	user, err := r.users.FindByEmail(req.Context(), input.Email)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidCredentials, "invalid credentials")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if !auth.CheckPassword(input.Password, user.PasswordHash) {
		r.lockout.RecordFailure(req.Context(), input.Email)
		if r.loginRateLimiter != nil {
			r.loginRateLimiter.RecordFailure(req.Context(), extractLoginIP(req), input.Email)
		}
		apiErr := apperrors.New(apperrors.ErrInvalidCredentials, "invalid credentials")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if !user.IsActive {
		apiErr := apperrors.New(apperrors.ErrAccountDisabled, "account is disabled")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	r.lockout.RecordSuccess(req.Context(), input.Email)
	if r.loginRateLimiter != nil {
		r.loginRateLimiter.RecordSuccess(req.Context(), extractLoginIP(req), input.Email)
	}

	if err := r.users.UpdateLastLogin(req.Context(), user.ID); err != nil {
		// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
		slog.Warn("failed to update last login", "error", err, "user_id", user.ID)
	}

	token, err := r.auth.GenerateToken(user.ID, user.Email, user.Role, "")
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrTokenInvalid, "failed to generate token")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"token": token})
}

func (r *Router) refreshTokenHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	newToken, err := r.auth.GenerateToken(claims.UserID, claims.Email, claims.Role, claims.OrgID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrTokenInvalid, "failed to generate token")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"token": newToken})
}

func (r *Router) currentUserHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	user, err := r.users.FindByID(req.Context(), claims.UserID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrNotFound, "user not found")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	type userResponse struct {
		ID          string     `json:"id"`
		Email       string     `json:"email"`
		Name        string     `json:"name"`
		AvatarURL   string     `json:"avatar_url,omitempty"`
		Role        string     `json:"role"`
		IsActive    bool       `json:"is_active"`
		LastLoginAt *time.Time `json:"last_login_at,omitempty"`
		CreatedAt   time.Time  `json:"created_at"`
		UpdatedAt   time.Time  `json:"updated_at"`
	}

	resp := userResponse{
		ID:          user.ID,
		Email:       user.Email,
		Name:        user.Name,
		AvatarURL:   user.AvatarURL,
		Role:        user.Role,
		IsActive:    user.IsActive,
		LastLoginAt: user.LastLoginAt,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
	response.JSON(w, http.StatusOK, resp)
}

func (r *Router) updateProfileHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	var input struct {
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if err := r.users.UpdateProfilePartial(req.Context(), claims.UserID, input.Name, input.AvatarURL); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update profile")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}

func (r *Router) createOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	input.Name = strings.TrimSpace(input.Name)

	v.Required("name", input.Name)

	if v.WriteResponse(w, req) {
		return
	}
	slug := strings.ToLower(strings.ReplaceAll(input.Name, " ", "-"))
	org := &repository.Organization{
		Name:        input.Name,
		Slug:        slug,
		Description: input.Description,
		OwnerID:     claims.UserID,
		Plan:        "free",
	}
	if err := r.orgs.Create(req.Context(), org); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			apiErr := apperrors.New(apperrors.ErrAlreadyExists, "organization slug already exists")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create organization")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if err := r.orgs.AddMember(req.Context(), org.ID, claims.UserID, "owner"); err != nil {
		slog.Warn("failed to add owner as member", "error", err)
	}

	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type:    "organization.created",
			Payload: map[string]interface{}{"org_id": org.ID, "name": org.Name, "slug": org.Slug},
		})
	}

	response.Created(w, org)
}

func (r *Router) listOrgsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	orgs, err := r.orgs.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalErrorR(w, req, "failed to list organizations")
		return
	}
	if orgs == nil {
		orgs = []repository.Organization{}
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(orgs, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

func (r *Router) getOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := chi.URLParam(req, "orgID")
	org, err := r.requireOrgMemberWithOrg(req.Context(), orgID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	response.JSON(w, http.StatusOK, org)
}

func (r *Router) updateOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := chi.URLParam(req, "orgID")
	if err := r.requireOrgOwner(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Plan        string                 `json:"plan"`
		Settings    map[string]interface{} `json:"settings"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if err := r.orgs.Update(req.Context(), orgID, input.Name, input.Description, input.Plan, input.Settings); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update organization")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "organization.updated", Payload: map[string]interface{}{"org_id": orgID},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "organization updated"})
}

func (r *Router) deleteOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := chi.URLParam(req, "orgID")
	if err := r.requireOrgOwner(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	if err := r.orgs.Delete(req.Context(), orgID); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to delete organization")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "organization.deleted", Payload: map[string]interface{}{"org_id": orgID},
		})
	}
	response.NoContent(w)
}

func (r *Router) createProjectHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	var input struct {
		OrgID       string `json:"org_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	input.Name = strings.TrimSpace(input.Name)

	v.Required("org_id", input.OrgID)
	v.Required("name", input.Name)

	if v.WriteResponse(w, req) {
		return
	}
	member, err := r.orgs.IsMember(req.Context(), input.OrgID, claims.UserID)
	if err != nil || !member {
		apiErr := apperrors.New(apperrors.ErrInsufficientPerms, "access denied to organization")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	project := &repository.Project{
		OrgID:       input.OrgID,
		Name:        input.Name,
		Description: input.Description,
		Status:      "active",
	}
	if err := r.projects.Create(req.Context(), project); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create project")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type:    "project.created",
			Payload: map[string]interface{}{"project_id": project.ID, "name": project.Name, "org_id": project.OrgID},
		})
	}
	response.Created(w, project)
}

func (r *Router) listProjectsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequestR(w, req, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.ForbiddenR(w, req, "access denied to organization")
		return
	}
	projects, err := r.projects.ListByOrg(req.Context(), orgID)
	if err != nil {
		response.InternalErrorR(w, req, "failed to list projects")
		return
	}
	if projects == nil {
		projects = []repository.Project{}
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(projects, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

func (r *Router) getProjectHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	project, err := r.requireProjectMember(req.Context(), projectID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	response.JSON(w, http.StatusOK, project)
}

func (r *Router) updateProjectHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	_, err := r.requireProjectMember(req.Context(), projectID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50)
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if err := r.projects.Update(req.Context(), projectID, input.Name, input.Description, input.Status); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update project")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "project.updated", Payload: map[string]interface{}{"project_id": projectID},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "project updated"})
}

func (r *Router) deleteProjectHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	if err := r.projects.Delete(req.Context(), projectID); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to delete project")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "project.deleted", Payload: map[string]interface{}{"project_id": projectID},
		})
	}
	response.NoContent(w)
}

func (r *Router) createAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.ForbiddenR(w, req, "access denied")
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Config      map[string]interface{} `json:"config"`
	}
	v, ok := validation.DecodeAndValidate(w, req, &input)
	if !ok {
		return
	}

	input.Name = strings.TrimSpace(input.Name)

	v.Required("name", input.Name)

	if v.WriteResponse(w, req) {
		return
	}
	agent := &repository.Agent{
		ProjectID:   projectID,
		Name:        input.Name,
		Description: input.Description,
		Config:      input.Config,
		Status:      "idle",
	}
	if err := r.agents.Create(req.Context(), agent); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create agent")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type:    "agent.created",
			Payload: map[string]interface{}{"agent_id": agent.ID, "project_id": projectID, "name": agent.Name},
		})
	}
	response.Created(w, agent)
}

func (r *Router) listAgentsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.ForbiddenR(w, req, "access denied")
		return
	}
	agents, err := r.agents.ListByProject(req.Context(), projectID)
	if err != nil {
		response.InternalErrorR(w, req, "failed to list agents")
		return
	}
	if agents == nil {
		agents = []repository.Agent{}
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(agents, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

func (r *Router) getAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	response.JSON(w, http.StatusOK, agent)
}

func (r *Router) updateAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Status      string                 `json:"status"`
		Config      map[string]interface{} `json:"config"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	var config map[string]interface{}
	if input.Config != nil {
		config = input.Config
	} else {
		config = agent.Config
	}
	if err := r.agents.Update(req.Context(), agentID, input.Name, input.Description, input.Status, config); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update agent")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "agent.updated", Payload: map[string]interface{}{"agent_id": agentID},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "agent updated"})
}

func (r *Router) deleteAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	agentID := chi.URLParam(req, "agentID")
	if _, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	if err := r.agents.Delete(req.Context(), agentID); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to delete agent")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "agent.deleted", Payload: map[string]interface{}{"agent_id": agentID},
		})
	}
	response.NoContent(w)
}

func (r *Router) createSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	session := &repository.Session{
		ProjectID: agent.ProjectID,
		AgentID:   agentID,
		UserID:    claims.UserID,
		Status:    "active",
	}
	if err := r.sessions.Create(req.Context(), session); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create session")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	// Track active sessions gauge for Grafana dashboard.
	telemetry.ActiveSessions.WithLabelValues(agent.ProjectID).Inc()
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "session.created",
			Payload: map[string]interface{}{
				"session_id": session.ID, "agent_id": agentID,
				"project_id": agent.ProjectID, "user_id": claims.UserID,
			},
		})
	}
	response.Created(w, session)
}

func (r *Router) listSessionsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.UnauthorizedR(w, req, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	if _, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID); err != nil {
		response.ForbiddenR(w, req, "access denied")
		return
	}
	sessions, err := r.sessions.ListByAgent(req.Context(), agentID)
	if err != nil {
		response.InternalErrorR(w, req, "failed to list sessions")
		return
	}
	if sessions == nil {
		sessions = []repository.Session{}
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(sessions, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

func (r *Router) getSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, _, err := r.requireSessionMember(req.Context(), sessionID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	response.JSON(w, http.StatusOK, session)
}

func (r *Router) updateSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, _, err := r.requireSessionMember(req.Context(), sessionID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if input.Status == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "status is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if input.Status == "completed" {
		if err := r.sessions.EndSession(req.Context(), sessionID); err != nil {
			apiErr := apperrors.New(apperrors.ErrDBError, "failed to end session")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		telemetry.ActiveSessions.WithLabelValues(session.ProjectID).Dec()
	} else {
		if err := r.sessions.Update(req.Context(), sessionID, input.Status); err != nil {
			apiErr := apperrors.New(apperrors.ErrDBError, "failed to update session")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		// Decrement active sessions gauge on terminal states.
		if input.Status == "completed" || input.Status == "failed" {
			telemetry.ActiveSessions.WithLabelValues(session.ProjectID).Dec()
		}
	}
	if r.webhookEngine != nil {
		var lifecycleEvent string
		switch input.Status {
		case "completed":
			lifecycleEvent = "session.completed"
		case "failed":
			lifecycleEvent = "session.failed"
		case "active":
			lifecycleEvent = "session.active"
		default:
			lifecycleEvent = "session.updated"
		}
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: lifecycleEvent,
			Payload: map[string]interface{}{
				"session_id": sessionID, "agent_id": session.AgentID,
				"project_id": session.ProjectID, "user_id": claims.UserID, "status": input.Status,
			},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "session updated"})
}

func (r *Router) createEventsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	_, _, err := r.requireSessionMember(req.Context(), sessionID, claims.UserID)
	if err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		EventType  string                 `json:"event_type"`
		Source     string                 `json:"source"`
		Payload    map[string]interface{} `json:"payload"`
		TokensUsed int                    `json:"tokens_used"`
		CostUsd    float64                `json:"cost_usd"`
		LatencyMs  int                    `json:"latency_ms"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.EventType = strings.TrimSpace(input.EventType)
	if input.EventType == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "event_type is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	event := &repository.Event{
		SessionID:  sessionID,
		EventType:  input.EventType,
		Source:     input.Source,
		Payload:    input.Payload,
		TokensUsed: input.TokensUsed,
		CostUsd:    input.CostUsd,
		LatencyMs:  input.LatencyMs,
	}
	if err := r.events.Create(req.Context(), event); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to create event")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	response.Created(w, event)
}

func (r *Router) batchEventsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	if _, _, err := r.requireSessionMember(req.Context(), sessionID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input []struct {
		EventType  string                 `json:"event_type"`
		Source     string                 `json:"source"`
		Payload    map[string]interface{} `json:"payload"`
		TokensUsed int                    `json:"tokens_used"`
		CostUsd    float64                `json:"cost_usd"`
		LatencyMs  int                    `json:"latency_ms"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if len(input) == 0 {
		apiErr := apperrors.New(apperrors.ErrMissingField, "events array is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	events := make([]repository.Event, len(input))
	for i, e := range input {
		events[i] = repository.Event{
			SessionID:  sessionID,
			EventType:  strings.TrimSpace(e.EventType),
			Source:     e.Source,
			Payload:    e.Payload,
			TokensUsed: e.TokensUsed,
			CostUsd:    e.CostUsd,
			LatencyMs:  e.LatencyMs,
		}
	}
	if err := r.events.BatchCreate(req.Context(), events); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to batch create events")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	response.JSON(w, http.StatusCreated, map[string]int{"created": len(events)})
}

func (r *Router) costAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	from, to, err := parseTimeRange(req)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	summary, err := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get cost analytics"))
		return
	}
	response.JSON(w, http.StatusOK, summary)
}

func (r *Router) tokenAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	from, to, err := parseTimeRange(req)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	summary, err := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get token analytics"))
		return
	}
	response.JSON(w, http.StatusOK, summary)
}

func (r *Router) sessionAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	stats, err := r.events.GetSessionStatsByOrg(req.Context(), orgID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get session analytics"))
		return
	}
	response.JSON(w, http.StatusOK, stats)
}

func (r *Router) dashboardOverviewHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	stats, err := r.events.GetSessionStatsByOrg(req.Context(), orgID)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get overview"))
		return
	}
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now()
	costSummary, costErr := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	tokenSummary, tokenErr := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	topAgents, agentsErr := r.events.GetTopAgentsByOrg(req.Context(), orgID, 5)

	warnings := []string{}
	if costErr != nil {
		warnings = append(warnings, "cost data unavailable")
	}
	if tokenErr != nil {
		warnings = append(warnings, "token data unavailable")
	}
	if agentsErr != nil {
		warnings = append(warnings, "top agents data unavailable")
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"sessions":   stats,
		"cost_30d":   costSummary,
		"tokens_30d": tokenSummary,
		"top_agents": topAgents,
		"warnings":   warnings,
	})
}

func (r *Router) dashboardActivityHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	activity, err := r.events.GetRecentActivity(req.Context(), orgID, 20)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get activity"))
		return
	}
	response.JSON(w, http.StatusOK, activity)
}

func (r *Router) dashboardTopAgentsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.JSON(w, http.StatusBadRequest, apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required"))
		return
	}
	if err := r.requireOrgMember(req.Context(), orgID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	agents, err := r.events.GetTopAgentsByOrg(req.Context(), orgID, 10)
	if err != nil {
		response.JSON(w, http.StatusInternalServerError, apperrors.New(apperrors.ErrDBError, "failed to get top agents"))
		return
	}
	response.JSON(w, http.StatusOK, agents)
}

func parseTimeRange(req *http.Request) (time.Time, time.Time, error) {
	to := time.Now()
	from := to.AddDate(0, 0, -30)
	if f := req.URL.Query().Get("from"); f != "" {
		t, err := time.Parse("2006-01-02", f)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'from' date: %w", err)
		}
		from = t
	}
	if t := req.URL.Query().Get("to"); t != "" {
		parsed, err := time.Parse("2006-01-02", t)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'to' date: %w", err)
		}
		to = parsed
	}
	return from, to, nil
}

const maxRequestBodySize = 2 << 20

func limitBodySize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Body != nil {
			req.Body = http.MaxBytesReader(w, req.Body, maxRequestBodySize)
		}
		next.ServeHTTP(w, req)
	})
}

// extractLoginIP extracts the client IP from RemoteAddr for login rate limiting.
func extractLoginIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = ip[:idx]
	}
	return ip
}

// redactToken returns a safe-to-log prefix of a credential (first 6 chars)
// plus a count of remaining chars — never the full value. Keeps auth-failure
// logs diagnosable without leaking secrets.
func redactToken(token string) string {
	if len(token) <= 6 {
		return "***"
	}
	return token[:6] + "…"
}

func (r *Router) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var claims *auth.Claims

		if r.apiKeyAuth != nil {
			c, err := r.apiKeyAuth.Authenticate(req)
			if err != nil {
				apiErr := apperrors.New(apperrors.ErrAPIKeyInvalid, "invalid API key")
				response.JSON(w, apiErr.HTTPStatus(), apiErr)
				return
			}
			if c != nil {
				claims = c
			}
		}

		if claims == nil {
			authHeader := req.Header.Get("Authorization")
			if authHeader == "" {
				apiErr := apperrors.New(apperrors.ErrMissingAuth, "missing authorization header")
				response.JSON(w, apiErr.HTTPStatus(), apiErr)
				return
			}
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				apiErr := apperrors.New(apperrors.ErrTokenInvalid, "invalid authorization format")
				response.JSON(w, apiErr.HTTPStatus(), apiErr)
				return
			}
			c, err := r.auth.ValidateToken(parts[1])
			if err != nil {
				// Redacted auth-failure observability: log the token's shape (never
				// the value) so misconfigured clients — e.g. an extension that
				// stored a stale or non-API-key secret — can be diagnosed from the
				// API log alone.
				slog.Warn("auth: JWT validation failed",
					"error", err,
					"token_prefix", redactToken(parts[1]),
					"token_len", len(parts[1]),
					"has_underscore", strings.Contains(parts[1], "_"),
					"has_dot", strings.Contains(parts[1], "."),
				)
				apiErr := apperrors.New(apperrors.ErrTokenExpired, "invalid or expired token")
				response.JSON(w, apiErr.HTTPStatus(), apiErr)
				return
			}
			claims = c
		}

		ctx := auth.ContextWithClaims(req.Context(), claims)

		if r.db != nil && r.db.Pool != nil {
			conn, err := r.db.Pool.Acquire(req.Context())
			if err != nil {
				slog.Warn("auth: failed to acquire DB connection for RLS", "error", err)
			} else {
				defer conn.Release()
				if _, err := conn.Exec(req.Context(), "SELECT app_auth.set_current_user_id($1)", claims.UserID); err != nil {
					slog.Debug("auth: failed to set RLS session user", "error", err)
				} else {
					ctx = database.WithConn(ctx, conn)
					slog.Debug("auth: set RLS session user", "user_id", claims.UserID)
				}
			}
		}

		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

func (r *Router) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		claims, ok := auth.ClaimsFromContext(req.Context())
		if !ok {
			apiErr := apperrors.New(apperrors.ErrMissingAuth, "unauthorized")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		if claims.Role != "admin" && claims.Role != "superadmin" {
			apiErr := apperrors.New(apperrors.ErrInsufficientPerms, "insufficient permissions")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func (r *Router) authRateLimitMiddleware(next http.Handler) http.Handler {
	if r.authRL == nil {
		slog.Warn("auth rate limiting disabled: Redis-backed limiter not configured")
		return next // pass through when no rate limiter configured
	}
	return r.authRL.Middleware(func(req *http.Request) string {
		return mw.RateLimitByIPKey(req)
	})(next)
}

func (r *Router) eventsRateLimitMiddleware(next http.Handler) http.Handler {
	if r.rl == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			slog.Warn("events rate limiting disabled: Redis-backed limiter not configured")
			response.ErrorR(w, req, http.StatusServiceUnavailable, "INFRA_002", "rate limiting not available")
		})
	}
	return r.rl.Middleware(func(req *http.Request) string {
		claims, ok := auth.ClaimsFromContext(req.Context())
		if ok {
			return "user:" + claims.UserID
		}
		return "ip:" + req.RemoteAddr
	})(next)
}

func (r *Router) metricsHandler(w http.ResponseWriter, req *http.Request) {
	h := telemetry.MetricsHandler()
	if h != nil {
		h.ServeHTTP(w, req)
	} else {
		response.ErrorR(w, req, http.StatusServiceUnavailable, "INFRA_002", "metrics not available")
	}
}
