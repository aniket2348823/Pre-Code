package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/telemetry"
	"github.com/vigilagent/vigilagent/pkg/response"
)



func (r *Router) setupMiddleware() {
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Heartbeat("/health"))
	r.Use(requestIDMiddleware)
	r.Use(func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, 30*time.Second, "request timeout")
	})
	r.useCORS()
}

// useCORS applies CORS middleware from configuration.
func (r *Router) useCORS() {
	allowedOrigins := r.cfg.CORS.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"*"}
	}
	allowedMethods := r.cfg.CORS.AllowedMethods
	if len(allowedMethods) == 0 {
		allowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	allowedHeaders := r.cfg.CORS.AllowedHeaders
	if len(allowedHeaders) == 0 {
		allowedHeaders = []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Request-ID"}
	}

	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			origin := req.Header.Get("Origin")
			if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				for _, o := range allowedOrigins {
					if o == origin {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			w.Header().Set("Access-Control-Allow-Methods", joinStrings(allowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", joinStrings(allowedHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", "86400")

			if req.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, req)
		})
	})
}

// requestIDMiddleware propagates the X-Request-ID header into the response.
func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// The chi RequestID middleware already sets this in context; just echo it in the response.
		requestID := req.Header.Get("X-Request-ID")
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, req)
	})
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

func (r *Router) setupRoutes() {
	r.Route("/api/v1", func(v1 chi.Router) {
		v1.Get("/health", r.healthHandler)
		v1.Get("/ready", r.readinessHandler)

		// Prometheus metrics endpoint (accessible without auth for scraping)
		v1.Get("/metrics", r.metricsHandler)

		public := v1.Group(nil)
		{
			public.Post("/auth/register", r.registerHandler)
			public.Post("/auth/login", r.loginHandler)
		}

		protected := v1.Group(nil)
		protected.Use(r.authMiddleware)
		{
			protected.Get("/users/me", r.currentUserHandler)
			protected.Post("/auth/refresh", r.refreshTokenHandler)
			protected.Put("/users/me", r.updateProfileHandler)

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

			protected.Post("/memory/search", r.searchMemoryHandler)
			protected.Post("/memory", r.createMemoryHandler)

			events := protected.Group(nil)
			events.Use(r.eventsRateLimitMiddleware)
			{
				events.Post("/sessions/{sessionID}/events", r.createEventsHandler)
				events.Post("/sessions/{sessionID}/events/batch", r.batchEventsHandler)
			}

			protected.Get("/analytics/cost", r.costAnalyticsHandler)
			protected.Get("/analytics/tokens", r.tokenAnalyticsHandler)
			protected.Get("/analytics/sessions", r.sessionAnalyticsHandler)

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

			admin := protected.Group(nil)
			admin.Use(r.adminMiddleware)
			{
				admin.Get("/admin/stats", r.adminStatsHandler)
				admin.Get("/admin/users", r.adminListUsersHandler)
				admin.Put("/admin/users/{userID}/role", r.adminUpdateUserRoleHandler)
				admin.Delete("/admin/users/{userID}", r.adminDeleteUserHandler)
			}

			// WebSocket endpoint for real-time agent streaming (requires auth)
			protected.Get("/ws", r.handleWebSocket)
		}
	})
}


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
			checks["postgres"] = "healthy"
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

// registerHandler creates a new user account with email/password.
func (r *Router) registerHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)
	if input.Email == "" || input.Password == "" {
		response.BadRequest(w, "email and password are required")
		return
	}
	if !strings.Contains(input.Email, "@") || !strings.Contains(input.Email, ".") {
		response.BadRequest(w, "invalid email address")
		return
	}
	if len(input.Password) < 12 {
		response.BadRequest(w, "password must be at least 12 characters")
		return
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		response.InternalError(w, "failed to hash password")
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
			response.JSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
			return
		}
		response.InternalError(w, "failed to create user")
		return
	}

	token, err := r.auth.GenerateToken(user.ID, user.Email, user.Role, "")
	if err != nil {
		response.InternalError(w, "failed to generate token")
		return
	}

	response.Created(w, map[string]string{"token": token, "user_id": user.ID})
}

