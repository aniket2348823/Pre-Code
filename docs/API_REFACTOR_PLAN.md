# VigilAgent — API Refactor Plan

> **Status:** Planning only — no code changes. This document is the foundation for all future API development.

---

## 1. Route Classification

### What Should Be `/api/v1` (Public API)

All existing routes are already under `/api/v1`. The classification should be refined:

| Category | Routes | Visibility |
|----------|--------|------------|
| **Public (unauthenticated)** | `/auth/*`, `/health`, `/ready`, `/metrics` | External |
| **Authenticated (JWT/API key)** | All protected routes | External |
| **Admin-only** | `/admin/*` | Internal (platform operators) |

### What Should Remain Internal

- `/metrics` — Should be behind auth or network-level restriction in production
- `/admin/*` — Platform admin only, should require admin role + separate auth
- `/providers/cost-override` — Operational endpoint, not for external consumers
- `/middleware/*` — Engine internals, not a public API surface

### Recommended Internal-Only Endpoints

```
POST /internal/middleware/process
GET  /internal/middleware/metrics
GET  /internal/middleware/patterns
POST /internal/providers/cost-override
GET  /internal/providers/health
```

---

## 2. Duplicated Endpoints

### Identified Duplications

| Duplication | Routes | Resolution |
|-------------|--------|------------|
| Cost analytics vs Cost Intel | `GET /analytics/cost` vs `GET /analytics/cost-intel` | Merge into `/analytics/cost` with `?view=summary|detailed` |
| Session analytics vs Dashboard | `GET /analytics/sessions` vs `GET /dashboard/overview` (overlapping data) | Dashboard should compose from analytics endpoints |
| Dashboard overview vs individual analytics | `GET /dashboard/overview` calls cost + token + session analytics | Keep dashboard as a composition endpoint, document it as such |
| Validate vs Validate-full vs Schema vs Compliance | 4 separate validation endpoints | Consider `/validate` with `?layers=schema,requirements,compliance,scan` |
| Memory search vs Memory create | `POST /memory/search` vs `POST /memory` | RESTful: `POST /memory` for create, `GET /memory/search?q=...` for search |

### Recommended Merges

1. **Analytics:** Consolidate `cost`, `tokens`, `sessions` into `GET /analytics?org_id=...&from=...&to=...&metrics=cost,tokens,sessions`
2. **Validation:** Keep individual endpoints but add a unified `POST /validate` that runs all layers
3. **Memory:** Change `POST /memory/search` to `GET /memory/search?q=...&types=...&limit=...`

---

## 3. REST Principle Violations

| Violation | Location | Fix |
|-----------|----------|-----|
| `POST /tasks/{taskID}/cancel` | POST for action | `POST /tasks/{taskID}/actions/cancel` or `PATCH /tasks/{taskID}` with `{"status":"cancelled"}` |
| `POST /tasks/{taskID}/hitl` | POST for action | `POST /tasks/{taskID}/actions/approve` |
| `POST /tasks/batch` | Non-RESTful batch | `POST /tasks:batch` (Google-style) or keep as-is |
| `POST /memory/search` | POST for read | `GET /memory/search?q=...` |
| `POST /validate` | POST for validation (read-like) | Acceptable for complex payloads |
| `POST /scan` | POST for analysis | Acceptable for code payload |
| `POST /skills/{skillID}/rate` | Nested action | `POST /skills/{skillID}/ratings` (create a rating resource) |
| `POST /skills/{skillID}/install` | Nested action | `POST /skills/{skillID}/installs` (create an install resource) |
| `POST /webhooks` | No idempotency key | Add `Idempotency-Key` header support |

---

## 4. Missing Middleware

| Middleware | Priority | Purpose |
|-----------|----------|---------|
| **Pagination** | P1 | Cursor or offset pagination for all list endpoints |
| **Filtering** | P1 | Query parameter parsing for list endpoints |
| **Sorting** | P2 | `?sort=created_at&order=desc` support |
| **Idempotency** | P1 | `Idempotency-Key` header for POST endpoints |
| **Request Validation** | P1 | Structured input validation (replace manual JSON decode) |
| **Response Envelope** | P2 | Consistent `{data, meta, errors}` response format |
| **Deprecation** | P3 | `Sunset` and `Deprecation` headers |
| **API Version Negotiation** | P3 | `Accept` header versioning |
| **Request Logging** | P2 | Audit trail for all state-changing operations |
| **RBAC** | P2 | Project-level role checks |

