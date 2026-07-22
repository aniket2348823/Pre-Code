# VigilAgent — API Style Guide

> Engineering standards for all API development. Every new endpoint and modification must follow these conventions.

---

## 1. Naming Conventions

### URL Patterns
- **Plural nouns** for collections: `/projects`, `/tasks`, `/skills`
- **Singular for actions**: `/auth/login`, `/tasks/{id}/actions/cancel`
- **Lowercase kebab-case** for multi-word paths: `/cost-intel`, `/api-keys`
- **No verbs in URLs** (use HTTP methods instead)
- **Nested resources** for hierarchical relationships: `/projects/{id}/agents`

### Path Parameters
- Use `{resourceID}` format: `{projectID}`, `{taskID}`, `{skillID}`
- Always UUID format: `550e8400-e29b-41d4-a716-446655440000`

### Query Parameters
- **snake_case**: `org_id`, `page_size`, `sort_by`
- **Boolean**: `is_active=true`, `include_deleted=false`
- **Date range**: `from=2026-01-01&to=2026-07-15`

### Request/Response Fields
- **snake_case** in JSON: `created_at`, `user_id`, `is_active`
- **Consistent naming**: `id` (not `ID` or `Id`), `org_id` (not `organization_id`)

---

## 2. Routing

### Route Organization
```
/api/v1/
├── /auth/                    # Authentication
├── /users/                   # User management
├── /organizations/           # Organization management
├── /projects/                # Project management
│   └── /{projectID}/agents/  # Agents within projects
├── /agents/                  # Agent management
│   └── /{agentID}/sessions/  # Sessions within agents
├── /sessions/                # Session management
├── /tasks/                   # Task management
├── /memory/                  # Memory system
├── /skills/                  # Skills marketplace
├── /alerts/                  # Alert management
├── /billing/                 # Billing management
├── /api-keys/                # API key management
├── /webhooks/                # Webhook management
├── /analytics/               # Analytics
├── /dashboard/               # Dashboard
├── /admin/                   # Admin operations
└── /internal/                # Internal endpoints
```

### HTTP Methods
| Method | Purpose | Idempotent | Safe |
|--------|---------|------------|------|
| `GET` | Read resource | Yes | Yes |
| `POST` | Create resource / Execute action | No | No |
| `PUT` | Full replacement | Yes | No |
| `PATCH` | Partial update | Yes | No |
| `DELETE` | Remove resource | Yes | No |

---

## 3. Versioning

### Strategy
- **URL-based versioning**: `/api/v1/`, `/api/v2/`
- **Minor changes**: Additive fields, new endpoints (no version bump)
- **Breaking changes**: Remove/rename fields, change semantics (bump version)
- **Deprecation period**: 6 months minimum

### Deprecation Headers
```http
HTTP/1.1 200 OK
Deprecation: Sat, 01 Jan 2027 00:00:00 GMT
Sunset: Sat, 01 Jul 2027 00:00:00 GMT
Link: <https://api.vigilagent.com/api/v2/tasks>; rel="successor-version"
```

---

## 4. Pagination

### Cursor-Based Pagination (Preferred)
```
GET /api/v1/tasks?project_id=xxx&limit=20&cursor=abc123
```

Response:
```json
{
  "data": [...],
  "meta": {
    "limit": 20,
    "has_more": true,
    "next_cursor": "def456"
  }
}
```

### Offset-Based Pagination (Fallback)
```
GET /api/v1/tasks?project_id=xxx&limit=20&offset=0
```