// loginHandler authenticates a user and returns a JWT token.
func (r *Router) loginHandler(w http.ResponseWriter, req *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	user, err := r.users.FindByEmail(req.Context(), input.Email)
	if err != nil {
		response.Unauthorized(w, "invalid credentials")
		return
	}

	if !auth.CheckPassword(input.Password, user.PasswordHash) {
		response.Unauthorized(w, "invalid credentials")
		return
	}

	if !user.IsActive {
		response.Forbidden(w, "account is disabled")
		return
	}

	_ = r.users.UpdateLastLogin(req.Context(), user.ID)

	token, err := r.auth.GenerateToken(user.ID, user.Email, user.Role, "")
	if err != nil {
		response.InternalError(w, "failed to generate token")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"token": token})
}

// refreshTokenHandler issues a new JWT from a valid existing token.
func (r *Router) refreshTokenHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	newToken, err := r.auth.GenerateToken(claims.UserID, claims.Email, claims.Role, claims.OrgID)
	if err != nil {
		response.InternalError(w, "failed to generate token")
		return
	}

	response.JSON(w, http.StatusOK, map[string]string{"token": newToken})
}

// currentUserHandler returns the currently authenticated user's profile.
func (r *Router) currentUserHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	user, err := r.users.FindByID(req.Context(), claims.UserID)
	if err != nil {
		response.NotFound(w, "user not found")
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
		response.BadRequest(w, "invalid request body")
		return
	}
	if err := r.users.UpdateProfile(req.Context(), claims.UserID, input.Name, input.AvatarURL); err != nil {
		response.InternalError(w, "failed to update profile")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "profile updated"})
}

func (r *Router) createOrgHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		response.BadRequest(w, "name is required")
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
			response.JSON(w, http.StatusConflict, map[string]string{"error": "organization slug already exists"})
			return
		}
		response.InternalError(w, "failed to create organization")
		return
	}
	// Add owner as admin member
	if err := r.orgs.AddMember(req.Context(), org.ID, claims.UserID, "owner"); err != nil {
		// Log but do not fail - org is already created
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
		response.InternalError(w, "failed to list organizations")
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
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	org, err := r.orgs.FindByID(req.Context(), orgID)
	if err != nil {
		response.NotFound(w, err.Error())
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
	owner, err := r.orgs.IsOwner(req.Context(), orgID, claims.UserID)
	if err != nil || !owner {
		response.Forbidden(w, "only the owner can update the organization")
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Plan        string                 `json:"plan"`
		Settings    map[string]interface{} `json:"settings"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if err := r.orgs.Update(req.Context(), orgID, input.Name, input.Description, input.Plan, input.Settings); err != nil {
		response.InternalError(w, "failed to update organization")
		return
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
	owner, err := r.orgs.IsOwner(req.Context(), orgID, claims.UserID)
	if err != nil || !owner {
		response.Forbidden(w, "only the owner can delete the organization")
		return
	}
	if err := r.orgs.Delete(req.Context(), orgID); err != nil {
		response.InternalError(w, "failed to delete organization")
		return
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
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.OrgID == "" || input.Name == "" {
		response.BadRequest(w, "org_id and name are required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), input.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied to organization")
		return
	}
	project := &repository.Project{
		OrgID:       input.OrgID,
		Name:        input.Name,
		Description: input.Description,
		Status:      "active",
	}
	if err := r.projects.Create(req.Context(), project); err != nil {
		response.InternalError(w, "failed to create project")
		return
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
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied to organization")
		return
	}
	projects, err := r.projects.ListByOrg(req.Context(), orgID)
	if err != nil {
		response.InternalError(w, "failed to list projects")
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
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
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
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	var input struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if err := r.projects.Update(req.Context(), projectID, input.Name, input.Description, input.Status); err != nil {
		response.InternalError(w, "failed to update project")
		return
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
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	if err := r.projects.Delete(req.Context(), projectID); err != nil {
		response.InternalError(w, "failed to delete project")
		return
	}
	response.NoContent(w)
}


func (r *Router) createAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Config      map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		response.BadRequest(w, "name is required")
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
		response.InternalError(w, "failed to create agent")
		return
	}
	response.Created(w, agent)
}

func (r *Router) listAgentsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	projectID := chi.URLParam(req, "projectID")
	project, err := r.projects.FindByID(req.Context(), projectID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	agents, err := r.agents.ListByProject(req.Context(), projectID)
	if err != nil {
		response.InternalError(w, "failed to list agents")
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
		response.Unauthorized(w, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, err := r.agents.FindByID(req.Context(), agentID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), agent.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	response.JSON(w, http.StatusOK, agent)
}

func (r *Router) updateAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, err := r.agents.FindByID(req.Context(), agentID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), agent.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	var input struct {
		Name        string                 `json:"name"`
		Description string                 `json:"description"`
		Status      string                 `json:"status"`
		Config      map[string]interface{} `json:"config"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	var config map[string]interface{}
	if input.Config != nil {
		config = input.Config
	} else {
		config = agent.Config
	}
	if err := r.agents.Update(req.Context(), agentID, input.Name, input.Description, input.Status, config); err != nil {
		response.InternalError(w, "failed to update agent")
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "agent updated"})
}

func (r *Router) deleteAgentHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, err := r.agents.FindByID(req.Context(), agentID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), agent.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	if err := r.agents.Delete(req.Context(), agentID); err != nil {
		response.InternalError(w, "failed to delete agent")
		return
	}
	response.NoContent(w)
}