---

## 5. Authentication Improvements

### Current Issues
1. API key scopes exist in DB but are not enforced in middleware
2. No OAuth2/OIDC support for third-party integrations
3. No API key rotation mechanism
4. No session management (concurrent session limit)
5. JWT tokens have no revocation mechanism

### Recommended Improvements

| Improvement | Priority | Effort | Impact |
|-------------|----------|--------|--------|
| Enforce API key scopes | P1 | Low | Security |
| Add API key rotation | P1 | Medium | Security |
| Implement JWT blacklist (Redis) | P2 | Medium | Security |
| Add OAuth2/OIDC support | P2 | High | Developer experience |
| Add session management | P3 | Medium | Security |
| Add MFA support | P3 | High | Security |

---

## 6. Rate Limiting Improvements

### Current Issues
1. In-memory rate limiting doesn't work across instances
2. No per-plan rate limiting (free vs pro vs enterprise)
3. No per-endpoint rate limiting configuration
4. No rate limit bypass for internal services
5. Rate limit headers added globally but limits applied per-group

### Recommended Improvements

| Improvement | Priority | Effort | Impact |
|-------------|----------|--------|--------|
| Redis-backed rate limiting for all endpoints | P1 | Medium | Scalability |
| Per-plan rate limits | P1 | Medium | Revenue |
| Per-endpoint configurable limits | P2 | Medium | Flexibility |
| Rate limit bypass for service-to-service | P2 | Low | Operations |
| Sliding window for all endpoints | P2 | Low | Accuracy |

---

## 7. Pagination

### Current State
- Most list endpoints return all records (no pagination)
- `TaskRepository.ListByProject` accepts `offset, limit` but handlers don't expose it
- `SkillRepository.List` accepts `offset, limit`
- `UserRepository.List` accepts `offset, limit`

### Recommended Pagination Pattern

```json
// Request
GET /api/v1/projects?org_id=xxx&cursor=abc123&limit=20

// Response
{
  "data": [...],
  "meta": {
    "total": 156,
    "limit": 20,
    "has_more": true,
    "next_cursor": "def456"
  }
}
```

### Endpoints Needing Pagination

| Endpoint | Current | Fix |
|----------|---------|-----|
| `GET /organizations` | Returns all | Add `?limit=&cursor=` |
| `GET /projects` | Returns all | Add `?limit=&cursor=` |
| `GET /projects/{id}/agents` | Returns all | Add `?limit=&cursor=` |
| `GET /agents/{id}/sessions` | Returns all | Add `?limit=&cursor=` |
| `GET /tasks` | Has offset/limit in repo, not exposed | Expose pagination params |
| `GET /alerts` | Returns all | Add `?limit=&cursor=` |
| `GET /api-keys` | Returns all | Add `?limit=&cursor=` |
| `GET /webhooks` | Returns all | Add `?limit=&cursor=` |
| `GET /skills` | Has offset/limit in repo | Expose pagination params |
| `GET /skills/{id}/ratings` | Has offset/limit in repo | Expose pagination params |
| `GET /admin/users` | Has offset/limit in repo | Expose pagination params |

---

## 8. Filtering & Sorting

### Recommended Filtering Pattern

```
GET /api/v1/tasks?project_id=xxx&status=pending&model=gpt-4o&from=2026-01-01&to=2026-07-15
GET /api/v1/skills?category=security&min_rating=4&sort=downloads&order=desc
GET /api/v1/alerts?is_active=true&type=cost_threshold
```

### Endpoints Needing Filtering

| Endpoint | Filters Needed |
|----------|---------------|
| `GET /tasks` | `status`, `project_id`, `model`, `from`, `to` |
| `GET /skills` | `category`, `min_rating`, `sort`, `order` |
| `GET /alerts` | `type`, `is_active` |
| `GET /events` | `event_type`, `from`, `to` |
| `GET /admin/users` | `role`, `is_active`, `search` |
| `GET /analytics/cost` | `from`, `to` (already exists) |

---

## 9. Validation Improvements

### Current State
- Manual `json.NewDecoder().Decode()` + string checks in every handler
- `internal/validator` package exists but is not used by handlers
- No request body schema validation
- No query parameter validation

