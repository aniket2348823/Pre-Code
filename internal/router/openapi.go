package router

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"gopkg.in/yaml.v3"
)

// --- OpenAPI 3.0 Go Structs ---

type OpenAPISpec struct {
	OpenAPI    string               `json:"openapi" yaml:"openapi"`
	Info       Info                 `json:"info" yaml:"info"`
	Servers    []Server             `json:"servers,omitempty" yaml:"servers,omitempty"`
	Paths      map[string]*PathItem `json:"paths" yaml:"paths"`
	Components Components           `json:"components" yaml:"components"`
	Tags       []Tag                `json:"tags,omitempty" yaml:"tags,omitempty"`
}

type Info struct {
	Title       string `json:"title" yaml:"title"`
	Description string `json:"description" yaml:"description"`
	Version     string `json:"version" yaml:"version"`
}

type Server struct {
	URL         string `json:"url" yaml:"url"`
	Description string `json:"description" yaml:"description"`
}

type Tag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
}

type PathItem struct {
	Get    *Operation `json:"get,omitempty" yaml:"get,omitempty"`
	Post   *Operation `json:"post,omitempty" yaml:"post,omitempty"`
	Put    *Operation `json:"put,omitempty" yaml:"put,omitempty"`
	Patch  *Operation `json:"patch,omitempty" yaml:"patch,omitempty"`
	Delete *Operation `json:"delete,omitempty" yaml:"delete,omitempty"`
}

type Operation struct {
	Summary     string                `json:"summary" yaml:"summary"`
	Description string                `json:"description,omitempty" yaml:"description,omitempty"`
	Tags        []string              `json:"tags,omitempty" yaml:"tags,omitempty"`
	OperationID string                `json:"operationId,omitempty" yaml:"operationId,omitempty"`
	Parameters  []Parameter           `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	RequestBody *RequestBody          `json:"requestBody,omitempty" yaml:"requestBody,omitempty"`
	Responses   map[string]*Response  `json:"responses" yaml:"responses"`
	Security    []map[string][]string `json:"security,omitempty" yaml:"security,omitempty"`
}

type Parameter struct {
	Name     string `json:"name" yaml:"name"`
	In       string `json:"in" yaml:"in"`
	Required bool   `json:"required" yaml:"required"`
	Schema   Schema `json:"schema" yaml:"schema"`
	Ref      string `json:"$ref,omitempty" yaml:"$ref,omitempty"`
}

type RequestBody struct {
	Required bool                  `json:"required" yaml:"required"`
	Content  map[string]*MediaType `json:"content" yaml:"content"`
}

type Response struct {
	Description string                `json:"description" yaml:"description"`
	Content     map[string]*MediaType `json:"content,omitempty" yaml:"content,omitempty"`
	Ref         string                `json:"$ref,omitempty" yaml:"$ref,omitempty"`
}

type MediaType struct {
	Schema Schema `json:"schema" yaml:"schema"`
}

type Schema struct {
	Type        string            `json:"type,omitempty" yaml:"type,omitempty"`
	Format      string            `json:"format,omitempty" yaml:"format,omitempty"`
	Properties  map[string]Schema `json:"properties,omitempty" yaml:"properties,omitempty"`
	Items       *Schema           `json:"items,omitempty" yaml:"items,omitempty"`
	Required    []string          `json:"required,omitempty" yaml:"required,omitempty"`
	Enum        []interface{}     `json:"enum,omitempty" yaml:"enum,omitempty"`
	Ref         string            `json:"$ref,omitempty" yaml:"$ref,omitempty"`
	Minimum     *int              `json:"minimum,omitempty" yaml:"minimum,omitempty"`
	Maximum     *int              `json:"maximum,omitempty" yaml:"maximum,omitempty"`
	MinLength   *int              `json:"minLength,omitempty" yaml:"minLength,omitempty"`
	Additional  *Schema           `json:"additionalProperties,omitempty" yaml:"additionalProperties,omitempty"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
}

type Components struct {
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes" yaml:"securitySchemes"`
	Parameters      map[string]*Parameter      `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Responses       map[string]*Response       `json:"responses,omitempty" yaml:"responses,omitempty"`
	Schemas         map[string]Schema          `json:"schemas" yaml:"schemas"`
}

type SecurityScheme struct {
	Type      string `json:"type" yaml:"type"`
	Scheme    string `json:"scheme,omitempty" yaml:"scheme,omitempty"`
	BearerFmt string `json:"bearerFormat,omitempty" yaml:"bearerFormat,omitempty"`
	In        string `json:"in,omitempty" yaml:"in,omitempty"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
}

// --- Schema Helpers ---

func schemaRef(name string) Schema {
	return Schema{Ref: "#/components/schemas/" + name}
}

func paramRef(name string) Parameter {
	return Parameter{Ref: "#/components/parameters/" + name}
}

func responseRef(name string) *Response {
	return &Response{Ref: "#/components/responses/" + name}
}

func strSchema(t string) Schema {
	return Schema{Type: t}
}

func intSchema() Schema {
	return Schema{Type: "integer"}
}

func numSchema() Schema {
	return Schema{Type: "number"}
}

func boolSchema() Schema {
	return Schema{Type: "boolean"}
}

func objSchema(props map[string]Schema) Schema {
	return Schema{Type: "object", Properties: props}
}

func objSchemaRequired(props map[string]Schema, required ...string) Schema {
	return Schema{Type: "object", Properties: props, Required: required}
}

func arraySchema(items Schema) Schema {
	return Schema{Type: "array", Items: &items}
}

func stringWithFmt(f string) Schema {
	return Schema{Type: "string", Format: f}
}

func stringWithMin(min int) Schema {
	return Schema{Type: "string", MinLength: &min}
}

func intWithRange(min, max int) Schema {
	return Schema{Type: "integer", Minimum: &min, Maximum: &max}
}

func additionalStringSchema() Schema {
	return Schema{Type: "object", Additional: &Schema{Type: "string"}}
}

// --- Security helpers ---

var jwtSecurity = []map[string][]string{{"BearerAuth": {}}}
var apiKeySecurity = []map[string][]string{{"ApiKeyAuth": {}}}
var anyAuthSecurity = []map[string][]string{{"BearerAuth": {}}, {"ApiKeyAuth": {}}}

// --- Shared error responses ---

func errorResponse(desc string, schemaName string) *Response {
	return &Response{
		Description: desc,
		Content: map[string]*MediaType{
			"application/json": {Schema: Schema{Ref: "#/components/schemas/" + schemaName}},
		},
	}
}

// --- Operation builder methods ---

func op(summary, tag string) *Operation {
	return &Operation{
		Summary: summary,
		Tags:    []string{tag},
	}
}

func opAuth(summary, tag string) *Operation {
	o := op(summary, tag)
	o.Security = anyAuthSecurity
	return o
}

func opJWT(summary, tag string) *Operation {
	o := op(summary, tag)
	o.Security = jwtSecurity
	return o
}

func (o *Operation) withResp(code string, resp *Response) *Operation {
	if o.Responses == nil {
		o.Responses = make(map[string]*Response)
	}
	o.Responses[code] = resp
	return o
}

func (o *Operation) withReqBody(required bool, schema Schema) *Operation {
	o.RequestBody = &RequestBody{
		Required: required,
		Content: map[string]*MediaType{
			"application/json": {Schema: schema},
		},
	}
	return o
}