Response:
```json
{
  "data": [...],
  "meta": {
    "total": 156,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

### Defaults
- Default `limit`: 20
- Maximum `limit`: 100
- Default `offset`: 0

---

## 5. Sorting

### Pattern
```
GET /api/v1/tasks?sort=created_at&order=desc
GET /api/v1/skills?sort=downloads&order=desc
```

### Conventions
- `sort` parameter: field name (snake_case)
- `order` parameter: `asc` (default) or `desc`
- Multiple sorts: `sort=created_at,-cost` (prefix `-` for descending)

---

## 6. Filtering

### Pattern
```
GET /api/v1/tasks?status=pending&model=gpt-4o&from=2026-01-01&to=2026-07-15
GET /api/v1/skills?category=security&min_rating=4&is_published=true
```

### Filter Types
| Type | Example | SQL Equivalent |
|------|---------|----------------|
| Equality | `?status=pending` | `WHERE status = 'pending'` |
| Range | `?from=2026-01-01&to=2026-07-15` | `WHERE created_at BETWEEN ...` |
| Minimum | `?min_rating=4` | `WHERE rating >= 4` |
| Boolean | `?is_active=true` | `WHERE is_active = true` |
| Search | `?q=authentication` | `WHERE name ILIKE '%authentication%'` |

---

## 7. Searching

### Full-Text Search
```
GET /api/v1/skills?q=security+scanner&category=security
```

### Search Response
```json
{
  "data": [...],
  "meta": {
    "query": "security scanner",
    "total": 12,
    "expanded_to": ["security scanner", "vulnerability detection"]
  }
}
```

---

## 8. Status Codes

### Success Codes
| Code | Usage |
|------|-------|
| `200 OK` | Successful GET, PUT, PATCH |
| `201 Created` | Successful POST (resource created) |
| `204 No Content` | Successful DELETE |

### Client Error Codes
| Code | Usage |
|------|-------|
| `400 Bad Request` | Invalid request body or parameters |
| `401 Unauthorized` | Missing or invalid authentication |
| `403 Forbidden` | Authenticated but not authorized |
| `404 Not Found` | Resource does not exist |
| `409 Conflict` | Resource already exists (duplicate) |
| `422 Unprocessable Entity` | Validation failure |
| `429 Too Many Requests` | Rate limit exceeded |

### Server Error Codes
| Code | Usage |
|------|-------|
| `500 Internal Server Error` | Unexpected server failure |
| `502 Bad Gateway` | Upstream service failure (LLM provider) |
| `503 Service Unavailable` | Service temporarily down |

---

## 9. JSON Responses

### Content Type
```http
Content-Type: application/json; charset=utf-8
```

### Success Response (Single Resource)
```json
{
  "data": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "name": "My Project",
    "org_id": "org-uuid",
    "status": "active",
    "created_at": "2026-07-15T10:00:00Z",
    "updated_at": "2026-07-15T10:00:00Z"
  }
}
```

### Success Response (Collection)
```json
{
  "data": [
    {"id": "...", "name": "Project 1"},
    {"id": "...", "name": "Project 2"}
  ],
  "meta": {
    "total": 42,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

### Success Response (Action)
```json
{
  "data": {
    "id": "task-uuid",
    "status": "cancelled",
    "message": "Task cancelled successfully"
  }
}
```

### Timestamps
- Always ISO 8601 format: `2026-07-15T10:00:00Z`
- Always UTC
- Field names: `created_at`, `updated_at`, `started_at`, `ended_at`

---

## 10. Errors

### Error Response Format
```json
{
  "error": {
    "code": "VALIDATION_001",
    "message": "Request validation failed",
    "details": [
      {
        "field": "name",
        "rule": "required",
        "message": "name is required"
      },
      {
        "field": "email",
        "rule": "format",
        "message": "invalid email format"
      }
    ]
  }
}
```

### Error Code Convention
```
CATEGORY_NUMBER
```

| Category | Prefix | Examples |
|----------|--------|----------|
| Authentication | `AUTH_` | `AUTH_001`, `AUTH_002` |
| Validation | `VALIDATION_` | `VALIDATION_001` |
| Not Found | `NOT_FOUND_` | `NOT_FOUND_001` |
| Permission | `PERMISSION_` | `PERMISSION_001` |
| Rate Limit | `RATE_LIMIT_` | `RATE_LIMIT_001` |
| Internal | `INTERNAL_` | `INTERNAL_001` |
| External | `EXTERNAL_` | `EXTERNAL_001` |

### Error Response Headers
```http
HTTP/1.1 429 Too Many Requests
Retry-After: 60
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1689434460
```

---

## 11. Validation

### Request Validation
- Validate all input before processing
- Return structured validation errors with field-level details
- Never expose internal error messages to clients

### Validation Rules
| Rule | Example | Description |
|------|---------|-------------|
| `required` | `org_id` | Field must be present |
| `uuid` | `projectID` | Must be valid UUID |
| `email` | `email` | Must be valid email format |
| `min` | `password` (min 12) | Minimum length/value |
| `max` | `name` (max 255) | Maximum length/value |
| `pattern` | `slug` | Must match regex |
| `enum` | `status` | Must be one of allowed values |

---

## 12. Logging

### Structured Logging
```json
{
  "level": "info",
  "time": "2026-07-15T10:00:00Z",
  "msg": "request completed",
  "request_id": "abc123",
  "method": "POST",
  "path": "/api/v1/tasks",
  "status": 201,
  "duration_ms": 45,
  "user_id": "user-uuid",
  "org_id": "org-uuid"
}
```

### Log Levels
| Level | Usage |
|-------|-------|
| `debug` | Development debugging |
| `info` | Normal operations |
| `warn` | Degraded but functional |
| `error` | Failure requiring attention |

### Never Log
- Passwords or password hashes
- JWT tokens
- API key plaintext
- Credit card numbers
- Personal identifiable information (PII)

---

## 13. Request IDs

### Generation
- Auto-generated if not provided: 32-character hex string
- Client-provided via `X-Request-Id` header (reused)

### Propagation
```http
Request:
X-Request-Id: abc123def456

Response:
X-Request-Id: abc123def456
```

### Usage
- Include in all log entries
- Include in error responses
- Use for distributed tracing correlation

---

## 14. Tracing

### OpenTelemetry Integration
- Service name: `vigilagent-api`
- Trace context propagated via `traceparent` header
- Span creation at handler entry/exit
- Attributes: `http.method`, `http.url`, `http.status_code`

### Metrics
- Prometheus endpoint: `/api/v1/metrics`
- Standard HTTP metrics: request count, latency, error rate
- Business metrics: task completions, LLM costs, scan results

---

## 15. Streaming

### SSE (Server-Sent Events)
```http
GET /api/v1/tasks/{taskID}/stream
Accept: text/event-stream
```

Response:
```
event: token
id: 1
data: {"token": "Hello"}

event: token
id: 2
data: {"token": " world"}

event: done
id: 3
data: {"result": "...", "tokens_used": 150}
```

### Event Types
| Event | Payload | Purpose |
|-------|---------|---------|
| `token` | `{"token": "..."}` | Streaming LLM output |
| `status` | `{"status": "...", "detail": "..."}` | Status updates |
| `critique` | `{...}` | Evaluation results |
| `done` | `{...}` | Stream completion |
| `error` | `{"error": "..."}` | Error occurred |

---

## 16. File Uploads

### Pattern (Future)
```http
POST /api/v1/scan
Content-Type: multipart/form-data

--boundary
Content-Disposition: form-data; name="code"
Content-Type: text/plain

func main() { ... }
--boundary--
```

### Limits
- Maximum file size: 2 MiB (enforced by `limitBodySize` middleware)
- Allowed content types: `text/plain`, `application/json`

---

## 17. Authentication

### JWT Token
```http
Authorization: Bearer eyJhbGciOiJIUzI1NiIs...
```

### API Key
```http
X-API-Key: va_abc123def456...
# or
Authorization: Bearer va_abc123def456...
```

### Token Rotation
```http
Response:
X-New-Token: eyJhbGciOiJIUzI1NiIs...
X-Token-Rotated: true
```

---

## 18. Authorization

### Role Hierarchy
```
admin > user
```

### Permission Checks
1. **Authentication**: Verify JWT/API key is valid
2. **Role Check**: Verify user has required role (admin endpoints)
3. **Resource Ownership**: Verify user owns/belongs to the resource's org
4. **API Key Scopes**: Verify API key has required scope (when enforced)

---

## 19. Rate Limits

### Headers
```http
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1689434460
```

### Rate Limit Tiers (Future)
| Tier | Limit | Window |
|------|-------|--------|
| Free | 60 req/min | 1 minute |
| Pro | 600 req/min | 1 minute |
| Enterprise | 6000 req/min | 1 minute |

---

## 20. Idempotency

### Pattern
```http
POST /api/v1/tasks
Idempotency-Key: unique-key-from-client

# Server stores response for 24 hours
# Same key → same response (no duplicate creation)
```

### Implementation
- Client generates unique key per request
- Server stores key → response mapping in Redis (24h TTL)
- Duplicate requests return cached response
- Applies to: `POST` (create) and `DELETE` endpoints

---

## 21. Caching

### Response Caching
```http
Cache-Control: private, max-age=60
ETag: "abc123"
```

### Cache Headers
| Header | Value | Purpose |
|--------|-------|---------|
| `Cache-Control` | `private, max-age=60` | Client-side caching |
| `ETag` | `"abc123"` | Conditional requests |
| `Last-Modified` | `Wed, 15 Jul 2026 10:00:00 GMT` | Conditional requests |

### Cacheable Endpoints
- `GET /health` — No cache (always fresh)
- `GET /skills` — Short cache (60s)
- `GET /skills/{id}` — Short cache (60s)
- `GET /analytics/*` — No cache (real-time)

### Non-Cacheable Endpoints
- All `POST`, `PUT`, `PATCH`, `DELETE` — No cache
- `GET /tasks/{id}/stream` — SSE (no cache)
- `GET /admin/*` — No cache