func (r *Router) createSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, err := r.agents.FindByID(req.Context(), agentID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), agent.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	session := &repository.Session{
		ProjectID: agent.ProjectID,
		AgentID:   agentID,
		UserID:    claims.UserID,
		Status:    "active",
	}
	if err := r.sessions.Create(req.Context(), session); err != nil {
		response.InternalError(w, "failed to create session")
		return
	}
	response.Created(w, session)
}

func (r *Router) listSessionsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	agentID := chi.URLParam(req, "agentID")
	agent, err := r.agents.FindByID(req.Context(), agentID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), agent.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	sessions, err := r.sessions.ListByAgent(req.Context(), agentID)
	if err != nil {
		response.InternalError(w, "failed to list sessions")
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
		response.Unauthorized(w, "missing authentication")
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, err := r.sessions.FindByID(req.Context(), sessionID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), session.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	response.JSON(w, http.StatusOK, session)
}

func (r *Router) updateSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, err := r.sessions.FindByID(req.Context(), sessionID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), session.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Status == "" {
		response.BadRequest(w, "status is required")
		return
	}
	if input.Status == "completed" {
		if err := r.sessions.EndSession(req.Context(), sessionID); err != nil {
			response.InternalError(w, "failed to end session")
			return
		}
	} else {
		if err := r.sessions.Update(req.Context(), sessionID, input.Status); err != nil {
			response.InternalError(w, "failed to update session")
			return
		}
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "session updated"})
}


func (r *Router) createEventsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, err := r.sessions.FindByID(req.Context(), sessionID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), session.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
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
		response.BadRequest(w, "invalid request body")
		return
	}
	input.EventType = strings.TrimSpace(input.EventType)
	if input.EventType == "" {
		response.BadRequest(w, "event_type is required")
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
		response.InternalError(w, "failed to create event")
		return
	}
	response.Created(w, event)
}