### Recommended Approach

```go
// Use struct tags for validation
type CreateProjectRequest struct {
    OrgID       string `json:"org_id" validate:"required,uuid"`
    Name        string `json:"name" validate:"required,min=1,max=255"`
    Description string `json:"description" validate:"max=1000"`
}
```

### Validation Middleware

```go
func ValidateRequest[T any](next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var req T
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            response.BadRequest(w, "invalid request body")
            return
        }
        if err := validate.Struct(req); err != nil {
            response.ValidationError(w, err)
            return
        }
        ctx := context.WithValue(r.Context(), validatedRequestKey, &req)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

---

## 10. Response Format Inconsistencies

### Current Issues

| Issue | Examples | Fix |
|-------|----------|-----|
| Mixed success responses | `{"message": "..."}` vs `{"token": "..."}` vs `{"id": "..."}` | Standardize to `{"data": {...}}` |
| Error field inconsistency | `"error"` vs `"message"` vs `"code"` | Standardize to `{"error": {"code": "...", "message": "..."}}` |
| No envelope wrapping | Raw arrays vs objects | Wrap all responses in `{"data": ..., "meta": ...}` |
| HTTP status code inconsistency | Some errors return 200, some 400 | Strict status code usage |

### Recommended Response Format

```json
// Success (single)
{
  "data": {
    "id": "uuid",
    "name": "My Project",
    "created_at": "2026-07-15T10:00:00Z"
  }
}

// Success (list)
{
  "data": [...],
  "meta": {
    "total": 100,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}

// Error
{
  "error": {
    "code": "VALIDATION_001",
    "message": "org_id is required",
    "details": [
      {"field": "org_id", "rule": "required", "message": "org_id is required"}
    ]
  }
}

// Validation Error
{
  "error": {
    "code": "VALIDATION_001",
    "message": "Request validation failed",
    "details": [
      {"field": "name", "rule": "required", "message": "name is required"},
      {"field": "email", "rule": "format", "message": "invalid email format"}
    ]
  }
}
```

---

## 11. Prioritization

### Phase 1: Foundation (Weeks 1-4)

| Task | Effort | Impact | Risk |
|------|--------|--------|------|
| Add pagination middleware | Medium | High | Low |
| Standardize error responses | Medium | High | Low |
| Enforce API key scopes | Low | High | Low |
| Add request validation middleware | Medium | High | Low |
| Add idempotency key support | Medium | High | Low |

### Phase 2: Polish (Weeks 5-8)

| Task | Effort | Impact | Risk |
|------|--------|--------|------|
| Add filtering/sorting middleware | Medium | Medium | Low |
| Implement billing endpoints | High | High | Medium |
| Add Redis-backed rate limiting | Medium | High | Low |
| Add API key rotation | Medium | Medium | Low |
| Standardize response envelope | High | Medium | Medium |

### Phase 3: Scale (Weeks 9-12)

| Task | Effort | Impact | Risk |
|------|--------|--------|------|
| Add RBAC middleware | High | High | Medium |
| Add OAuth2/OIDC support | High | High | High |
| Add request/response logging | Medium | Medium | Low |
| Add API versioning strategy | Medium | Medium | Low |
| Add WebSocket rooms/channels | High | Medium | Medium |

### Phase 4: Enterprise (Weeks 13-16)

| Task | Effort | Impact | Risk |
|------|--------|--------|------|
| Add MFA support | High | Medium | Medium |
| Add audit trail logging | Medium | High | Low |
| Add GraphQL endpoint | High | Medium | High |
| Add API documentation (OpenAPI) | Medium | High | Low |
| Add per-plan rate limiting | Medium | High | Low |

---

## 12. What Should Never Change

| Item | Reason |
|------|--------|
| `/api/v1/health` | Kubernetes liveness probe depends on it |
| `/api/v1/ready` | Kubernetes readiness probe depends on it |
| JWT token format | All clients depend on it |
| API key prefix `va_` | All integrations depend on it |
| Error HTTP status codes (401, 403, 404, 500) | Client error handling depends on them |
| Database schema primary keys (UUID) | All foreign keys depend on them |
| Webhook event types | All webhook consumers depend on them |
| SSE event types (`token`, `done`, `error`) | VS Code extension depends on them |
