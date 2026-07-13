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
	apperrors "github.com/vigilagent/vigilagent/internal/errors"
	"github.com/vigilagent/vigilagent/internal/compression"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/database"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/requestid"
	"github.com/vigilagent/vigilagent/internal/slogger"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// rlHeaders is the in-memory rate limit headers middleware, set once in setupMiddleware.
var rlHeaders *mw.RateLimitHeadersMiddleware

func (r *Router) setupMiddleware() {
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/health"))
	r.Use(requestid.Middleware)
	r.Use(slogger.Middleware)
	r.Use(compression.Middleware)
	r.useCORSFromConfig()
	r.Use(r.securityHeadersMiddleware)
	timeout := 30 * time.Second
	r.Use(func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, timeout, `{"error":"request timeout"}`)
	})

	// Rate limit headers on ALL responses (in-memory, informational)
	rlHeaders = mw.NewRateLimitHeadersMiddleware(10000, time.Minute)
	r.Use(rlHeaders.Middleware(func(req *http.Request) string {
		if claims, ok := auth.ClaimsFromContext(req.Context()); ok {
			return "user:" + claims.UserID
		}
		return mw.RateLimitByIPKey(req)
	}))
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

func (r *Router) setupRoutes() {
	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Get("/health", r.healthHandler)
		v1.Get("/ready", r.readinessHandler)
		v1.Get("/metrics", r.metricsHandler)

		public := v1.Group(nil)
		public.Use(limitBodySize)
		public.Use(r.authRateLimitMiddleware)
		public.Use(mw.SanitizeMiddleware)
		{
			public.Post("/auth/register", r.registerHandler)
			public.Post("/auth/login", r.loginHandler)
			public.Post("/auth/forgot-password", r.forgotPasswordHandler)
			public.Post("/auth/reset-password", r.resetPasswordHandler)
			public.Get("/auth/verify-email", r.verifyEmailHandler)
		}

		protected := v1.Group(nil)
		protected.Use(limitBodySize)
		protected.Use(r.authMiddleware)
		protected.Use(r.apiKeyRateLimitMiddleware)
		{
			protected.Get("/users/me", r.currentUserHandler)
			protected.With(mw.JWTRotationMiddleware(mw.DefaultJWTRotationConfig(), r.auth)).Post("/auth/refresh",
				r.refreshTokenHandler,
			)
			protected.With(mw.RequireJWTRefresh(r.auth)).Put("/users/me",
				r.updateProfileHandler,
			)

			protected.Post("/organizations", r.createOrgHandler)
			protected.Get("/organizations", r.listOrgsHandler)
			protected.Get("/organizations/{orgID}", r.getOrgHandler)
			protected.Put("/organizations/{orgID}", r.updateOrgHandler)
			protected.Delete("/organizations/{orgID}", r.deleteOrgHandler)

			protected.Post("/projects", r.createProjectHandler)
			protected.Get("/projects", r.listProjectsHandler)
			protected.Get("/projects/{projectID}", r.getProjectHandler)
			protected.Put("/projects/{projectID}", r.updateProjectHandler)
			protected.Delete("/projects/{projectID}", r.deleteProjectHandler)

			protected.Post("/projects/{projectID}/agents", r.createAgentHandler)
			protected.Get("/projects/{projectID}/agents", r.listAgentsHandler)
			protected.Get("/agents/{agentID}", r.getAgentHandler)
			protected.Put("/agents/{agentID}", r.updateAgentHandler)
			protected.Delete("/agents/{agentID}", r.deleteAgentHandler)

			protected.Post("/agents/{agentID}/sessions", r.createSessionHandler)
			protected.Get("/agents/{agentID}/sessions", r.listSessionsHandler)
			protected.Get("/sessions/{sessionID}", r.getSessionHandler)
			protected.Put("/sessions/{sessionID}", r.updateSessionHandler)

			protected.Post("/tasks", r.createTaskHandler)
			protected.Get("/tasks", r.listTasksHandler)
			protected.Get("/tasks/{taskID}", r.getTaskHandler)
			protected.Post("/tasks/{taskID}/cancel", r.cancelTaskHandler)
			protected.Get("/tasks/{taskID}/stream", r.streamTaskHandler)
			protected.Post("/tasks/{taskID}/hitl", r.approveHITLHandler)

			protected.Post("/memory/search", r.searchMemoryHandler)
			protected.Post("/memory", r.createMemoryHandler)

			protected.Post("/scan", r.scanHandler)
			protected.Post("/review", r.reviewHandler)
			protected.Post("/requirements", r.requirementsHandler)
			protected.Post("/validate", r.validateHandler)
			protected.Post("/schema", r.schemaHandler)
			protected.Post("/compliance", r.complianceHandler)
			protected.Post("/validate-full", r.pipelineHandler)

			protected.Post("/knowledge", r.knowledgeHandler)
			protected.Post("/skills/extract", r.skillEngineHandler)
			protected.Post("/confidence", r.confidenceHandler)
			protected.Post("/attack-graph", r.attackGraphHandler)
			protected.Post("/audit/trace", r.auditHandler)

			protected.Post("/middleware/process", r.middlewareProcessHandler)
			protected.Get("/middleware/metrics", r.middlewareMetricsHandler)
			protected.Get("/middleware/patterns", r.middlewarePatternsHandler)

			events := protected.Group(nil)
			events.Use(r.eventsRateLimitMiddleware)
			{
				events.Post("/sessions/{sessionID}/events", r.createEventsHandler)
				events.Post("/sessions/{sessionID}/events/batch", r.batchEventsHandler)
			}

			protected.Get("/analytics/cost", r.costAnalyticsHandler)
			protected.Get("/analytics/tokens", r.tokenAnalyticsHandler)
			protected.Get("/analytics/sessions", r.sessionAnalyticsHandler)
			protected.Get("/analytics/cost-intel", r.costIntelDashboardHandler)
			protected.Get("/analytics/cost-intel/forecast", r.costIntelForecastHandler)
			protected.Get("/analytics/cost-intel/recommendations", r.costIntelRecommendationsHandler)
			protected.Get("/analytics/cost-intel/anomalies", r.costIntelAnomaliesHandler)

			protected.Post("/tasks/batch", r.batchTaskHandler)
			protected.Get("/providers/health", r.healthStatsHandler)
			protected.Post("/providers/cost-override", r.costOverrideHandler)

			protected.Get("/dashboard/overview", r.dashboardOverviewHandler)
			protected.Get("/dashboard/activity", r.dashboardActivityHandler)
			protected.Get("/dashboard/top-agents", r.dashboardTopAgentsHandler)

			skills := protected.Group(nil)
			{
				skills.Get("/skills", r.listSkillsHandler)
				skills.Get("/skills/{skillID}", r.getSkillHandler)
				skills.Post("/skills", r.createSkillHandler)
				skills.Put("/skills/{skillID}", r.updateSkillHandler)
				skills.Delete("/skills/{skillID}", r.deleteSkillHandler)
				skills.Post("/skills/{skillID}/rate", r.rateSkillHandler)
				skills.Get("/skills/{skillID}/ratings", r.listSkillRatingsHandler)
				skills.Post("/skills/{skillID}/install", r.installSkillHandler)
			}

		// Skill marketplace RAG endpoints — gated behind feature flag
		if r.skillRAG != nil {
			if r.featureFlags == nil || r.featureFlags.IsEnabled(context.Background(), "skill_rag") {
				ragHandlers := NewRAGHandlers(r.skillRAG, r.skills)
				ragHandlers.RegisterRoutes(protected)
			}
		}

			protected.Get("/alerts", r.listAlertsHandler)
			protected.Post("/alerts", r.createAlertHandler)
			protected.Get("/alerts/{alertID}", r.getAlertHandler)
			protected.Put("/alerts/{alertID}", r.updateAlertHandler)
			protected.Delete("/alerts/{alertID}", r.deleteAlertHandler)

			protected.Get("/billing/invoices", r.listInvoicesHandler)
			protected.Get("/billing/invoices/{invoiceID}", r.getInvoiceHandler)
			protected.Post("/billing/checkout", r.createCheckoutHandler)
			protected.Get("/billing/subscription", r.getSubscriptionHandler)
			protected.Post("/billing/portal", r.createBillingPortalHandler)

			protected.Post("/api-keys", r.createAPIKeyHandler)
			protected.Get("/api-keys", r.listAPIKeysHandler)
			protected.Delete("/api-keys/{keyID}", r.deleteAPIKeyHandler)

			protected.Post("/webhooks", r.createWebhookHandler)
			protected.Get("/webhooks", r.listWebhooksHandler)
			protected.Get("/webhooks/stats", r.webhookStatsHandler)
			protected.Get("/webhooks/{webhookID}", r.getWebhookHandler)
			protected.Delete("/webhooks/{webhookID}", r.deleteWebhookHandler)
			protected.Get("/webhooks/{webhookID}/deliveries", r.getWebhookDeliveriesHandler)

			admin := protected.Group(nil)
			admin.Use(r.adminMiddleware)
			{
				admin.Get("/admin/stats", r.adminStatsHandler)
				admin.Get("/admin/users", r.adminListUsersHandler)
				admin.Put("/admin/users/{userID}/role", r.adminUpdateUserRoleHandler)
				admin.Delete("/admin/users/{userID}", r.adminDeleteUserHandler)
			}

			protected.Get("/ws", r.handleWebSocket)

			if r.authSessionMiddleware != nil {
				protected.Get("/auth/session-check", r.authSessionMiddleware.AuthSessionCheckHandler)
			}
		}
	})
}


