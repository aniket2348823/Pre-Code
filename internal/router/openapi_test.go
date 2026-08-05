package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func openapiTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestGenerateOpenAPI_ValidSpec(t *testing.T) {
	spec := GenerateOpenAPI()
	require.NotNil(t, spec)

	assert.Equal(t, "3.0.3", spec.OpenAPI)
	assert.Equal(t, "VigilAgent API", spec.Info.Title)
	assert.Equal(t, "1.0.0", spec.Info.Version)
	assert.NotEmpty(t, spec.Servers)
	assert.Equal(t, "/api/v1", spec.Servers[0].URL)
}

func TestGenerateOpenAPI_AllPathsPresent(t *testing.T) {
	spec := GenerateOpenAPI()

	paths := []string{
		"/health",
		"/ready",
		"/auth/register",
		"/auth/login",
		"/auth/logout",
		"/auth/refresh",
		"/auth/forgot-password",
		"/auth/reset-password",
		"/auth/verify-email",
		"/users/me",
		"/users/me/password",
		"/organizations",
		"/organizations/{orgID}",
		"/projects",
		"/projects/{projectID}",
		"/projects/{projectID}/agents",
		"/agents/{agentID}",
		"/agents/{agentID}/sessions",
		"/sessions/{sessionID}",
		"/sessions/{sessionID}/events",
		"/sessions/{sessionID}/events/batch",
		"/tasks",
		"/tasks/{taskID}",
		"/tasks/{taskID}/cancel",
		"/tasks/{taskID}/stream",
		"/tasks/{taskID}/hitl",
		"/tasks/batch",
		"/memory/search",
		"/memory",
		"/skills",
		"/skills/{skillID}",
		"/skills/{skillID}/rate",
		"/skills/{skillID}/ratings",
		"/skills/{skillID}/install",
		"/alerts",
		"/alerts/{alertID}",
		"/analytics/cost",
		"/analytics/tokens",
		"/analytics/sessions",
		"/analytics/cost-intel",
		"/analytics/cost-intel/forecast",
		"/analytics/cost-intel/recommendations",
		"/analytics/cost-intel/anomalies",
		"/dashboard/overview",
		"/dashboard/activity",
		"/dashboard/top-agents",
		"/scan",
		"/review",
		"/requirements",
		"/validate",
		"/schema",
		"/compliance",
		"/validate-full",
		"/knowledge",
		"/skills/extract",
		"/confidence",
		"/attack-graph",
		"/audit/trace",
		"/middleware/process",
		"/middleware/metrics",
		"/middleware/patterns",
		"/api-keys",
		"/api-keys/{keyID}",
		"/api-keys/{keyID}/rotate",
		"/webhooks",
		"/webhooks/stats",
		"/webhooks/{webhookID}",
		"/webhooks/{webhookID}/deliveries",
		"/webhooks/replay",
		"/billing/invoices",
		"/billing/invoices/{invoiceID}",
		"/billing/checkout",
		"/billing/subscription",
		"/billing/portal",
		"/admin/stats",
		"/admin/users",
		"/admin/users/{userID}/role",
		"/admin/users/{userID}",
		"/feature-flags",
		"/feature-flags/check",
		"/audit/logs",
		"/audit/retention",
		"/audit/cleanup",
		"/hitl/pending",
		"/hitl/decide",
		"/hitl/status",
		"/providers",
		"/providers/{providerID}/models",
		"/models/{modelID}",
		"/providers/health",
		"/providers/cost-override",
		"/export/conversations",
		"/export/skills",
		"/import",
		"/ratelimit/dashboard",
		"/deep-analyze",
		"/batch",
		"/metrics",
		"/ws",
		"/users/me/sessions",
		"/users/me/sessions/active",
		"/sessions/{sessionID}/invalidate",
		"/invitations/{token}/accept",
		"/organizations/{orgID}/invitations",
		"/organizations/{orgID}/invitations/{invitationID}",
	}

	for _, p := range paths {
		_, ok := spec.Paths[p]
		assert.True(t, ok, "missing path: %s", p)
	}
}

func TestGenerateOpenAPI_AuthSchemes(t *testing.T) {
	spec := GenerateOpenAPI()

	assert.Contains(t, spec.Components.SecuritySchemes, "BearerAuth")
	assert.Contains(t, spec.Components.SecuritySchemes, "ApiKeyAuth")

	bearer := spec.Components.SecuritySchemes["BearerAuth"]
	assert.Equal(t, "http", bearer.Type)
	assert.Equal(t, "bearer", bearer.Scheme)
	assert.Equal(t, "JWT", bearer.BearerFmt)

	apikey := spec.Components.SecuritySchemes["ApiKeyAuth"]
	assert.Equal(t, "apiKey", apikey.Type)
	assert.Equal(t, "header", apikey.In)
	assert.Equal(t, "X-API-Key", apikey.Name)
}

func TestGenerateOpenAPI_PathParameters(t *testing.T) {
	spec := GenerateOpenAPI()

	expectedParams := []string{
		"OrgID", "ProjectID", "AgentID", "SessionID",
		"TaskID", "SkillID", "AlertID", "KeyID",
		"WebhookID", "UserID",
	}
	for _, name := range expectedParams {
		_, ok := spec.Components.Parameters[name]
		assert.True(t, ok, "missing parameter: %s", name)
	}
}

