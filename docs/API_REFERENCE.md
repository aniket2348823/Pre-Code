# API Reference

> Complete reference for all VigilAgent REST API endpoints.

**Base URL**: `http://localhost:8080/api/v1`  
**Authentication**: JWT Bearer token or API Key via `Authorization: Bearer <token>` or `X-API-Key: <key>` header.  
**Content-Type**: `application/json` (unless otherwise specified)  
**Interactive Docs**: Available at `/api/v1/docs` (Swagger UI) when the server is running.

---

## Table of Contents

- [Authentication](#authentication)
- [Organizations](#organizations)
- [Projects](#projects)
- [Agents](#agents)
- [Sessions](#sessions)
- [Tasks](#tasks)
- [Human-in-the-Loop (HITL)](#human-in-the-loop-hitl)
- [Memory](#memory)
- [Code Analysis & Scanning](#code-analysis--scanning)
- [Skills Marketplace](#skills-marketplace)
- [LLM Providers](#llm-providers)
- [Analytics & Cost Intelligence](#analytics--cost-intelligence)
- [Alerts](#alerts)
- [API Keys](#api-keys)
- [Webhooks](#webhooks)
- [Billing](#billing)
- [Admin](#admin)
- [System](#system)
- [Real-Time](#real-time)
- [Error Responses](#error-responses)
- [Rate Limiting](#rate-limiting)
- [Pagination](#pagination)

---

## Authentication

### Register
```
POST /auth/register
```
Create a new user account.

**Request Body:**
```json
{
  "email": "user@example.com",
  "password": "SecureP@ss123",
  "name": "John Doe"
}
```

**Response** `201 Created`:
```json
{
  "id": "uuid",
  "email": "user@example.com",
  "name": "John Doe",
  "created_at": "2026-01-01T00:00:00Z"
}
```

---

### Login
```
POST /auth/login
```
Authenticate and receive a JWT token.

**Request Body:**
```json
{
  "email": "user@example.com",
  "password": "SecureP@ss123"
}
```

**Response** `200 OK`:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "refresh_token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-01-02T00:00:00Z",
  "user": {
    "id": "uuid",
    "email": "user@example.com",
    "name": "John Doe",
    "role": "user"
  }
}
```

---

### Logout 🔒
```
POST /auth/logout
```
Invalidate the current JWT token.

**Response** `200 OK`:
```json
{
  "message": "logged out successfully"
}
```

---

### Refresh Token 🔒
```
POST /auth/refresh
```
Exchange a refresh token for a new access token.

**Request Body:**
```json
{
  "refresh_token": "eyJhbGciOiJIUzI1NiIs..."
}
```

**Response** `200 OK`:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIs...",
  "expires_at": "2026-01-02T00:00:00Z"
}
```

---

### Forgot Password
```
POST /auth/forgot-password
```
Initiate a password reset flow. Sends a reset link to the user's email.

**Request Body:**
```json
{
  "email": "user@example.com"
}
```

**Response** `200 OK`:
```json
{
  "message": "password reset email sent"
}
```

---

### Reset Password
```
POST /auth/reset-password
```
Complete the password reset using the token from the email.

**Request Body:**
```json
{
  "token": "reset-token-from-email",
  "new_password": "NewSecureP@ss456"
}
```

---

### Verify Email
```
GET /auth/verify-email?token=verification-token
```
Verify a user's email address using the verification token.

---

### Get Current User 🔒
```
GET /users/me
```

**Response** `200 OK`:
```json
{
  "id": "uuid",
  "email": "user@example.com",
  "name": "John Doe",
  "role": "user",
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

---

### Update Profile 🔒
```
PUT /users/me
```

**Request Body:**
```json
{
  "name": "Jane Doe"
}
```

---

### Change Password 🔒
```
PUT /users/me/password
```

**Request Body:**
```json
{
  "current_password": "OldP@ss123",
  "new_password": "NewP@ss456"
}
```

---

### List Active Sessions 🔒
```
GET /users/me/sessions
GET /users/me/sessions/active
```

---

### Invalidate Session 🔒
```
POST /sessions/{sessionID}/invalidate
```

---

### Session Check 🔒
```
GET /auth/session-check
```
Verify the current session is still valid.

---

## Organizations

### Create Organization 🔒
```
POST /organizations
```

**Request Body:**
```json
{
  "name": "Acme Corp",
  "description": "Enterprise development team"
}
```

**Response** `201 Created`:
```json
{
  "id": "uuid",
  "name": "Acme Corp",
  "description": "Enterprise development team",
  "owner_id": "uuid",
  "created_at": "2026-01-01T00:00:00Z"
}
```

---

### List Organizations 🔒
```
GET /organizations
```

---

### Get Organization 🔒
```
GET /organizations/{orgID}
```

---

### Update Organization 🔒
```
PUT /organizations/{orgID}
```

---

### Delete Organization 🔒
```
DELETE /organizations/{orgID}
```

---

### Invite Team Member 🔒
```
POST /organizations/{orgID}/invitations
```

**Request Body:**
```json
{
  "email": "teammate@example.com",
  "role": "member"
}
```

---

### List Invitations 🔒
```
GET /organizations/{orgID}/invitations
```

---

### Revoke Invitation 🔒
```
DELETE /organizations/{orgID}/invitations/{invitationID}
```

---

### Accept Invitation 🔒
```
POST /invitations/{token}/accept
```

---

## Projects

### Create Project 🔒
```
POST /projects
```

**Request Body:**
```json
{
  "name": "Backend API",
  "organization_id": "uuid",
  "description": "Main backend service"
}
```

---

### List Projects 🔒
```
GET /projects
```

---

### Get Project 🔒
```
GET /projects/{projectID}
```

---

### Update Project 🔒
```
PUT /projects/{projectID}
```

---

### Delete Project 🔒
```
DELETE /projects/{projectID}
```

---

## Agents

### Create Agent 🔒
```
POST /projects/{projectID}/agents
```

**Request Body:**
```json
{
  "name": "Code Reviewer",
  "description": "Automated code review agent",
  "model": "gpt-4o",
  "system_prompt": "You are an expert code reviewer...",
  "max_iterations": 10,
  "tools": ["file_read", "file_write", "shell_exec"]
}
```

---

### List Agents 🔒
```
GET /projects/{projectID}/agents
```

---

### Get Agent 🔒
```
GET /agents/{agentID}
```

---

### Update Agent 🔒
```
PUT /agents/{agentID}
```

---

### Delete Agent 🔒
```
DELETE /agents/{agentID}
```

---

## Sessions

### Create Session 🔒
```
POST /agents/{agentID}/sessions
```

---

### List Sessions 🔒
```
GET /agents/{agentID}/sessions
```

---

### Get Session 🔒
```
GET /sessions/{sessionID}
```

---

### Update Session 🔒
```
PUT /sessions/{sessionID}
```

---

### Add Event to Session 🔒
```
POST /sessions/{sessionID}/events
```

**Request Body:**
```json
{
  "type": "user_message",
  "content": "Review this function for security issues"
}
```

---

### Batch Add Events 🔒
```
POST /sessions/{sessionID}/events/batch
```

---

## Tasks

### Submit Task 🔒
```
POST /tasks
```

**Request Body:**
```json
{
  "agent_id": "uuid",
  "prompt": "Review the authentication module for vulnerabilities",
  "max_tokens": 4096,
  "max_iterations": 10,
  "priority": "high"
}
```

**Response** `201 Created`:
```json
{
  "id": "uuid",
  "agent_id": "uuid",
  "status": "pending",
  "prompt": "Review the authentication module...",
  "created_at": "2026-01-01T00:00:00Z"
}
```

---

### List Tasks 🔒
```
GET /tasks
```

---

### Get Task 🔒
```
GET /tasks/{taskID}
```

**Response** `200 OK`:
```json
{
  "id": "uuid",
  "status": "completed",
  "prompt": "...",
  "result": "...",
  "input_tokens": 2500,
  "output_tokens": 1200,
  "cost_usd": 0.045,
  "iterations": 3,
  "created_at": "2026-01-01T00:00:00Z",
  "completed_at": "2026-01-01T00:01:30Z"
}
```

---

### Cancel Task 🔒
```
POST /tasks/{taskID}/cancel
```

---

### Stream Task Progress 🔒
```
GET /tasks/{taskID}/stream
```
Server-Sent Events (SSE) stream. Returns real-time progress updates.

**Event Types:**
| Event    | Description                              |
|----------|------------------------------------------|
| `token`  | Streaming token output                   |
| `status` | State machine transition                 |
| `critique`| LLM critique engine feedback            |
| `done`   | Task completed                           |
| `error`  | Task failed                              |

**Example SSE Stream:**
```
event: status
data: {"state": "planning", "message": "Generating execution plan..."}

event: token
data: {"content": "The authentication module has"}

event: critique
data: {"finding": "SQL injection risk", "severity": "high"}

event: done
data: {"task_id": "uuid", "status": "completed"}
```

---

### Submit Batch Tasks 🔒
```
POST /tasks/batch
```

---

### HITL Decision for Task 🔒
```
POST /tasks/{taskID}/hitl
```

---

## Human-in-the-Loop (HITL)

### List Pending HITL Decisions 🔒
```
GET /hitl/pending
```

**Response** `200 OK`:
```json
{
  "checkpoints": [
    {
      "id": "uuid",
      "task_id": "uuid",
      "action": "execute_shell_command",
      "details": "rm -rf /tmp/build",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

---

### Submit HITL Decision 🔒
```
POST /hitl/decide
```

**Request Body:**
```json
{
  "checkpoint_id": "uuid",
  "decision": "approved",
  "reason": "Command is safe to execute"
}
```

---

### Get HITL Status 🔒
```
GET /hitl/status
```

---

## Memory

### Store Memory Entry 🔒
```
POST /memory
```

**Request Body:**
```json
{
  "content": "The user prefers Python for backend tasks",
  "type": "semantic",
  "metadata": {
    "source": "conversation",
    "session_id": "uuid"
  }
}
```

---

### Search Memory 🔒
```
POST /memory/search
```
Performs semantic vector similarity search using pgvector.

**Request Body:**
```json
{
  "query": "What programming language does the user prefer?",
  "limit": 5,
  "threshold": 0.7
}
```

**Response** `200 OK`:
```json
{
  "results": [
    {
      "id": "uuid",
      "content": "The user prefers Python for backend tasks",
      "similarity": 0.92,
      "type": "semantic",
      "created_at": "2026-01-01T00:00:00Z"
    }
  ]
}
```

---

## Code Analysis & Scanning

### Static Security Scan 🔒
```
POST /scan
```

**Request Body:**
```json
{
  "code": "func handler(w http.ResponseWriter, r *http.Request) {\n  db.Query(\"SELECT * FROM users WHERE id=\" + r.URL.Query().Get(\"id\"))\n}",
  "language": "go",
  "filename": "handler.go"
}
```

**Response** `200 OK`:
```json
{
  "findings": [
    {
      "rule": "sql-injection",
      "severity": "critical",
      "message": "SQL injection via string concatenation",
      "line": 2,
      "confidence": 0.95,
      "cwe": "CWE-89"
    }
  ],
  "summary": {
    "critical": 1,
    "high": 0,
    "medium": 0,
    "low": 0
  }
}
```

---

### Full Code Review 🔒
```
POST /review
```
Runs the complete parallel analysis pipeline (deterministic + LLM critique).

---

### Deep Analysis 🔒
```
POST /deep-analyze
```

---

### Requirements Validation 🔒
```
POST /requirements
```

---

### Schema Validation 🔒
```
POST /schema
```

---

### Code Validation 🔒
```
POST /validate
```

---

### Full Pipeline Validation 🔒
```
POST /validate-full
```

---

### Compliance Check 🔒
```
POST /compliance
```
Validates code against SOC2, HIPAA, and PCI-DSS standards.

---

### Knowledge Base Ingestion 🔒
```
POST /knowledge
```

---

### Skill Extraction 🔒
```
POST /skills/extract
```

---

### Confidence Scoring 🔒
```
POST /confidence
```

---

### Attack Graph Generation 🔒
```
POST /attack-graph
```

---

### Audit Trace 🔒
```
POST /audit/trace
```

---

### Middleware Processing 🔒
```
POST /middleware/process
GET  /middleware/metrics
GET  /middleware/patterns
```

---

## Skills Marketplace

### List Skills 🔒
```
GET /skills
```

---

### Get Skill 🔒
```
GET /skills/{skillID}
```

---

### Create Skill 🔒
```
POST /skills
```

**Request Body:**
```json
{
  "name": "code-reviewer",
  "description": "Automated code review skill",
  "version": "1.0.0",
  "tags": ["review", "security"],
  "manifest": {
    "entry_point": "review.go",
    "permissions": ["file_read"],
    "tools": ["static_analysis"]
  }
}
```

---

### Update Skill 🔒
```
PUT /skills/{skillID}
```

---

### Delete Skill 🔒
```
DELETE /skills/{skillID}
```

---

### Rate Skill 🔒
```
POST /skills/{skillID}/rate
```

**Request Body:**
```json
{
  "rating": 5,
  "review": "Excellent code review coverage"
}
```

---

### Get Skill Ratings 🔒
```
GET /skills/{skillID}/ratings
```

---

### Install Skill 🔒
```
POST /skills/{skillID}/install
```

---

## LLM Providers

### List Providers
```
GET /providers
```
Public endpoint. Returns all configured LLM providers and their status.

---

### List Provider Models
```
GET /providers/{providerID}/models
```

---

### Get Model Details
```
GET /models/{modelID}
```

---

### Provider Health 🔒
```
GET /providers/health
```
Returns real-time health status and latency for all providers.

---

### Cost Override (Admin) 🔒
```
POST /providers/cost-override
```

---

## Analytics & Cost Intelligence

### Cost Analytics 🔒
```
GET /analytics/cost
```
Returns aggregate cost data across all LLM usage.

---

### Token Usage 🔒
```
GET /analytics/tokens
```

---

### Session Analytics 🔒
```
GET /analytics/sessions
```

---

### Cost Intelligence Dashboard 🔒
```
GET /analytics/cost-intel
```
Returns comprehensive cost tracking data.

---

### Cost Forecast 🔒
```
GET /analytics/cost-intel/forecast
```
Projects future costs based on current usage patterns.

---

### Optimization Recommendations 🔒
```
GET /analytics/cost-intel/recommendations
```
Suggests cost-saving model routing changes.

---

### Cost Anomaly Detection 🔒
```
GET /analytics/cost-intel/anomalies
```
Identifies unusual spending patterns.

---

### Dashboard Overview 🔒
```
GET /dashboard/overview
GET /dashboard/activity
GET /dashboard/top-agents
```

---

## Alerts

### List Alerts 🔒
```
GET /alerts
```

---

### Create Alert 🔒
```
POST /alerts
```

---

### Get Alert 🔒
```
GET /alerts/{alertID}
```

---

### Update Alert 🔒
```
PUT /alerts/{alertID}
```

---

### Delete Alert 🔒
```
DELETE /alerts/{alertID}
```

---

## API Keys

### Create API Key 🔒
```
POST /api-keys
```

**Request Body:**
```json
{
  "name": "Production Key",
  "scopes": ["read", "write", "scan"]
}
```

**Response** `201 Created`:
```json
{
  "id": "uuid",
  "key": "vgl_sk_abc123...",
  "name": "Production Key",
  "scopes": ["read", "write", "scan"],
  "created_at": "2026-01-01T00:00:00Z"
}
```

> ⚠️ The `key` field is only returned once at creation time. It is stored as a SHA-256 hash.

---

### List API Keys 🔒
```
GET /api-keys
```

---

### Rotate API Key 🔒
```
POST /api-keys/{keyID}/rotate
```

---

### Delete API Key 🔒
```
DELETE /api-keys/{keyID}
```

---

## Webhooks

### Register Webhook 🔒
```
POST /webhooks
```

**Request Body:**
```json
{
  "url": "https://example.com/webhook",
  "events": ["task.completed", "task.failed", "hitl.required"],
  "secret": "webhook-signing-secret"
}
```

---

### List Webhooks 🔒
```
GET /webhooks
```

---

### Get Webhook 🔒
```
GET /webhooks/{webhookID}
```

---

### Delete Webhook 🔒
```
DELETE /webhooks/{webhookID}
```

---

### Get Webhook Stats 🔒
```
GET /webhooks/stats
```

---

### List Deliveries 🔒
```
GET /webhooks/{webhookID}/deliveries
```

---

### Replay Delivery 🔒
```
POST /webhooks/replay
```

---

## Billing

### List Invoices 🔒
```
GET /billing/invoices
GET /billing/invoices/{invoiceID}
```

---

### Create Checkout Session 🔒
```
POST /billing/checkout
```

---

### Get Subscription 🔒
```
GET /billing/subscription
```

---

### Customer Portal 🔒
```
POST /billing/portal
```

---

## Admin

All admin endpoints require the `admin` role.

### System Statistics 🔒
```
GET /admin/stats
```

---

### List Users 🔒
```
GET /admin/users
```

---

### Update User Role 🔒
```
PUT /admin/users/{userID}/role
```

---

### Delete User 🔒
```
DELETE /admin/users/{userID}
```

---

### Feature Flags 🔒
```
GET    /feature-flags
PUT    /feature-flags
DELETE /feature-flags
GET    /feature-flags/check
```

---

### Audit Logs 🔒
```
GET  /audit/logs
POST /audit/cleanup
GET  /audit/retention
```

---

## System

### Health Check
```
GET /health
```
**Response** `200 OK`:
```json
{
  "status": "healthy",
  "timestamp": "2026-01-01T00:00:00Z"
}
```

---

### Readiness Check
```
GET /ready
```
Checks PostgreSQL, Redis, and NATS connectivity.

---

### Swagger UI
```
GET /docs
```

---

### OpenAPI Spec
```
GET /docs/openapi.yaml
```

---

### Prometheus Metrics
```
GET /metrics
```

---

## Real-Time

### WebSocket Connection 🔒
```
GET /ws
```
Upgrades to a WebSocket connection for real-time bidirectional communication.

---

### Rate Limit Dashboard 🔒
```
GET /ratelimit/dashboard
```

---

### Export / Import 🔒
```
GET  /export/conversations
GET  /export/skills
POST /import
```

---

### Batch Operations 🔒
```
POST /batch
```

---

## Error Responses

All errors follow a consistent format:

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Invalid request body",
    "details": [
      {
        "field": "email",
        "message": "must be a valid email address"
      }
    ]
  }
}
```

### HTTP Status Codes

| Code | Meaning                                          |
|------|--------------------------------------------------|
| 200  | Success                                          |
| 201  | Created                                          |
| 400  | Bad Request — invalid input                      |
| 401  | Unauthorized — missing or invalid authentication |
| 403  | Forbidden — insufficient permissions             |
| 404  | Not Found — resource does not exist              |
| 409  | Conflict — duplicate resource                    |
| 422  | Unprocessable Entity — validation failure        |
| 429  | Too Many Requests — rate limit exceeded          |
| 500  | Internal Server Error                            |

---

## Rate Limiting

Rate limits are enforced per-user using Redis-backed token bucket and sliding window algorithms.

| Tier    | Requests/Minute |
|---------|-----------------|
| Free    | 60              |
| Pro     | 300             |
| Admin   | Unlimited       |

When rate limited, the response includes:
```
HTTP/1.1 429 Too Many Requests
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1704067260
Retry-After: 30
```

---

## Pagination

List endpoints support cursor-based pagination:

```
GET /tasks?limit=20&offset=0
```

| Parameter | Type    | Default | Description           |
|-----------|---------|---------|-----------------------|
| `limit`   | integer | 20      | Items per page (max 100) |
| `offset`  | integer | 0       | Number of items to skip  |

**Response includes pagination metadata:**
```json
{
  "data": [...],
  "pagination": {
    "total": 150,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```