// --- Email Handlers ---
func (r *Router) forgotPasswordHandler(w http.ResponseWriter, req *http.Request) {
	// Rate limit this endpoint to prevent email bombing
	lockoutKey := "forgot-password:" + req.RemoteAddr
	if r.lockout != nil && r.lockout.IsLocked(req.Context(), lockoutKey) {
		response.JSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many requests, please try again later"})
		return
	}

	var input struct {
		Email string `json:"email"`
	}
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
		baseURL := fmt.Sprintf("http://%s", req.Host)
		if r.cfg != nil && r.cfg.Server.Env == "production" {
			baseURL = "https://" + req.Host
		}
		if err := r.email.SendPasswordResetEmail(req.Context(), user.ID, user.Email, baseURL); err != nil {
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
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if input.Token == "" || input.NewPassword == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "token and new_password are required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if len(input.NewPassword) < 12 {
		apiErr := apperrors.New(apperrors.ErrPasswordTooWeak, "password must be at least 12 characters")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	if r.email == nil {
		apiErr := apperrors.New(apperrors.ErrServiceDown, "email service not configured")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}

	vt, ok := r.email.ValidateToken(input.Token)
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

	r.email.InvalidateToken(input.Token)

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

	vt, ok := r.email.ValidateToken(token)
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

	r.email.InvalidateToken(token)

	response.JSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// --- Health + Readiness ---

func (r *Router) healthHandler(w http.ResponseWriter, req *http.Request) {
	response.JSON(w, http.StatusOK, map[string]string{"status": "healthy"})
}

func (r *Router) readinessHandler(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	checks := map[string]string{}
	allHealthy := true

	if r.db != nil {
		if err := r.db.HealthCheck(ctx); err != nil {
			checks["postgres"] = "unhealthy: " + err.Error()
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
			checks["redis"] = "unhealthy: " + err.Error()
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
			checks["nats"] = "unhealthy: " + err.Error()
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

func (r *Router) registerHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)
	if input.Email == "" || input.Password == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "email and password are required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if !strings.Contains(input.Email, "@") || !strings.Contains(input.Email, ".") {
		apiErr := apperrors.New(apperrors.ErrInvalidEmail, "invalid email address")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if len(input.Password) < 12 {
		apiErr := apperrors.New(apperrors.ErrPasswordTooWeak, "password must be at least 12 characters")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
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
		baseURL := fmt.Sprintf("http://%s", req.Host)
		if r.cfg != nil && r.cfg.Server.Env == "production" {
			baseURL = "https://" + req.Host
		}
		go func() {
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("panic in email verification goroutine", "panic", rec, "user_id", user.ID)
				}
			}()
			// Use timeout context since request context is canceled after response
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
			"code":       apiErr.Code,
			"error":      apiErr.Message,
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

	if err := r.users.UpdateLastLogin(req.Context(), user.ID); err != nil {
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

	response.JSON(w, http.StatusOK, user)
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
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if err := r.users.UpdateProfile(req.Context(), claims.UserID, input.Name, input.AvatarURL); err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to update profile")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}

func (r *Router) createOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		apiErr := apperrors.New(apperrors.ErrMissingAuth, "missing authentication")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "name is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
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
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgs, err := r.orgs.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to list organizations")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if orgs == nil {
		orgs = []repository.Organization{}
	}
	response.JSON(w, http.StatusOK, orgs)
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
		response.Unauthorized(w, "missing authentication")
		return
	}
	var input struct {
		OrgID       string `json:"org_id"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.OrgID == "" || input.Name == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "org_id and name are required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
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
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "org_id query parameter is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		apiErr := apperrors.New(apperrors.ErrInsufficientPerms, "access denied to organization")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	projects, err := r.projects.ListByOrg(req.Context(), orgID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to list projects")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if projects == nil {
		projects = []repository.Project{}
	}
	response.JSON(w, http.StatusOK, projects)
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
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	projectID := chi.URLParam(req, "projectID")
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Config      map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		apiErr := apperrors.New(apperrors.ErrInvalidBody, "invalid request body")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		apiErr := apperrors.New(apperrors.ErrMissingField, "name is required")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
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
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	projectID := chi.URLParam(req, "projectID")
	if _, err := r.requireProjectMember(req.Context(), projectID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	agents, err := r.agents.ListByProject(req.Context(), projectID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to list agents")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if agents == nil {
		agents = []repository.Agent{}
	}
	response.JSON(w, http.StatusOK, agents)
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
		response.JSON(w, http.StatusUnauthorized, apperrors.New(apperrors.ErrMissingAuth, "missing authentication"))
		return
	}
	agentID := chi.URLParam(req, "agentID")
	if _, _, err := r.requireAgentMember(req.Context(), agentID, claims.UserID); err != nil {
		response.JSON(w, http.StatusForbidden, apperrors.New(apperrors.ErrInsufficientPerms, "access denied"))
		return
	}
	sessions, err := r.sessions.ListByAgent(req.Context(), agentID)
	if err != nil {
		apiErr := apperrors.New(apperrors.ErrDBError, "failed to list sessions")
		response.JSON(w, apiErr.HTTPStatus(), apiErr)
		return
	}
	if sessions == nil {
		sessions = []repository.Session{}
	}
	response.JSON(w, http.StatusOK, sessions)
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
	} else {
		if err := r.sessions.Update(req.Context(), sessionID, input.Status); err != nil {
			apiErr := apperrors.New(apperrors.ErrDBError, "failed to update session")
			response.JSON(w, apiErr.HTTPStatus(), apiErr)
			return
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
	from, to := parseTimeRange(req)
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
	from, to := parseTimeRange(req)
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
	costSummary, _ := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	tokenSummary, _ := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	topAgents, _ := r.events.GetTopAgentsByOrg(req.Context(), orgID, 5)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"sessions":   stats,
		"cost_30d":   costSummary,
		"tokens_30d": tokenSummary,
		"top_agents": topAgents,
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

func parseTimeRange(req *http.Request) (time.Time, time.Time) {
	to := time.Now()
	from := to.AddDate(0, 0, -30)
	if f := req.URL.Query().Get("from"); f != "" {
		if t, err := time.Parse("2006-01-02", f); err == nil {
			from = t
		}
	}
	if t := req.URL.Query().Get("to"); t != "" {
		if parsed, err := time.Parse("2006-01-02", t); err == nil {
			to = parsed
		}
	}
	return from, to
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
			response.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rate limiting not available"})
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
		response.JSON(w, http.StatusServiceUnavailable, map[string]string{"error": "metrics not available"})
	}
}