func TestGenerateOpenAPI_ErrorResponses(t *testing.T) {
	spec := GenerateOpenAPI()

	expectedResponses := []string{
		"BadRequest", "Unauthorized", "Forbidden", "NotFound",
	}
	for _, name := range expectedResponses {
		_, ok := spec.Components.Responses[name]
		assert.True(t, ok, "missing response: %s", name)
	}
}

func TestGenerateOpenAPI_SchemasPresent(t *testing.T) {
	spec := GenerateOpenAPI()

	schemas := []string{
		"ErrorResponse", "MessageResponse", "HealthResponse",
		"ReadinessResponse", "RegisterResponse", "LoginResponse",
		"User", "ChangePasswordRequest", "Organization",
		"Project", "Agent", "Session", "EventInput", "Event",
		"CreateTaskRequest", "Task", "Skill", "SkillRating",
		"Alert", "APIKey", "APIKeyCreated", "Webhook",
		"WebhookStats", "WebhookDelivery", "Invoice",
		"CostSummary", "TokenSummary", "ScanResult",
		"ReviewResult", "Finding", "MemoryResult",
		"FeatureFlag", "AuditLog", "HITLCheckpoint",
		"Provider", "Model", "Subscription",
		"Invitation", "SessionStats",
	}
	for _, name := range schemas {
		_, ok := spec.Components.Schemas[name]
		assert.True(t, ok, "missing schema: %s", name)
	}
}

func TestGenerateOpenAPI_PublicRoutesNoAuth(t *testing.T) {
	spec := GenerateOpenAPI()

	publicPaths := []string{
		"/health",
		"/ready",
		"/auth/register",
		"/auth/login",
		"/auth/forgot-password",
		"/auth/reset-password",
		"/auth/verify-email",
		"/providers",
	}

	for _, p := range publicPaths {
		item, ok := spec.Paths[p]
		require.True(t, ok, "missing path: %s", p)
		var op *Operation
		if item.Get != nil {
			op = item.Get
		} else if item.Post != nil {
			op = item.Post
		}
		require.NotNil(t, op, "no operation for %s", p)
		assert.Nil(t, op.Security, "public route %s should have no security", p)
	}
}

func TestGenerateOpenAPI_ProtectedRoutesHaveAuth(t *testing.T) {
	spec := GenerateOpenAPI()

	protectedPaths := map[string]string{
		"/users/me":           "get",
		"/organizations":      "post",
		"/projects":           "post",
		"/tasks":              "post",
		"/skills":             "get",
		"/alerts":             "get",
		"/analytics/cost":     "get",
		"/admin/stats":        "get",
		"/api-keys":           "get",
		"/webhooks":           "get",
		"/billing/invoices":   "get",
		"/memory/search":      "post",
		"/scan":               "post",
		"/middleware/process": "post",
	}

	for p, method := range protectedPaths {
		item, ok := spec.Paths[p]
		require.True(t, ok, "missing path: %s", p)
		var op *Operation
		switch method {
		case "get":
			op = item.Get
		case "post":
			op = item.Post
		}
		require.NotNil(t, op, "no %s operation for %s", method, p)
		assert.NotNil(t, op.Security, "protected route %s %s should have security", method, p)
	}
}

func TestGenerateOpenAPI_Tags(t *testing.T) {
	spec := GenerateOpenAPI()
	assert.NotEmpty(t, spec.Tags)

	tagNames := make(map[string]bool)
	for _, tag := range spec.Tags {
		tagNames[tag.Name] = true
	}
	for _, name := range []string{"Infrastructure", "Auth", "Users", "Organizations", "Projects", "Agents", "Sessions", "Events", "Tasks", "Memory", "Skills", "Alerts", "Analytics", "Scan", "API Keys", "Webhooks", "Billing", "Admin"} {
		assert.True(t, tagNames[name], "missing tag: %s", name)
	}
}

func TestGenerateOpenAPI_MarshalYAML(t *testing.T) {
	spec := GenerateOpenAPI()
	data, err := spec.MarshalYAML()
	require.NoError(t, err)
	assert.Contains(t, string(data), "openapi: 3.0.3")
	assert.Contains(t, string(data), "VigilAgent API")
}

func TestGenerateOpenAPI_MarshalJSON(t *testing.T) {
	spec := GenerateOpenAPI()
	data, err := spec.MarshalJSONBytes()
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "3.0.3", parsed["openapi"])
}

func TestGenerateOpenAPI_OperationsHaveResponses(t *testing.T) {
	spec := GenerateOpenAPI()

	for path, item := range spec.Paths {
		ops := map[string]*Operation{
			"GET":    item.Get,
			"POST":   item.Post,
			"PUT":    item.Put,
			"DELETE": item.Delete,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			assert.NotEmpty(t, op.Responses, "%s %s missing responses", method, path)
		}
	}
}