func (r *Router) batchEventsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	sessionID := chi.URLParam(req, "sessionID")
	session, err := r.sessions.FindByID(req.Context(), sessionID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	project, err := r.projects.FindByID(req.Context(), session.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
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
		response.BadRequest(w, "invalid request body")
		return
	}
	if len(input) == 0 {
		response.BadRequest(w, "events array is required")
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
		response.InternalError(w, "failed to batch create events")
		return
	}
	response.JSON(w, http.StatusCreated, map[string]int{"created": len(events)})
}

func (r *Router) costAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	from, to := parseTimeRange(req)
	summary, err := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get cost analytics")
		return
	}
	response.JSON(w, http.StatusOK, summary)
}

func (r *Router) tokenAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	from, to := parseTimeRange(req)
	summary, err := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get token analytics")
		return
	}
	response.JSON(w, http.StatusOK, summary)
}

func (r *Router) sessionAnalyticsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	stats, err := r.events.GetSessionStatsByOrg(req.Context(), orgID)
	if err != nil {
		response.InternalError(w, "failed to get session analytics")
		return
	}
	response.JSON(w, http.StatusOK, stats)
}

func (r *Router) dashboardOverviewHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	stats, err := r.events.GetSessionStatsByOrg(req.Context(), orgID)
	if err != nil {
		response.InternalError(w, "failed to get overview")
		return
	}
	// Get cost summary for last 30 days
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now()
	costSummary, _ := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	tokenSummary, _ := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	topAgents, _ := r.events.GetTopAgentsByOrg(req.Context(), orgID, 5)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"sessions":      stats,
		"cost_30d":      costSummary,
		"tokens_30d":    tokenSummary,
		"top_agents":    topAgents,
	})
}

func (r *Router) dashboardActivityHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	activity, err := r.events.GetRecentActivity(req.Context(), orgID, 20)
	if err != nil {
		response.InternalError(w, "failed to get activity")
		return
	}
	response.JSON(w, http.StatusOK, activity)
}

func (r *Router) dashboardTopAgentsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}
	agents, err := r.events.GetTopAgentsByOrg(req.Context(), orgID, 10)
	if err != nil {
		response.InternalError(w, "failed to get top agents")
		return
	}
	response.JSON(w, http.StatusOK, agents)
}

// parseTimeRange extracts from/to time params, defaulting to last 30 days.
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

// Skills, Alerts, Billing, Admin, Memory handlers are implemented in:
// skills_handlers.go, alerts_handlers.go, billing_handlers.go, admin_handlers.go, memory_handlers.go

// authMiddleware validates JWT tokens or API keys on protected routes.
func (r *Router) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Try API key auth first (X-API-Key header or Bearer vga_... token)
		if r.apiKeyAuth != nil {
			claims, err := r.apiKeyAuth.Authenticate(req)
			if err != nil {
				response.Unauthorized(w, err.Error())
				return
			}
			if claims != nil {
				ctx := auth.ContextWithClaims(req.Context(), claims)
				next.ServeHTTP(w, req.WithContext(ctx))
				return
			}
		}

		// Fall back to JWT auth
		authHeader := req.Header.Get("Authorization")
		if authHeader == "" {
			response.JSON(w, http.StatusUnauthorized, map[string]string{"error": "missing authorization header"})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid authorization format"})
			return
		}
		claims, err := r.auth.ValidateToken(parts[1])
		if err != nil {
			response.JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid or expired token"})
			return
		}
		ctx := auth.ContextWithClaims(req.Context(), claims)
		next.ServeHTTP(w, req.WithContext(ctx))
	})
}

// adminMiddleware checks that the authenticated user has admin role.
func (r *Router) adminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		claims, ok := auth.ClaimsFromContext(req.Context())
		if !ok {
			response.JSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if claims.Role != "admin" && claims.Role != "superadmin" {
			response.JSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
			return
		}
		next.ServeHTTP(w, req)
	})
}

// eventsRateLimitMiddleware applies Redis-backed rate limiting to event ingestion.
func (r *Router) eventsRateLimitMiddleware(next http.Handler) http.Handler {
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