func (o *Operation) withParam(params ...Parameter) *Operation {
	o.Parameters = append(o.Parameters, params...)
	return o
}

func (o *Operation) withID(id string) *Operation {
	o.OperationID = id
	return o
}

func mediaJSON(schema Schema) map[string]*MediaType {
	return map[string]*MediaType{"application/json": {Schema: schema}}
}

// --- GenerateOpenAPI returns the full spec ---

func GenerateOpenAPI() *OpenAPISpec {
	s := &OpenAPISpec{
		OpenAPI: "3.0.3",
		Info: Info{
			Title:       "VigilAgent API",
			Description: "Centralized API for VigilAgent orchestrator, agent runtime, safety, analytics, and security posture scanning.",
			Version:     "1.0.0",
		},
		Servers: []Server{{URL: "/api/v1", Description: "Local API server"}},
		Tags: []Tag{
			{Name: "Infrastructure", Description: "Health, readiness, metrics"},
			{Name: "Auth", Description: "Registration, login, password reset, email verification"},
			{Name: "Users", Description: "User profile management"},
			{Name: "Organizations", Description: "Organization CRUD and membership"},
			{Name: "Projects", Description: "Project CRUD"},
			{Name: "Agents", Description: "Agent CRUD"},
			{Name: "Sessions", Description: "Agent session management"},
			{Name: "Events", Description: "Session event ingestion"},
			{Name: "Tasks", Description: "Task creation, execution, HITL"},
			{Name: "Memory", Description: "Semantic memory search and creation"},
			{Name: "Skills", Description: "Skill marketplace CRUD and ratings"},
			{Name: "Alerts", Description: "Alert rules CRUD"},
			{Name: "Analytics", Description: "Cost, token, session analytics and dashboards"},
			{Name: "Scan", Description: "Code scanning, review, validation pipeline"},
			{Name: "Middleware", Description: "Middleware processing and metrics"},
			{Name: "API Keys", Description: "API key management"},
			{Name: "Webhooks", Description: "Webhook CRUD, deliveries, replay"},
			{Name: "Billing", Description: "Invoices, subscriptions, checkout"},
			{Name: "Admin", Description: "Admin-only platform management"},
			{Name: "Realtime", Description: "WebSocket and SSE streaming"},
			{Name: "Feature Flags", Description: "Feature flag management"},
			{Name: "Export", Description: "Data export and import"},
		},
		Paths:      make(map[string]*PathItem),
		Components: buildComponents(),
	}

	// =========================================================================
	// Public routes (no auth)
	// =========================================================================
	s.addPath("/health", &PathItem{
		Get: op("Health check", "Infrastructure").withID("healthCheck").
			withResp("200", &Response{Description: "Service is healthy", Content: mediaJSON(schemaRef("HealthResponse"))}),
	})
	s.addPath("/ready", &PathItem{
		Get: op("Readiness check", "Infrastructure").withID("readinessCheck").
			withResp("200", &Response{Description: "All dependencies healthy", Content: mediaJSON(schemaRef("ReadinessResponse"))}).
			withResp("503", &Response{Description: "One or more dependencies unhealthy", Content: mediaJSON(schemaRef("ReadinessResponse"))}),
	})
	s.addPath("/docs", &PathItem{
		Get: op("Swagger UI", "Infrastructure").withID("swaggerUI").
			withResp("200", &Response{Description: "Swagger UI HTML page"}),
	})
	s.addPath("/docs/openapi.yaml", &PathItem{
		Get: op("OpenAPI spec (generated)", "Infrastructure").withID("openapiSpec").
			withResp("200", &Response{Description: "OpenAPI 3.0 YAML spec"}),
	})

	// =========================================================================
	// Auth (public)
	// =========================================================================
	s.addPath("/auth/register", &PathItem{
		Post: op("Register a new user", "Auth").withID("registerUser").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"email":    {Type: "string", Format: "email"},
				"password": stringWithMin(12),
				"name":     strSchema("string"),
			}, "email", "password", "name")).
			withResp("201", &Response{Description: "User registered", Content: mediaJSON(schemaRef("RegisterResponse"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("409", errorResponse("Email already registered", "ErrorResponse")),
	})
	s.addPath("/auth/login", &PathItem{
		Post: op("Authenticate and log in", "Auth").withID("loginUser").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"email":    strSchema("string"),
				"password": strSchema("string"),
			}, "email", "password")).
			withResp("200", &Response{Description: "Login successful", Content: mediaJSON(schemaRef("LoginResponse"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("401", responseRef("Unauthorized")).
			withResp("423", errorResponse("Account locked", "ErrorResponse")),
	})
	s.addPath("/auth/forgot-password", &PathItem{
		Post: op("Request password reset email", "Auth").withID("forgotPassword").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"email": strSchema("string"),
			}, "email")).
			withResp("200", &Response{Description: "Reset email sent", Content: mediaJSON(schemaRef("MessageResponse"))}).
			withResp("400", responseRef("BadRequest")),
	})
	s.addPath("/auth/reset-password", &PathItem{
		Post: op("Reset password with token", "Auth").withID("resetPassword").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"token":        strSchema("string"),
				"new_password": stringWithMin(12),
			}, "token", "new_password")).
			withResp("200", &Response{Description: "Password has been reset", Content: mediaJSON(schemaRef("MessageResponse"))}).
			withResp("400", responseRef("BadRequest")),
	})
	s.addPath("/auth/verify-email", &PathItem{
		Get: op("Verify email address", "Auth").withID("verifyEmail").
			withParam(Parameter{Name: "token", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Email verified", Content: mediaJSON(schemaRef("MessageResponse"))}).
			withResp("400", responseRef("BadRequest")),
	})

	// =========================================================================
	// Protected: Auth
	// =========================================================================
	s.addPath("/auth/logout", &PathItem{
		Post: opAuth("Log out and revoke current JWT", "Auth").withID("logoutUser").
			withResp("200", &Response{Description: "Logged out", Content: mediaJSON(schemaRef("MessageResponse"))}).
			withResp("401", responseRef("Unauthorized")),
	})
	s.addPath("/auth/refresh", &PathItem{
		Post: opJWT("Refresh JWT token", "Auth").withID("refreshToken").
			withResp("200", &Response{Description: "New token issued", Content: mediaJSON(schemaRef("LoginResponse"))}).
			withResp("401", responseRef("Unauthorized")),
	})

	// =========================================================================
	// Users
	// =========================================================================
	s.addPath("/users/me", &PathItem{
		Get: opAuth("Get current user profile", "Users").withID("getCurrentUser").
			withResp("200", &Response{Description: "Current user", Content: mediaJSON(schemaRef("User"))}).
			withResp("401", responseRef("Unauthorized")),
		Put: opJWT("Update current user profile", "Users").withID("updateProfile").
			withReqBody(false, objSchema(map[string]Schema{
				"name":       strSchema("string"),
				"avatar_url": strSchema("string"),
			})).
			withResp("200", &Response{Description: "Profile updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/users/me/password", &PathItem{
		Put: opJWT("Change password (revokes all tokens)", "Users").withID("changePassword").
			withReqBody(true, schemaRef("ChangePasswordRequest")).
			withResp("200", &Response{Description: "Password changed", Content: mediaJSON(schemaRef("MessageResponse"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("401", responseRef("Unauthorized")),
	})
	s.addPath("/users/me/sessions", &PathItem{
		Get: opJWT("List user sessions", "Users").withID("listUserSessions").
			withResp("200", &Response{Description: "List of user sessions", Content: mediaJSON(arraySchema(schemaRef("Session")))}),
	})
	s.addPath("/users/me/sessions/active", &PathItem{
		Get: opJWT("List active user sessions", "Users").withID("listActiveSessions").
			withResp("200", &Response{Description: "Active sessions", Content: mediaJSON(arraySchema(schemaRef("Session")))}),
	})

	// =========================================================================
	// Organizations
	// =========================================================================
	s.addPath("/organizations", &PathItem{
		Post: opJWT("Create an organization", "Organizations").withID("createOrganization").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
			}, "name")).
			withResp("201", &Response{Description: "Organization created", Content: mediaJSON(schemaRef("Organization"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("401", responseRef("Unauthorized")),
		Get: opJWT("List organizations", "Organizations").withID("listOrganizations").
			withResp("200", &Response{Description: "List of organizations", Content: mediaJSON(arraySchema(schemaRef("Organization")))}).
			withResp("401", responseRef("Unauthorized")),
	})
	s.addPath("/organizations/{orgID}", &PathItem{
		Get: opJWT("Get organization", "Organizations").withID("getOrganization").
			withParam(paramRef("OrgID")).
			withResp("200", &Response{Description: "Organization details", Content: mediaJSON(schemaRef("Organization"))}).
			withResp("403", responseRef("Forbidden")),
		Put: opJWT("Update organization", "Organizations").withID("updateOrganization").
			withParam(paramRef("OrgID")).
			withReqBody(false, objSchema(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"plan":        strSchema("string"),
				"settings":    objSchema(nil),
			})).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete organization", "Organizations").withID("deleteOrganization").
			withParam(paramRef("OrgID")).
			withResp("204", &Response{Description: "Deleted"}),
	})
	s.addPath("/organizations/{orgID}/invitations", &PathItem{
		Post: opJWT("Invite member to organization", "Organizations").withID("inviteMember").
			withParam(paramRef("OrgID")).
			withReqBody(true, objSchema(map[string]Schema{
				"email": strSchema("string"),
				"role":  strSchema("string"),
			})).
			withResp("201", &Response{Description: "Invitation sent", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Get: opJWT("List organization invitations", "Organizations").withID("listInvitations").
			withParam(paramRef("OrgID")).
			withResp("200", &Response{Description: "List of invitations", Content: mediaJSON(arraySchema(schemaRef("Invitation")))}),
	})
	s.addPath("/organizations/{orgID}/invitations/{invitationID}", &PathItem{
		Delete: opJWT("Revoke invitation", "Organizations").withID("revokeInvitation").
			withParam(paramRef("OrgID")).
			withParam(paramRef("InvitationID")).
			withResp("204", &Response{Description: "Revoked"}),
	})
	s.addPath("/invitations/{token}/accept", &PathItem{
		Post: opJWT("Accept invitation", "Organizations").withID("acceptInvitation").
			withParam(Parameter{Name: "token", In: "path", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Invitation accepted", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Projects
	// =========================================================================
	s.addPath("/projects", &PathItem{
		Post: opJWT("Create a project", "Projects").withID("createProject").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"org_id":      strSchema("string"),
				"name":        strSchema("string"),
				"description": strSchema("string"),
			}, "org_id", "name")).
			withResp("201", &Response{Description: "Project created", Content: mediaJSON(schemaRef("Project"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("401", responseRef("Unauthorized")),
		Get: opJWT("List projects", "Projects").withID("listProjects").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "List of projects", Content: mediaJSON(arraySchema(schemaRef("Project")))}),
	})
	s.addPath("/projects/{projectID}", &PathItem{
		Get: opJWT("Get project", "Projects").withID("getProject").
			withParam(paramRef("ProjectID")).
			withResp("200", &Response{Description: "Project details", Content: mediaJSON(schemaRef("Project"))}).
			withResp("403", responseRef("Forbidden")),
		Put: opJWT("Update project", "Projects").withID("updateProject").
			withParam(paramRef("ProjectID")).
			withReqBody(false, objSchema(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"status":      strSchema("string"),
			})).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete project", "Projects").withID("deleteProject").
			withParam(paramRef("ProjectID")).
			withResp("204", &Response{Description: "Deleted"}),
	})

	// =========================================================================
	// Agents
	// =========================================================================
	s.addPath("/projects/{projectID}/agents", &PathItem{
		Post: opJWT("Create an agent", "Agents").withID("createAgent").
			withParam(paramRef("ProjectID")).
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"config":      objSchema(nil),
			}, "name")).
			withResp("201", &Response{Description: "Agent created", Content: mediaJSON(schemaRef("Agent"))}),
		Get: opJWT("List agents", "Agents").withID("listAgents").
			withParam(paramRef("ProjectID")).
			withResp("200", &Response{Description: "List of agents", Content: mediaJSON(arraySchema(schemaRef("Agent")))}),
	})
	s.addPath("/agents/{agentID}", &PathItem{
		Get: opJWT("Get agent", "Agents").withID("getAgent").
			withParam(paramRef("AgentID")).
			withResp("200", &Response{Description: "Agent details", Content: mediaJSON(schemaRef("Agent"))}).
			withResp("403", responseRef("Forbidden")),
		Put: opJWT("Update agent", "Agents").withID("updateAgent").
			withParam(paramRef("AgentID")).
			withReqBody(false, objSchema(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"status":      strSchema("string"),
				"config":      objSchema(nil),
			})).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete agent", "Agents").withID("deleteAgent").
			withParam(paramRef("AgentID")).
			withResp("204", &Response{Description: "Deleted"}),
	})

	// =========================================================================
	// Sessions
	// =========================================================================
	s.addPath("/agents/{agentID}/sessions", &PathItem{
		Post: opJWT("Create session", "Sessions").withID("createSession").
			withParam(paramRef("AgentID")).
			withResp("201", &Response{Description: "Session created", Content: mediaJSON(schemaRef("Session"))}),
		Get: opJWT("List sessions", "Sessions").withID("listSessions").
			withParam(paramRef("AgentID")).
			withResp("200", &Response{Description: "List of sessions", Content: mediaJSON(arraySchema(schemaRef("Session")))}),
	})
	s.addPath("/sessions/{sessionID}", &PathItem{
		Get: opJWT("Get session", "Sessions").withID("getSession").
			withParam(paramRef("SessionID")).
			withResp("200", &Response{Description: "Session details", Content: mediaJSON(schemaRef("Session"))}).
			withResp("403", responseRef("Forbidden")),
		Put: opJWT("Update session", "Sessions").withID("updateSession").
			withParam(paramRef("SessionID")).
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"status": strSchema("string"),
			}, "status")).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/sessions/{sessionID}/invalidate", &PathItem{
		Post: opJWT("Invalidate session", "Sessions").withID("invalidateSession").
			withParam(paramRef("SessionID")).
			withResp("200", &Response{Description: "Session invalidated", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Events
	// =========================================================================
	s.addPath("/sessions/{sessionID}/events", &PathItem{
		Post: opJWT("Create event", "Events").withID("createEvent").
			withParam(paramRef("SessionID")).
			withReqBody(true, schemaRef("EventInput")).
			withResp("201", &Response{Description: "Event created", Content: mediaJSON(schemaRef("Event"))}),
	})
	s.addPath("/sessions/{sessionID}/events/batch", &PathItem{
		Post: opJWT("Batch create events", "Events").withID("batchCreateEvents").
			withParam(paramRef("SessionID")).
			withReqBody(true, arraySchema(schemaRef("EventInput"))).
			withResp("201", &Response{Description: "Created", Content: mediaJSON(objSchema(map[string]Schema{"created": intSchema()}))}),
	})

	// =========================================================================
	// Tasks
	// =========================================================================
	s.addPath("/tasks", &PathItem{
		Post: opJWT("Create and run a task", "Tasks").withID("createTask").
			withReqBody(true, schemaRef("CreateTaskRequest")).
			withResp("201", &Response{Description: "Task created", Content: mediaJSON(schemaRef("Task"))}).
			withResp("400", responseRef("BadRequest")).
			withResp("401", responseRef("Unauthorized")),
		Get: opJWT("List tasks", "Tasks").withID("listTasks").
			withParam(Parameter{Name: "project_id", In: "query", Required: true, Schema: strSchema("string")}).
			withParam(Parameter{Name: "status", In: "query", Required: false, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "List of tasks", Content: mediaJSON(arraySchema(schemaRef("Task")))}),
	})
	s.addPath("/tasks/{taskID}", &PathItem{
		Get: opJWT("Get task", "Tasks").withID("getTask").
			withParam(paramRef("TaskID")).
			withResp("200", &Response{Description: "Task details", Content: mediaJSON(schemaRef("Task"))}),
	})
	s.addPath("/tasks/{taskID}/cancel", &PathItem{
		Post: opJWT("Cancel a task", "Tasks").withID("cancelTask").
			withParam(paramRef("TaskID")).
			withResp("200", &Response{Description: "Cancelled", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/tasks/{taskID}/stream", &PathItem{
		Get: opJWT("Stream task updates (SSE)", "Tasks").withID("streamTask").
			withParam(paramRef("TaskID")).
			withResp("200", &Response{Description: "SSE event stream", Content: map[string]*MediaType{"text/event-stream": {Schema: strSchema("string")}}}),
	})
	s.addPath("/tasks/{taskID}/hitl", &PathItem{
		Post: opJWT("Approve/reject HITL checkpoint", "Tasks").withID("approveHITL").
			withParam(paramRef("TaskID")).
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"approved": boolSchema(),
				"notes":    strSchema("string"),
			}, "approved")).
			withResp("200", &Response{Description: "Resolved", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/tasks/batch", &PathItem{
		Post: opJWT("Batch task operations", "Tasks").withID("batchTasks").
			withReqBody(true, objSchema(map[string]Schema{
				"operations": arraySchema(strSchema("string")),
			})).
			withResp("200", &Response{Description: "Batch result", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Memory
	// =========================================================================
	s.addPath("/memory/search", &PathItem{
		Post: opJWT("Search memory", "Memory").withID("searchMemory").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"query": strSchema("string"),
				"limit": intSchema(),
			}, "query")).
			withResp("200", &Response{Description: "Search results", Content: mediaJSON(arraySchema(schemaRef("MemoryResult")))}),
	})
	s.addPath("/memory", &PathItem{
		Post: opJWT("Create memory", "Memory").withID("createMemory").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"content": strSchema("string"),
			}, "content")).
			withResp("201", &Response{Description: "Memory created"}),
	})

	// =========================================================================
	// Skills
	// =========================================================================
	s.addPath("/skills", &PathItem{
		Get: opJWT("List skills", "Skills").withID("listSkills").
			withParam(Parameter{Name: "category", In: "query", Required: false, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "List of skills", Content: mediaJSON(arraySchema(schemaRef("Skill")))}),
		Post: opJWT("Create skill", "Skills").withID("createSkill").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"name":        strSchema("string"),
				"slug":        strSchema("string"),
				"category":    strSchema("string"),
				"description": strSchema("string"),
			}, "name", "slug", "category")).
			withResp("201", &Response{Description: "Skill created", Content: mediaJSON(schemaRef("Skill"))}),
	})
	s.addPath("/skills/{skillID}", &PathItem{
		Get: opJWT("Get skill", "Skills").withID("getSkill").
			withParam(paramRef("SkillID")).
			withResp("200", &Response{Description: "Skill details", Content: mediaJSON(schemaRef("Skill"))}),
		Put: opJWT("Update skill", "Skills").withID("updateSkill").
			withParam(paramRef("SkillID")).
			withReqBody(false, objSchema(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
			})).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete skill", "Skills").withID("deleteSkill").
			withParam(paramRef("SkillID")).
			withResp("204", &Response{Description: "Deleted"}),
	})
	s.addPath("/skills/{skillID}/rate", &PathItem{
		Post: opJWT("Rate skill", "Skills").withID("rateSkill").
			withParam(paramRef("SkillID")).
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"rating": intWithRange(1, 5),
				"review": strSchema("string"),
			}, "rating")).
			withResp("200", &Response{Description: "Rated", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/skills/{skillID}/ratings", &PathItem{
		Get: opJWT("List skill ratings", "Skills").withID("listSkillRatings").
			withParam(paramRef("SkillID")).
			withResp("200", &Response{Description: "List of ratings", Content: mediaJSON(arraySchema(schemaRef("SkillRating")))}),
	})
	s.addPath("/skills/{skillID}/install", &PathItem{
		Post: opJWT("Install skill", "Skills").withID("installSkill").
			withParam(paramRef("SkillID")).
			withResp("200", &Response{Description: "Installed", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Alerts
	// =========================================================================
	s.addPath("/alerts", &PathItem{
		Get: opJWT("List alerts", "Alerts").withID("listAlerts").
			withResp("200", &Response{Description: "List of alerts", Content: mediaJSON(arraySchema(schemaRef("Alert")))}),
		Post: opJWT("Create alert", "Alerts").withID("createAlert").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"type":      strSchema("string"),
				"condition": objSchema(nil),
				"channel":   strSchema("string"),
			}, "type", "condition", "channel")).
			withResp("201", &Response{Description: "Alert created", Content: mediaJSON(schemaRef("Alert"))}),
	})
	s.addPath("/alerts/{alertID}", &PathItem{
		Get: opJWT("Get alert", "Alerts").withID("getAlert").
			withParam(paramRef("AlertID")).
			withResp("200", &Response{Description: "Alert details", Content: mediaJSON(schemaRef("Alert"))}),
		Put: opJWT("Update alert", "Alerts").withID("updateAlert").
			withParam(paramRef("AlertID")).
			withReqBody(false, objSchema(nil)).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete alert", "Alerts").withID("deleteAlert").
			withParam(paramRef("AlertID")).
			withResp("204", &Response{Description: "Deleted"}),
	})

	// =========================================================================
	// Analytics
	// =========================================================================
	s.addPath("/analytics/cost", &PathItem{
		Get: opJWT("Cost analytics", "Analytics").withID("costAnalytics").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Cost analytics", Content: mediaJSON(schemaRef("CostSummary"))}),
	})
	s.addPath("/analytics/tokens", &PathItem{
		Get: opJWT("Token analytics", "Analytics").withID("tokenAnalytics").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Token analytics", Content: mediaJSON(schemaRef("TokenSummary"))}),
	})
	s.addPath("/analytics/sessions", &PathItem{
		Get: opJWT("Session analytics", "Analytics").withID("sessionAnalytics").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Session analytics", Content: mediaJSON(schemaRef("SessionStats"))}),
	})
	s.addPath("/analytics/cost-intel", &PathItem{
		Get: opJWT("Cost intelligence dashboard", "Analytics").withID("costIntelDashboard").
			withResp("200", &Response{Description: "Cost intelligence", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/analytics/cost-intel/forecast", &PathItem{
		Get: opJWT("Cost forecast", "Analytics").withID("costIntelForecast").
			withResp("200", &Response{Description: "Forecast", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/analytics/cost-intel/recommendations", &PathItem{
		Get: opJWT("Cost recommendations", "Analytics").withID("costIntelRecommendations").
			withResp("200", &Response{Description: "Recommendations", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/analytics/cost-intel/anomalies", &PathItem{
		Get: opJWT("Cost anomalies", "Analytics").withID("costIntelAnomalies").
			withResp("200", &Response{Description: "Anomalies", Content: mediaJSON(objSchema(nil))}),
	})

	// --- Dashboard ---
	s.addPath("/dashboard/overview", &PathItem{
		Get: opJWT("Dashboard overview", "Analytics").withID("dashboardOverview").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Overview", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/dashboard/activity", &PathItem{
		Get: opJWT("Recent activity", "Analytics").withID("dashboardActivity").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Activity", Content: mediaJSON(arraySchema(objSchema(nil)))}),
	})
	s.addPath("/dashboard/top-agents", &PathItem{
		Get: opJWT("Top agents", "Analytics").withID("dashboardTopAgents").
			withParam(Parameter{Name: "org_id", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Top agents", Content: mediaJSON(arraySchema(objSchema(nil)))}),
	})

	// =========================================================================
	// Scan
	// =========================================================================
	s.addPath("/scan", &PathItem{
		Post: opJWT("Run code scan", "Scan").withID("scanCode").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"code":     strSchema("string"),
				"filename": strSchema("string"),
			}, "code", "filename")).
			withResp("200", &Response{Description: "Scan results", Content: mediaJSON(schemaRef("ScanResult"))}),
	})
	s.addPath("/review", &PathItem{
		Post: opJWT("Full review pipeline", "Scan").withID("reviewCode").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"code":     strSchema("string"),
				"prompt":   strSchema("string"),
				"language": strSchema("string"),
				"filename": strSchema("string"),
			}, "code")).
			withResp("200", &Response{Description: "Review results", Content: mediaJSON(schemaRef("ReviewResult"))}),
	})
	s.addPath("/requirements", &PathItem{
		Post: opJWT("Extract requirements", "Scan").withID("extractRequirements").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Requirements", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/validate", &PathItem{
		Post: opJWT("Validate code", "Scan").withID("validateCode").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Validation results", Content: mediaJSON(schemaRef("ScanResult"))}),
	})
	s.addPath("/schema", &PathItem{
		Post: opJWT("Extract schema", "Scan").withID("extractSchema").
			withReqBody(true, objSchema(map[string]Schema{
				"code":     strSchema("string"),
				"language": strSchema("string"),
			})).
			withResp("200", &Response{Description: "Schema", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/compliance", &PathItem{
		Post: opJWT("Check compliance", "Scan").withID("checkCompliance").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Compliance results", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/validate-full", &PathItem{
		Post: opJWT("Full validation pipeline", "Scan").withID("validateFull").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Full validation results", Content: mediaJSON(schemaRef("ScanResult"))}),
	})
	s.addPath("/knowledge", &PathItem{
		Post: opJWT("Knowledge extraction", "Scan").withID("extractKnowledge").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Knowledge", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/skills/extract", &PathItem{
		Post: opJWT("Extract skills from code", "Scan").withID("extractSkills").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Extracted skills", Content: mediaJSON(arraySchema(schemaRef("Skill")))}),
	})
	s.addPath("/confidence", &PathItem{
		Post: opJWT("Confidence scoring", "Scan").withID("confidenceScore").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Confidence scores", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/attack-graph", &PathItem{
		Post: opJWT("Generate attack graph", "Scan").withID("attackGraph").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Attack graph", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/audit/trace", &PathItem{
		Post: opJWT("Audit trace", "Scan").withID("auditTrace").
			withReqBody(true, objSchema(map[string]Schema{"code": strSchema("string")})).
			withResp("200", &Response{Description: "Audit trace", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/deep-analyze", &PathItem{
		Post: opJWT("Deep analysis", "Scan").withID("deepAnalyze").
			withReqBody(true, objSchema(map[string]Schema{
				"code":     strSchema("string"),
				"language": strSchema("string"),
			})).
			withResp("200", &Response{Description: "Analysis results", Content: mediaJSON(objSchema(nil))}),
	})

	// =========================================================================
	// Middleware
	// =========================================================================
	s.addPath("/middleware/process", &PathItem{
		Post: opJWT("Process through middleware", "Middleware").withID("middlewareProcess").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"code":     strSchema("string"),
				"language": strSchema("string"),
			}, "code")).
			withResp("200", &Response{Description: "Results", Content: mediaJSON(schemaRef("ScanResult"))}),
	})
	s.addPath("/middleware/metrics", &PathItem{
		Get: opJWT("Middleware metrics", "Middleware").withID("middlewareMetrics").
			withResp("200", &Response{Description: "Metrics", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/middleware/patterns", &PathItem{
		Get: opJWT("Middleware patterns", "Middleware").withID("middlewarePatterns").
			withResp("200", &Response{Description: "Patterns", Content: mediaJSON(objSchema(nil))}),
	})

	// =========================================================================
	// API Keys
	// =========================================================================
	s.addPath("/api-keys", &PathItem{
		Get: opJWT("List API keys", "API Keys").withID("listAPIKeys").
			withResp("200", &Response{Description: "List of API keys", Content: mediaJSON(arraySchema(schemaRef("APIKey")))}),
		Post: opJWT("Create API key", "API Keys").withID("createAPIKey").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"name":   strSchema("string"),
				"scopes": arraySchema(strSchema("string")),
			}, "name")).
			withResp("201", &Response{Description: "Key created (shown once)", Content: mediaJSON(schemaRef("APIKeyCreated"))}),
	})
	s.addPath("/api-keys/{keyID}", &PathItem{
		Delete: opJWT("Revoke API key", "API Keys").withID("deleteAPIKey").
			withParam(paramRef("KeyID")).
			withResp("204", &Response{Description: "Revoked"}),
	})
	s.addPath("/api-keys/{keyID}/rotate", &PathItem{
		Post: opJWT("Rotate API key", "API Keys").withID("rotateAPIKey").
			withParam(paramRef("KeyID")).
			withResp("200", &Response{Description: "New key generated", Content: mediaJSON(schemaRef("APIKeyCreated"))}),
	})

	// =========================================================================
	// Webhooks
	// =========================================================================
	s.addPath("/webhooks", &PathItem{
		Get: opJWT("List webhooks", "Webhooks").withID("listWebhooks").
			withResp("200", &Response{Description: "List of webhooks", Content: mediaJSON(arraySchema(schemaRef("Webhook")))}),
		Post: opJWT("Create webhook", "Webhooks").withID("createWebhook").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"url":    stringWithFmt("uri"),
				"events": arraySchema(strSchema("string")),
			}, "url", "events")).
			withResp("201", &Response{Description: "Webhook created", Content: mediaJSON(schemaRef("Webhook"))}),
	})
	s.addPath("/webhooks/stats", &PathItem{
		Get: opJWT("Webhook statistics", "Webhooks").withID("webhookStats").
			withResp("200", &Response{Description: "Statistics", Content: mediaJSON(schemaRef("WebhookStats"))}),
	})
	s.addPath("/webhooks/{webhookID}", &PathItem{
		Get: opJWT("Get webhook", "Webhooks").withID("getWebhook").
			withParam(paramRef("WebhookID")).
			withResp("200", &Response{Description: "Details", Content: mediaJSON(schemaRef("Webhook"))}),
		Delete: opJWT("Delete webhook", "Webhooks").withID("deleteWebhook").
			withParam(paramRef("WebhookID")).
			withResp("204", &Response{Description: "Deleted"}),
	})
	s.addPath("/webhooks/{webhookID}/deliveries", &PathItem{
		Get: opJWT("List deliveries", "Webhooks").withID("listWebhookDeliveries").
			withParam(paramRef("WebhookID")).
			withResp("200", &Response{Description: "List of deliveries", Content: mediaJSON(arraySchema(schemaRef("WebhookDelivery")))}),
	})
	s.addPath("/webhooks/replay", &PathItem{
		Post: opJWT("Replay webhook", "Webhooks").withID("replayWebhook").
			withReqBody(true, objSchema(map[string]Schema{
				"webhook_id": strSchema("string"),
				"event_id":   strSchema("string"),
			})).
			withResp("200", &Response{Description: "Replayed", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Billing
	// =========================================================================
	s.addPath("/billing/invoices", &PathItem{
		Get: opJWT("List invoices", "Billing").withID("listInvoices").
			withResp("200", &Response{Description: "List of invoices", Content: mediaJSON(arraySchema(schemaRef("Invoice")))}),
	})
	s.addPath("/billing/invoices/{invoiceID}", &PathItem{
		Get: opJWT("Get invoice", "Billing").withID("getInvoice").
			withParam(Parameter{Name: "invoiceID", In: "path", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Invoice details", Content: mediaJSON(schemaRef("Invoice"))}),
	})
	s.addPath("/billing/checkout", &PathItem{
		Post: opJWT("Create checkout session", "Billing").withID("createCheckout").
			withReqBody(true, objSchema(map[string]Schema{"plan": strSchema("string")})).
			withResp("201", &Response{Description: "Checkout created", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/billing/subscription", &PathItem{
		Get: opJWT("Get subscription", "Billing").withID("getSubscription").
			withResp("200", &Response{Description: "Subscription", Content: mediaJSON(schemaRef("Subscription"))}),
	})
	s.addPath("/billing/portal", &PathItem{
		Post: opJWT("Create billing portal", "Billing").withID("createBillingPortal").
			withResp("201", &Response{Description: "Portal URL", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Admin
	// =========================================================================
	s.addPath("/admin/stats", &PathItem{
		Get: opJWT("Platform statistics", "Admin").withID("adminStats").
			withResp("200", &Response{Description: "Statistics", Content: mediaJSON(objSchema(nil))}).
			withResp("403", responseRef("Forbidden")),
	})
	s.addPath("/admin/users", &PathItem{
		Get: opJWT("List all users", "Admin").withID("adminListUsers").
			withResp("200", &Response{Description: "List of users", Content: mediaJSON(arraySchema(schemaRef("User")))}),
	})
	s.addPath("/admin/users/{userID}/role", &PathItem{
		Put: opJWT("Update user role", "Admin").withID("adminUpdateUserRole").
			withParam(paramRef("UserID")).
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"role": Schema{Type: "string", Enum: []interface{}{"user", "admin"}},
			}, "role")).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/admin/users/{userID}", &PathItem{
		Delete: opJWT("Delete user", "Admin").withID("adminDeleteUser").
			withParam(paramRef("UserID")).
			withResp("204", &Response{Description: "Deleted"}),
	})

	// =========================================================================
	// Feature Flags
	// =========================================================================
	s.addPath("/feature-flags", &PathItem{
		Get: opJWT("List feature flags", "Feature Flags").withID("listFeatureFlags").
			withResp("200", &Response{Description: "List of feature flags", Content: mediaJSON(arraySchema(schemaRef("FeatureFlag")))}),
		Put: opJWT("Update feature flag", "Feature Flags").withID("updateFeatureFlag").
			withReqBody(true, objSchema(map[string]Schema{
				"name":    strSchema("string"),
				"enabled": boolSchema(),
			})).
			withResp("200", &Response{Description: "Updated", Content: mediaJSON(schemaRef("MessageResponse"))}),
		Delete: opJWT("Delete feature flag", "Feature Flags").withID("deleteFeatureFlag").
			withReqBody(true, objSchema(map[string]Schema{"name": strSchema("string")})).
			withResp("204", &Response{Description: "Deleted"}),
	})
	s.addPath("/feature-flags/check", &PathItem{
		Get: opJWT("Check feature flag", "Feature Flags").withID("checkFeatureFlag").
			withParam(Parameter{Name: "name", In: "query", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Flag status", Content: mediaJSON(objSchema(map[string]Schema{"enabled": boolSchema()}))}),
	})

	// =========================================================================
	// Audit
	// =========================================================================
	s.addPath("/audit/logs", &PathItem{
		Get: opJWT("List audit logs", "Admin").withID("listAuditLogs").
			withResp("200", &Response{Description: "List of audit logs", Content: mediaJSON(arraySchema(schemaRef("AuditLog")))}),
	})
	s.addPath("/audit/retention", &PathItem{
		Get: opJWT("Get audit retention", "Admin").withID("getAuditRetention").
			withResp("200", &Response{Description: "Retention policy", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/audit/cleanup", &PathItem{
		Post: opJWT("Cleanup audit logs", "Admin").withID("cleanupAuditLogs").
			withResp("200", &Response{Description: "Cleaned up", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// HITL
	// =========================================================================
	s.addPath("/hitl/pending", &PathItem{
		Get: opJWT("List HITL checkpoints", "Tasks").withID("listHITLCheckpoints").
			withResp("200", &Response{Description: "Pending checkpoints", Content: mediaJSON(arraySchema(schemaRef("HITLCheckpoint")))}),
	})
	s.addPath("/hitl/decide", &PathItem{
		Post: opJWT("Decide HITL checkpoint", "Tasks").withID("decideHITL").
			withReqBody(true, objSchemaRequired(map[string]Schema{
				"checkpoint_id": strSchema("string"),
				"approved":      boolSchema(),
				"notes":         strSchema("string"),
			}, "checkpoint_id", "approved")).
			withResp("200", &Response{Description: "Decided", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})
	s.addPath("/hitl/status", &PathItem{
		Get: opJWT("HITL status", "Tasks").withID("hitlStatus").
			withResp("200", &Response{Description: "Status", Content: mediaJSON(objSchema(nil))}),
	})

	// =========================================================================
	// Providers (public + protected)
	// =========================================================================
	s.addPath("/providers", &PathItem{
		Get: op("List providers", "Infrastructure").withID("listProviders").
			withResp("200", &Response{Description: "List of providers", Content: mediaJSON(arraySchema(schemaRef("Provider")))}),
	})
	s.addPath("/providers/{providerID}/models", &PathItem{
		Get: op("List provider models", "Infrastructure").withID("listProviderModels").
			withParam(Parameter{Name: "providerID", In: "path", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "List of models", Content: mediaJSON(arraySchema(schemaRef("Model")))}),
	})
	s.addPath("/models/{modelID}", &PathItem{
		Get: op("Get model", "Infrastructure").withID("getModel").
			withParam(Parameter{Name: "modelID", In: "path", Required: true, Schema: strSchema("string")}).
			withResp("200", &Response{Description: "Model details", Content: mediaJSON(schemaRef("Model"))}),
	})
	s.addPath("/providers/health", &PathItem{
		Get: opJWT("Provider health stats", "Analytics").withID("providerHealth").
			withResp("200", &Response{Description: "Health stats", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/providers/cost-override", &PathItem{
		Post: opJWT("Cost override (admin)", "Admin").withID("costOverride").
			withResp("200", &Response{Description: "Override set", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Export / Import
	// =========================================================================
	s.addPath("/export/conversations", &PathItem{
		Get: opJWT("Export conversations", "Export").withID("exportConversations").
			withResp("200", &Response{Description: "Exported conversations", Content: mediaJSON(objSchema(nil))}),
	})
	s.addPath("/export/skills", &PathItem{
		Get: opJWT("Export skills", "Export").withID("exportSkills").
			withResp("200", &Response{Description: "Exported skills", Content: mediaJSON(arraySchema(schemaRef("Skill")))}),
	})
	s.addPath("/import", &PathItem{
		Post: opJWT("Import data", "Export").withID("importData").
			withReqBody(true, objSchema(nil)).
			withResp("200", &Response{Description: "Imported", Content: mediaJSON(schemaRef("MessageResponse"))}),
	})

	// =========================================================================
	// Rate Limit
	// =========================================================================
	s.addPath("/ratelimit/dashboard", &PathItem{
		Get: opJWT("Rate limit dashboard", "Analytics").withID("rateLimitDashboard").
			withResp("200", &Response{Description: "Dashboard", Content: mediaJSON(objSchema(nil))}),
	})

	// =========================================================================
	// Batch
	// =========================================================================
	s.addPath("/batch", &PathItem{
		Post: opJWT("Batch API operations", "Tasks").withID("batchOperations").
			withReqBody(true, objSchema(nil)).
			withResp("200", &Response{Description: "Batch result", Content: mediaJSON(objSchema(nil))}),
	})

	// =========================================================================
	// Infrastructure (protected)
	// =========================================================================
	s.addPath("/metrics", &PathItem{
		Get: opJWT("Prometheus metrics", "Infrastructure").withID("metrics").
			withResp("200", &Response{Description: "Prometheus metrics format"}),
	})

	// =========================================================================
	// WebSocket
	// =========================================================================
	s.addPath("/ws", &PathItem{
		Get: opJWT("WebSocket connection", "Realtime").withID("websocket").
			withResp("101", &Response{Description: "WebSocket upgrade"}),
	})

	return s
}

func (s *OpenAPISpec) addPath(path string, item *PathItem) {
	s.Paths[path] = item
}

// --- YAML/JSON Marshaling ---

func (s *OpenAPISpec) MarshalYAML() ([]byte, error) {
	return yaml.Marshal(s)
}

func (s *OpenAPISpec) MarshalJSONBytes() ([]byte, error) {
	return json.Marshal(s)
}

// --- HTTP Handler ---

func (r *Router) openapiGeneratedHandler(w http.ResponseWriter, req *http.Request) {
	spec := GenerateOpenAPI()
	w.Header().Set("Content-Type", "application/yaml")
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(spec); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"failed to marshal openapi spec"}`))
		return
	}
}

// --- Components ---

func buildComponents() Components {
	return Components{
		SecuritySchemes: map[string]*SecurityScheme{
			"BearerAuth": {Type: "http", Scheme: "bearer", BearerFmt: "JWT"},
			"ApiKeyAuth": {Type: "apiKey", In: "header", Name: "X-API-Key"},
		},
		Parameters: map[string]*Parameter{
			"OrgID":        {Name: "orgID", In: "path", Required: true, Schema: strSchema("string")},
			"ProjectID":    {Name: "projectID", In: "path", Required: true, Schema: strSchema("string")},
			"AgentID":      {Name: "agentID", In: "path", Required: true, Schema: strSchema("string")},
			"SessionID":    {Name: "sessionID", In: "path", Required: true, Schema: strSchema("string")},
			"TaskID":       {Name: "taskID", In: "path", Required: true, Schema: strSchema("string")},
			"SkillID":      {Name: "skillID", In: "path", Required: true, Schema: strSchema("string")},
			"AlertID":      {Name: "alertID", In: "path", Required: true, Schema: strSchema("string")},
			"KeyID":        {Name: "keyID", In: "path", Required: true, Schema: strSchema("string")},
			"WebhookID":    {Name: "webhookID", In: "path", Required: true, Schema: strSchema("string")},
			"UserID":       {Name: "userID", In: "path", Required: true, Schema: strSchema("string")},
			"InvitationID": {Name: "invitationID", In: "path", Required: true, Schema: strSchema("string")},
		},
		Responses: map[string]*Response{
			"BadRequest":   errorResponse("Invalid request", "ErrorResponse"),
			"Unauthorized": errorResponse("Missing/invalid auth", "ErrorResponse"),
			"Forbidden":    errorResponse("Insufficient permissions", "ErrorResponse"),
			"NotFound":     errorResponse("Resource not found", "ErrorResponse"),
		},
		Schemas: map[string]Schema{
			"ErrorResponse": objSchema(map[string]Schema{
				"code":    strSchema("string"),
				"message": strSchema("string"),
			}),
			"MessageResponse": objSchema(map[string]Schema{
				"message": strSchema("string"),
			}),
			"HealthResponse": objSchema(map[string]Schema{
				"status": strSchema("string"),
			}),
			"ReadinessResponse": objSchema(map[string]Schema{
				"status": objSchema(map[string]Schema{"ready": boolSchema()}),
				"checks": additionalStringSchema(),
			}),
			"RegisterResponse": objSchema(map[string]Schema{
				"token":   strSchema("string"),
				"user_id": strSchema("string"),
			}),
			"LoginResponse": objSchema(map[string]Schema{
				"token": strSchema("string"),
			}),
			"User": objSchema(map[string]Schema{
				"id":             strSchema("string"),
				"email":          strSchema("string"),
				"name":           strSchema("string"),
				"avatar_url":     strSchema("string"),
				"role":           strSchema("string"),
				"is_active":      boolSchema(),
				"email_verified": boolSchema(),
				"last_login_at":  stringWithFmt("date-time"),
				"created_at":     stringWithFmt("date-time"),
				"updated_at":     stringWithFmt("date-time"),
			}),
			"ChangePasswordRequest": objSchemaRequired(map[string]Schema{
				"current_password": strSchema("string"),
				"new_password":     stringWithMin(12),
			}, "current_password", "new_password"),
			"Organization": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"name":        strSchema("string"),
				"slug":        strSchema("string"),
				"description": strSchema("string"),
				"owner_id":    strSchema("string"),
				"plan":        strSchema("string"),
				"settings":    objSchema(nil),
				"created_at":  stringWithFmt("date-time"),
				"updated_at":  stringWithFmt("date-time"),
			}),
			"Invitation": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"org_id":     strSchema("string"),
				"email":      strSchema("string"),
				"role":       strSchema("string"),
				"token":      strSchema("string"),
				"created_at": stringWithFmt("date-time"),
			}),
			"Project": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"org_id":      strSchema("string"),
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"status":      strSchema("string"),
				"created_at":  stringWithFmt("date-time"),
				"updated_at":  stringWithFmt("date-time"),
			}),
			"Agent": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"project_id":  strSchema("string"),
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"config":      objSchema(nil),
				"status":      strSchema("string"),
				"created_at":  stringWithFmt("date-time"),
				"updated_at":  stringWithFmt("date-time"),
			}),
			"Session": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"project_id": strSchema("string"),
				"agent_id":   strSchema("string"),
				"user_id":    strSchema("string"),
				"status":     strSchema("string"),
				"created_at": stringWithFmt("date-time"),
				"updated_at": stringWithFmt("date-time"),
			}),
			"EventInput": objSchemaRequired(map[string]Schema{
				"event_type":  strSchema("string"),
				"source":      strSchema("string"),
				"payload":     objSchema(nil),
				"tokens_used": intSchema(),
				"cost_usd":    numSchema(),
				"latency_ms":  intSchema(),
			}, "event_type"),
			"Event": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"session_id":  strSchema("string"),
				"event_type":  strSchema("string"),
				"source":      strSchema("string"),
				"payload":     objSchema(nil),
				"tokens_used": intSchema(),
				"cost_usd":    numSchema(),
				"latency_ms":  intSchema(),
				"created_at":  stringWithFmt("date-time"),
			}),
			"CreateTaskRequest": objSchemaRequired(map[string]Schema{
				"prompt":     strSchema("string"),
				"project_id": strSchema("string"),
				"model":      strSchema("string"),
				"agent_id":   strSchema("string"),
			}, "prompt", "project_id"),
			"Task": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"project_id": strSchema("string"),
				"user_id":    strSchema("string"),
				"prompt":     strSchema("string"),
				"status":     strSchema("string"),
				"model":      strSchema("string"),
				"agent_id":   strSchema("string"),
				"cost":       numSchema(),
				"created_at": stringWithFmt("date-time"),
				"updated_at": stringWithFmt("date-time"),
			}),
			"Skill": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"name":        strSchema("string"),
				"slug":        strSchema("string"),
				"category":    strSchema("string"),
				"description": strSchema("string"),
				"downloads":   intSchema(),
				"rating":      numSchema(),
				"created_at":  stringWithFmt("date-time"),
				"updated_at":  stringWithFmt("date-time"),
			}),
			"SkillRating": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"skill_id":   strSchema("string"),
				"user_id":    strSchema("string"),
				"rating":     intSchema(),
				"review":     strSchema("string"),
				"created_at": stringWithFmt("date-time"),
			}),
			"Alert": objSchema(map[string]Schema{
				"id":              strSchema("string"),
				"organization_id": strSchema("string"),
				"user_id":         strSchema("string"),
				"type":            strSchema("string"),
				"condition":       objSchema(nil),
				"channel":         strSchema("string"),
				"is_active":       boolSchema(),
				"created_at":      stringWithFmt("date-time"),
				"updated_at":      stringWithFmt("date-time"),
			}),
			"APIKey": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"name":       strSchema("string"),
				"prefix":     strSchema("string"),
				"scopes":     arraySchema(strSchema("string")),
				"is_active":  boolSchema(),
				"created_at": stringWithFmt("date-time"),
			}),
			"APIKeyCreated": objSchema(map[string]Schema{
				"id":   strSchema("string"),
				"key":  strSchema("string"),
				"name": strSchema("string"),
			}),
			"Webhook": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"url":        strSchema("string"),
				"events":     arraySchema(strSchema("string")),
				"secret":     strSchema("string"),
				"is_active":  boolSchema(),
				"created_at": stringWithFmt("date-time"),
				"updated_at": stringWithFmt("date-time"),
			}),
			"WebhookStats": objSchema(map[string]Schema{
				"total":      intSchema(),
				"successful": intSchema(),
				"failed":     intSchema(),
			}),
			"WebhookDelivery": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"webhook_id":  strSchema("string"),
				"event_type":  strSchema("string"),
				"status_code": intSchema(),
				"success":     boolSchema(),
				"error":       strSchema("string"),
				"created_at":  stringWithFmt("date-time"),
			}),
			"Invoice": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"user_id":    strSchema("string"),
				"amount_usd": numSchema(),
				"currency":   strSchema("string"),
				"status":     strSchema("string"),
				"created_at": stringWithFmt("date-time"),
			}),
			"Subscription": objSchema(map[string]Schema{
				"id":     strSchema("string"),
				"plan":   strSchema("string"),
				"status": strSchema("string"),
			}),
			"CostSummary": objSchema(map[string]Schema{
				"total_cost_usd": numSchema(),
				"by_model":       objSchema(nil),
			}),
			"TokenSummary": objSchema(map[string]Schema{
				"total_tokens": intSchema(),
				"by_model":     objSchema(nil),
			}),
			"SessionStats": objSchema(map[string]Schema{
				"total":  intSchema(),
				"active": intSchema(),
				"ended":  intSchema(),
			}),
			"ScanResult": objSchema(map[string]Schema{
				"findings": arraySchema(schemaRef("Finding")),
				"score":    numSchema(),
				"summary":  strSchema("string"),
			}),
			"ReviewResult": objSchema(map[string]Schema{
				"findings":   arraySchema(schemaRef("Finding")),
				"verdict":    strSchema("string"),
				"confidence": numSchema(),
				"fixed_code": strSchema("string"),
			}),
			"Finding": objSchema(map[string]Schema{
				"severity": strSchema("string"),
				"line":     intSchema(),
				"message":  strSchema("string"),
				"rule":     strSchema("string"),
			}),
			"MemoryResult": objSchema(map[string]Schema{
				"id":      strSchema("string"),
				"content": strSchema("string"),
				"score":   numSchema(),
			}),
			"FeatureFlag": objSchema(map[string]Schema{
				"name":        strSchema("string"),
				"description": strSchema("string"),
				"enabled":     boolSchema(),
			}),
			"AuditLog": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"user_id":    strSchema("string"),
				"action":     strSchema("string"),
				"resource":   strSchema("string"),
				"ip_address": strSchema("string"),
				"created_at": stringWithFmt("date-time"),
			}),
			"HITLCheckpoint": objSchema(map[string]Schema{
				"id":         strSchema("string"),
				"task_id":    strSchema("string"),
				"status":     strSchema("string"),
				"created_at": stringWithFmt("date-time"),
			}),
			"Provider": objSchema(map[string]Schema{
				"id":     strSchema("string"),
				"name":   strSchema("string"),
				"status": strSchema("string"),
			}),
			"Model": objSchema(map[string]Schema{
				"id":          strSchema("string"),
				"provider_id": strSchema("string"),
				"name":        strSchema("string"),
				"max_tokens":  intSchema(),
				"cost_input":  numSchema(),
				"cost_output": numSchema(),
			}),
		},
	}
}

// ── Swagger UI & Embedded OpenAPI Spec ────────────────────

//go:embed openapi.yaml
var openapiSpec []byte

func (r *Router) swaggerUIHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
  <title>VigilAgent API Documentation</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: '/api/v1/docs/openapi.yaml',
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIBundle.SwaggerUIStandalonePreset
        ],
        layout: "BaseLayout",
        deepLinking: true
      });
    };
  </script>
</body>
</html>`))
}

func (r *Router) openapiSpecHandler(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/yaml")
	w.Write(openapiSpec)
}
