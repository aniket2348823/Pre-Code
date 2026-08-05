package router

import (
	"context"

	"github.com/go-chi/chi/v5"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
)

// ── Route Table ───────────────────────────────────────────
//
// All chi route bindings live here, extracted from router.go
// so the engine/middleware setup stays focused on wiring.

func (r *Router) setupRoutes() {
	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Get("/health", r.healthHandler)
		v1.Get("/ready", r.readinessHandler)
		v1.Get("/docs", r.swaggerUIHandler)
		v1.Get("/docs/openapi.yaml", r.openapiSpecHandler)

		public := v1.Group(nil)
		public.Use(r.authRateLimitMiddleware)
		public.Use(mw.BodySizeLimiter(mw.DefaultBodySizeConfig()))
		public.Use(mw.SanitizeMiddleware)
		{
			public.Post("/auth/register", r.registerHandler)
			public.With(r.loginRateLimiter.Middleware()).Post("/auth/login", r.loginHandler)
			public.Post("/auth/forgot-password", r.forgotPasswordHandler)
			public.Post("/auth/reset-password", r.resetPasswordHandler)
			public.Get("/auth/verify-email", r.verifyEmailHandler)

			// Provider catalog — public, no auth required (used by VS Code extension, etc.)
			public.Get("/providers", r.listProvidersHandler)
			public.Get("/providers/{providerID}/models", r.listProviderModelsHandler)
			public.Get("/models/{modelID}", r.getModelHandler)
		}

		protected := v1.Group(nil)
		protected.Use(r.authMiddleware)
		protected.Use(r.apiKeyRateLimitMiddleware)
		protected.Use(mw.BodySizeLimiter(mw.DefaultBodySizeConfig()))

		// Read-only ETag cache config: 30s max-age, stale-while-revalidate 60s
		etagReadCfg := mw.DefaultETagConfig().
			WithPathPatterns([]string{
				"/api/v1/organizations",
				"/api/v1/projects",
				"/api/v1/agents",
				"/api/v1/sessions",
				"/api/v1/tasks",
				"/api/v1/skills",
				"/api/v1/alerts",
			})

		// Plan-based rate limiting + usage metering + quota enforcement
		if r.planRateLimiter != nil {
			protected.Use(r.planRateLimiter.Middleware(r.orgPlanExtractor))
		}
		if r.usageMetering != nil {
			protected.Use(r.usageMetering.Middleware(r.orgPlanExtractor))
		}
		if r.quotaEnforcer != nil {
			protected.Use(r.quotaEnforcer.Middleware(r.orgPlanExtractor))
		}

		// CSRF protection on all state-changing endpoints
		if r.csrf != nil {
			protected.Use(r.csrf.Middleware)
		}

		// Deep analysis — requires auth
		protected.Post("/deep-analyze", r.deepAnalyzeHandler)

		// Logout is protected — requires valid token to revoke
		protected.Post("/auth/logout", r.logoutHandler)
		protected.Put("/users/me/password", r.changePasswordHandler)

		{
			protected.Get("/users/me", r.currentUserHandler)
			protected.With(mw.JWTRotationMiddleware(mw.DefaultJWTRotationConfig(), r.auth)).Post("/auth/refresh",
				r.refreshTokenHandler,
			)
			protected.With(mw.RequireJWTRefresh(r.auth)).Put("/users/me",
				r.updateProfileHandler,
			)

			protected.With(mw.RequireScope("orgs:write")).Post("/organizations", r.createOrgHandler)
			protected.With(mw.RequireScope("orgs:read"), mw.ETagMiddleware(etagReadCfg)).Get("/organizations", r.listOrgsHandler)
			protected.With(mw.RequireScope("orgs:read"), mw.ETagMiddleware(etagReadCfg)).Get("/organizations/{orgID}", r.getOrgHandler)
			protected.With(mw.RequireScope("orgs:write")).Put("/organizations/{orgID}", r.updateOrgHandler)
			protected.With(mw.RequireScope("orgs:write")).Delete("/organizations/{orgID}", r.deleteOrgHandler)

			protected.With(mw.RequireScope("projects:write")).Post("/projects", r.createProjectHandler)
			protected.With(mw.RequireScope("projects:read"), mw.ETagMiddleware(etagReadCfg)).Get("/projects", r.listProjectsHandler)
			protected.With(mw.RequireScope("projects:read"), mw.ETagMiddleware(etagReadCfg)).Get("/projects/{projectID}", r.getProjectHandler)
			protected.With(mw.RequireScope("projects:write")).Put("/projects/{projectID}", r.updateProjectHandler)
			protected.With(mw.RequireScope("projects:write")).Delete("/projects/{projectID}", r.deleteProjectHandler)

			protected.With(mw.RequireScope("agents:write")).Post("/projects/{projectID}/agents", r.createAgentHandler)
			protected.With(mw.RequireScope("agents:read"), mw.ETagMiddleware(etagReadCfg)).Get("/projects/{projectID}/agents", r.listAgentsHandler)
			protected.With(mw.RequireScope("agents:read"), mw.ETagMiddleware(etagReadCfg)).Get("/agents/{agentID}", r.getAgentHandler)
			protected.With(mw.RequireScope("agents:write")).Put("/agents/{agentID}", r.updateAgentHandler)
			protected.With(mw.RequireScope("agents:write")).Delete("/agents/{agentID}", r.deleteAgentHandler)

			protected.With(mw.RequireScope("agents:write")).Post("/agents/{agentID}/sessions", r.createSessionHandler)
			protected.With(mw.RequireScope("agents:read"), mw.ETagMiddleware(etagReadCfg)).Get("/agents/{agentID}/sessions", r.listSessionsHandler)
			protected.With(mw.RequireScope("agents:read"), mw.ETagMiddleware(etagReadCfg)).Get("/sessions/{sessionID}", r.getSessionHandler)
			protected.With(mw.RequireScope("agents:write")).Put("/sessions/{sessionID}", r.updateSessionHandler)

			if r.idempotency != nil {
				protected.With(mw.RequireScope("tasks:write"), r.idempotency.AsMiddleware()).Post("/tasks", r.createTaskHandler)
			} else {
				protected.With(mw.RequireScope("tasks:write")).Post("/tasks", r.createTaskHandler)
			}
			protected.With(mw.RequireScope("tasks:read"), mw.ETagMiddleware(etagReadCfg)).Get("/tasks", r.listTasksHandler)
			protected.With(mw.RequireScope("tasks:read"), mw.ETagMiddleware(etagReadCfg)).Get("/tasks/{taskID}", r.getTaskHandler)
			protected.With(mw.RequireScope("tasks:write")).Post("/tasks/{taskID}/cancel", r.cancelTaskHandler)
			protected.With(mw.RequireScope("tasks:read")).Get("/tasks/{taskID}/stream", r.streamTaskHandler)
			protected.With(mw.RequireScope("tasks:write")).Post("/tasks/{taskID}/hitl", r.approveHITLHandler)

			protected.With(mw.RequireScope("memory:read")).Post("/memory/search", r.searchMemoryHandler)
			protected.With(mw.RequireScope("memory:write")).Post("/memory", r.createMemoryHandler)

			protected.With(mw.RequireScope("scan:write")).Post("/scan", r.scanHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/review", r.reviewHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/requirements", r.requirementsHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/validate", r.validateHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/schema", r.schemaHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/compliance", r.complianceHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/validate-full", r.pipelineHandler)

			protected.With(mw.RequireScope("scan:write")).Post("/knowledge", r.knowledgeHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/skills/extract", r.skillEngineHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/confidence", r.confidenceHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/attack-graph", r.attackGraphHandler)
			protected.With(mw.RequireScope("scan:write")).Post("/audit/trace", r.auditHandler)

			protected.With(mw.RequireScope("scan:write")).Post("/middleware/process", r.middlewareProcessHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/middleware/metrics", r.middlewareMetricsHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/middleware/patterns", r.middlewarePatternsHandler)

			events := protected.Group(nil)
			events.Use(r.eventsRateLimitMiddleware)
			events.Use(mw.RequireScope("agents:write"))
			{
				events.Post("/sessions/{sessionID}/events", r.createEventsHandler)
				events.Post("/sessions/{sessionID}/events/batch", r.batchEventsHandler)
			}

			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/cost", r.costAnalyticsHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/tokens", r.tokenAnalyticsHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/sessions", r.sessionAnalyticsHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/cost-intel", r.costIntelDashboardHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/cost-intel/forecast", r.costIntelForecastHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/cost-intel/recommendations", r.costIntelRecommendationsHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/analytics/cost-intel/anomalies", r.costIntelAnomaliesHandler)

			protected.With(mw.RequireScope("tasks:write")).Post("/tasks/batch", r.batchTaskHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/providers/health", r.healthStatsHandler)
			protected.With(mw.RequireScope("admin")).Post("/providers/cost-override", r.costOverrideHandler)

			protected.With(mw.RequireScope("analytics:read")).Get("/dashboard/overview", r.dashboardOverviewHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/dashboard/activity", r.dashboardActivityHandler)
			protected.With(mw.RequireScope("analytics:read")).Get("/dashboard/top-agents", r.dashboardTopAgentsHandler)

			skills := protected.Group(nil)
			{
				skills.With(mw.RequireScope("skills:read"), mw.ETagMiddleware(etagReadCfg)).Get("/skills", r.listSkillsHandler)
				skills.With(mw.RequireScope("skills:read"), mw.ETagMiddleware(etagReadCfg)).Get("/skills/{skillID}", r.getSkillHandler)
				skills.With(mw.RequireScope("skills:write")).Post("/skills", r.createSkillHandler)
				skills.With(mw.RequireScope("skills:write")).Put("/skills/{skillID}", r.updateSkillHandler)
				skills.With(mw.RequireScope("skills:write")).Delete("/skills/{skillID}", r.deleteSkillHandler)
				skills.With(mw.RequireScope("skills:write")).Post("/skills/{skillID}/rate", r.rateSkillHandler)
				skills.With(mw.RequireScope("skills:read"), mw.ETagMiddleware(etagReadCfg)).Get("/skills/{skillID}/ratings", r.listSkillRatingsHandler)
				skills.With(mw.RequireScope("skills:write")).Post("/skills/{skillID}/install", r.installSkillHandler)
			}

			// Skill marketplace RAG endpoints — gated behind feature flag
			if r.skillRAG != nil {
				if r.featureFlags == nil || r.featureFlags.IsEnabled(context.Background(), "skill_rag") {
					ragHandlers := NewRAGHandlers(r.skillRAG, r.skills)
					ragHandlers.RegisterRoutes(protected)
				}
			}

			protected.With(mw.RequireScope("alerts:read"), mw.ETagMiddleware(etagReadCfg)).Get("/alerts", r.listAlertsHandler)
			protected.With(mw.RequireScope("alerts:write")).Post("/alerts", r.createAlertHandler)
			protected.With(mw.RequireScope("alerts:read"), mw.ETagMiddleware(etagReadCfg)).Get("/alerts/{alertID}", r.getAlertHandler)
			protected.With(mw.RequireScope("alerts:write")).Put("/alerts/{alertID}", r.updateAlertHandler)
			protected.With(mw.RequireScope("alerts:write")).Delete("/alerts/{alertID}", r.deleteAlertHandler)

			protected.With(mw.RequireScope("billing:read"), mw.ETagMiddleware(etagReadCfg)).Get("/billing/invoices", r.listInvoicesHandler)
			protected.With(mw.RequireScope("billing:read"), mw.ETagMiddleware(etagReadCfg)).Get("/billing/invoices/{invoiceID}", r.getInvoiceHandler)
			if r.idempotency != nil {
				protected.With(mw.RequireScope("billing:write"), r.idempotency.AsMiddleware()).Post("/billing/checkout", r.createCheckoutHandler)
			} else {
				protected.With(mw.RequireScope("billing:write")).Post("/billing/checkout", r.createCheckoutHandler)
			}
			protected.With(mw.RequireScope("billing:read")).Get("/billing/subscription", r.getSubscriptionHandler)
			if r.idempotency != nil {
				protected.With(mw.RequireScope("billing:write"), r.idempotency.AsMiddleware()).Post("/billing/portal", r.createBillingPortalHandler)
			} else {
				protected.With(mw.RequireScope("billing:write")).Post("/billing/portal", r.createBillingPortalHandler)
			}

			protected.With(mw.RequireScope("api_keys:manage"), r.apiKeyCreateRateLimiter.Middleware()).Post("/api-keys", r.createAPIKeyHandler)
			protected.With(mw.RequireScope("api_keys:manage")).Get("/api-keys", r.listAPIKeysHandler)
			protected.With(mw.RequireScope("api_keys:manage")).Post("/api-keys/{keyID}/rotate", r.rotateAPIKeyHandler)
			protected.With(mw.RequireScope("api_keys:manage")).Delete("/api-keys/{keyID}", r.deleteAPIKeyHandler)

			protected.With(mw.RequireScope("webhooks:write")).Post("/webhooks", r.createWebhookHandler)
			protected.With(mw.RequireScope("webhooks:read")).Get("/webhooks", r.listWebhooksHandler)
			protected.With(mw.RequireScope("webhooks:read")).Get("/webhooks/stats", r.webhookStatsHandler)
			protected.With(mw.RequireScope("webhooks:read")).Get("/webhooks/{webhookID}", r.getWebhookHandler)
			protected.With(mw.RequireScope("webhooks:write")).Delete("/webhooks/{webhookID}", r.deleteWebhookHandler)
			protected.With(mw.RequireScope("webhooks:read")).Get("/webhooks/{webhookID}/deliveries", r.getWebhookDeliveriesHandler)

			// Webhook replay
			protected.With(mw.RequireScope("webhooks:write")).Post("/webhooks/replay", r.replayWebhookHandler)

			// Rate limit dashboard
			protected.With(mw.RequireScope("analytics:read")).Get("/ratelimit/dashboard", r.rateLimitDashboardHandler)

			// Export/Import
			protected.With(mw.RequireScope("agents:read")).Get("/export/conversations", r.exportConversationsHandler)
			protected.With(mw.RequireScope("skills:read")).Get("/export/skills", r.exportSkillsHandler)
			protected.With(mw.RequireScope("skills:write")).Post("/import", r.importDataHandler)

			// Feature flags (admin only)
			protected.With(mw.RequireScope("admin")).Get("/feature-flags", r.listFeatureFlagsHandler)
			protected.With(mw.RequireScope("admin")).Put("/feature-flags", r.updateFeatureFlagHandler)
			protected.With(mw.RequireScope("admin")).Delete("/feature-flags", r.deleteFeatureFlagHandler)
			protected.With(mw.RequireScope("agents:read")).Get("/feature-flags/check", r.checkFeatureFlagHandler)

			// Audit log retention (admin only)
			protected.With(mw.RequireScope("admin")).Post("/audit/cleanup", r.cleanupAuditLogsHandler)
			protected.With(mw.RequireScope("admin")).Get("/audit/retention", r.getAuditRetentionHandler)

			admin := protected.Group(nil)
			admin.Use(r.adminMiddleware)
			{
				admin.Get("/admin/stats", r.adminStatsHandler)
				admin.Get("/admin/users", r.adminListUsersHandler)
				admin.Put("/admin/users/{userID}/role", r.adminUpdateUserRoleHandler)
				admin.Delete("/admin/users/{userID}", r.adminDeleteUserHandler)
			}

			protected.With(mw.RequireScope("admin")).Get("/metrics", r.metricsHandler)

			// Team invitations
			protected.With(mw.RequireScope("orgs:write")).Post("/organizations/{orgID}/invitations", r.inviteMemberHandler)
			protected.With(mw.RequireScope("orgs:read")).Get("/organizations/{orgID}/invitations", r.listInvitationsHandler)
			protected.With(mw.RequireScope("orgs:write")).Delete("/organizations/{orgID}/invitations/{invitationID}", r.revokeInvitationHandler)
			protected.Post("/invitations/{token}/accept", r.acceptInvitationHandler)

			// Audit log viewer
			protected.With(mw.RequireScope("admin")).Get("/audit/logs", r.listAuditLogsHandler)

			// HITL queue endpoints
			if r.hitlQueue != nil {
				protected.With(mw.RequireScope("tasks:read")).Get("/hitl/pending", r.listHITLCheckpointsHandler)
				protected.With(mw.RequireScope("tasks:write")).Post("/hitl/decide", r.decideHITLHandler)
				protected.With(mw.RequireScope("tasks:read")).Get("/hitl/status", r.hitlStatusHandler)
			}

			// User session management
			protected.With(mw.RequireScope("agents:read")).Get("/users/me/sessions", r.listUserSessionsHandler)
			protected.With(mw.RequireScope("agents:read")).Get("/users/me/sessions/active", r.listActiveSessionsHandler)
			protected.With(mw.RequireScope("agents:write")).Post("/sessions/{sessionID}/invalidate", r.invalidateSessionHandler)

			protected.Get("/ws", r.handleWebSocket)

			// General batch endpoint — process multiple API operations
			protected.Post("/batch", r.batchHandler)

			if r.authSessionMiddleware != nil {
				protected.Get("/auth/session-check", r.authSessionMiddleware.AuthSessionCheckHandler)
			}
		}
	})
}