func TestGenerateOpenAPI_OperationsHaveTags(t *testing.T) {
	spec := GenerateOpenAPI()

	for path, item := range spec.Paths {
		ops := map[string]*Operation{
			"GET":    item.Get,
			"POST":   item.Post,
			"PUT":    item.Put,
			"DELETE": item.Delete,
		}
		for method, op := range ops {
			if op == nil {
				continue
			}
			assert.NotEmpty(t, op.Tags, "%s %s missing tags", method, path)
		}
	}
}

func TestOpenAPIGeneratedHandler(t *testing.T) {
	r := openapiTestRouter()
	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiGeneratedHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/yaml")

	body := w.Body.String()
	assert.Contains(t, body, "openapi: 3.0.3")
	assert.Contains(t, body, "VigilAgent API")
	assert.Contains(t, body, "/auth/login")
	assert.Contains(t, body, "BearerAuth")
}

func TestOpenAPIGeneratedHandler_YAMLIsValid(t *testing.T) {
	r := openapiTestRouter()
	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiGeneratedHandler(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var parsed map[string]interface{}
	require.NoError(t, yaml.Unmarshal(w.Body.Bytes(), &parsed))
	assert.Equal(t, "3.0.3", parsed["openapi"])
}

func TestGenerateOpenAPI_ScopesDocumented(t *testing.T) {
	spec := GenerateOpenAPI()

	// Check that operations requiring specific scopes are present
	protected := spec.Paths["/organizations"]
	require.NotNil(t, protected)
	require.NotNil(t, protected.Post)
	assert.NotNil(t, protected.Post.Security, "POST /organizations requires auth")

	tasks := spec.Paths["/tasks"]
	require.NotNil(t, tasks)
	require.NotNil(t, tasks.Post)
	assert.NotNil(t, tasks.Post.Security, "POST /tasks requires auth")
}

func TestGenerateOpenAPI_PathParametersHaveRequiredFlag(t *testing.T) {
	spec := GenerateOpenAPI()

	for name, param := range spec.Components.Parameters {
		assert.True(t, param.Required, "parameter %s should be required", name)
		assert.Equal(t, "path", param.In, "parameter %s should be in path", name)
	}
}

func TestGenerateOpenAPI_RequestBodySchemas(t *testing.T) {
	spec := GenerateOpenAPI()

	// Register should have requestBody with required fields
	register := spec.Paths["/auth/register"]
	require.NotNil(t, register)
	require.NotNil(t, register.Post)
	require.NotNil(t, register.Post.RequestBody)
	assert.True(t, register.Post.RequestBody.Required)

	body := register.Post.RequestBody.Content["application/json"]
	require.NotNil(t, body)
	assert.Contains(t, body.Schema.Required, "email")
	assert.Contains(t, body.Schema.Required, "password")
	assert.Contains(t, body.Schema.Required, "name")
}

func TestGenerateOpenAPI_SSEStreamResponse(t *testing.T) {
	spec := GenerateOpenAPI()

	stream := spec.Paths["/tasks/{taskID}/stream"]
	require.NotNil(t, stream)
	require.NotNil(t, stream.Get)

	resp, ok := stream.Get.Responses["200"]
	require.True(t, ok)
	_, ok = resp.Content["text/event-stream"]
	assert.True(t, ok, "SSE endpoint should have text/event-stream content type")
}

func TestGenerateOpenAPI_WebSocketEndpoint(t *testing.T) {
	spec := GenerateOpenAPI()

	ws := spec.Paths["/ws"]
	require.NotNil(t, ws)
	require.NotNil(t, ws.Get)
	assert.Contains(t, ws.Get.Summary, "WebSocket")
}

func TestGenerateOpenAPI_DeleteReturnsNoContent(t *testing.T) {
	spec := GenerateOpenAPI()

	deletePaths := []struct {
		path   string
		method string
	}{
		{"/organizations/{orgID}", "delete"},
		{"/projects/{projectID}", "delete"},
		{"/agents/{agentID}", "delete"},
		{"/skills/{skillID}", "delete"},
		{"/alerts/{alertID}", "delete"},
		{"/webhooks/{webhookID}", "delete"},
		{"/admin/users/{userID}", "delete"},
	}

	for _, dp := range deletePaths {
		item, ok := spec.Paths[dp.path]
		require.True(t, ok, "missing path: %s", dp.path)
		require.NotNil(t, item.Delete, "missing DELETE for %s", dp.path)
		resp, ok := item.Delete.Responses["204"]
		assert.True(t, ok, "DELETE %s should return 204", dp.path)
		assert.Equal(t, "Deleted", resp.Description)
	}
}

// ── Swagger UI & Embedded OpenAPI Spec ────────────────────

func swaggerTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestSwaggerUIHandler(t *testing.T) {
	r := swaggerTestRouter()
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	r.swaggerUIHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	body := w.Body.String()
	assert.Contains(t, body, "SwaggerUIBundle")
	assert.Contains(t, body, "VigilAgent API Documentation")
	assert.Contains(t, body, "/api/v1/docs/openapi.yaml")
}

func TestOpenAPISpecHandler(t *testing.T) {
	r := swaggerTestRouter()
	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiSpecHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/yaml")
}
