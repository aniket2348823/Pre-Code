# VigilAgent — Complete API Audit

## 1. Repository Overview

VigilAgent is an AI agent management platform built in Go that provides real-time monitoring, analytics, and control for AI agents. The system exposes a production-grade REST API powering a web dashboard, CLI, VS Code extension, MCP server, and third-party integrations.

**Repository:** `github.com/vigilagent/vigilagent`
**Language:** Go 1.22+ (Dockerfile) / Go 1.26+ (go.mod)
**License:** Internal / Proprietary
**Maturity:** Sprint 4 complete (Weeks 13–16) — functional prototype with production-ready patterns

---

## 2. Current Architecture

```
                    ┌──────────────────────────────────────────┐
                    │              HTTP Clients                 │
                    │  Dashboard / CLI / VS Code / MCP / 3rd   │
                    └──────────────┬───────────────────────────┘
                                   │
                    ┌──────────────▼───────────────────────────┐
                    │         Chi Router (chi/v5)               │
                    │   /api/v1/* → Middleware → Handlers       │
                    └──────────────┬───────────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              │                    │                    │
    ┌─────────▼─────────┐ ┌───────▼───────┐ ┌─────────▼─────────┐
    │   Auth Layer       │ │ Rate Limiter  │ │ Security Headers  │
    │ JWT + API Keys     │ │ Redis-backed  │ │ CORS + CSRF       │
    └─────────┬─────────┘ └───────────────┘ └───────────────────┘
              │
    ┌─────────▼─────────────────────────────────────────┐
    │              Handler Layer (internal/router)       │
    │  Auth · Orgs · Projects · Agents · Sessions ·     │
    │  Tasks · Memory · Skills · Alerts · Billing ·     │
    │  Webhooks · Admin · Analytics · Dashboard ·       │
    │  Scanner · Review · Requirements · Validation ·   │
    │  Schema · Compliance · Pipeline · Cost Intel ·    │
    │  Provider Health · Middleware Engine               │
    └─────────┬─────────────────────────────────────────┘
              │
    ┌─────────▼─────────────────────────────────────────┐
    │          Repository Layer (internal/repository)    │
    │  UserRepo · OrgRepo · ProjectRepo · AgentRepo ·   │
    │  SessionRepo · EventRepo · TaskRepo · SkillRepo · │
    │  AlertRepo · APIKeyRepo                            │
    └─────────┬─────────────────────────────────────────┘
              │
    ┌─────────▼─────────────────────────────────────────┐
    │          Infrastructure Layer                      │
    │  PostgreSQL 16+pgvector · Redis 7 · NATS JetStream│
    └───────────────────────────────────────────────────┘
```

### Architectural Pattern
- **Layered architecture:** Router → Middleware → Handler → Repository → Database
- **No formal service layer:** Business logic lives in handlers (monolithic handler pattern)
- **Interface-based repositories:** 10 repository interfaces with compile-time checks
- **Event-driven webhooks:** PostgreSQL-backed webhook dispatch with async delivery
- **Pipeline pattern:** 4-layer deterministic validation pipeline (Schema → Requirements → Compliance → Static Analysis)

---

## 3. Technology Stack

| Layer | Technology | Version | Purpose |
|-------|-----------|---------|---------|
| Language | Go | 1.22/1.26 | Backend |
| Router | chi/v5 | 5.x | HTTP routing |
| Config | Viper | latest | YAML + env config |
| Database | PostgreSQL | 16 | Primary datastore |
| Vector DB | pgvector | extension | Semantic search |
| Cache | Redis | 7 | Rate limiting, sessions |
| Queue | NATS JetStream | 2.10 | Async messaging |
| Auth | JWT + API Keys | custom | Authentication |
| CLI | Cobra | latest | Command-line interface |
| Telemetry | OpenTelemetry + Prometheus | latest | Observability |
| LLM | OpenAI, Anthropic, Gemini, Mistral, Groq, DeepSeek, NVIDIA, Cohere | various | AI providers |
| Email | SMTP | custom | Transactional email |
| Billing | Stripe (placeholder) | SDK | Payment processing |

---

## 4. Dependency Graph

### External Dependencies (Key)
- `github.com/go-chi/chi/v5` — HTTP router
- `github.com/jackc/pgx/v5` — PostgreSQL driver
- `github.com/go-redis/redis/v8` — Redis client
- `github.com/spf13/viper` — Configuration
- `github.com/golang-jwt/jwt/v5` — JWT tokens
- `github.com/pgvector/pgvector-go` — Vector embeddings
- `github.com/nats-io/nats.go` — NATS messaging
- `github.com/stripe/stripe-go/v76` — Stripe billing
- `go.opentelemetry.io/otel` — OpenTelemetry tracing
- `github.com/prometheus/client_golang` — Prometheus metrics

### Internal Package Dependencies
```
router → middleware, auth, repository, config, webhook, scanner, sse, skills
middleware → auth, database, response
repository → database (pgx)
scanner → (self-contained analyzers)
memory → database, pgvector
llm → (self-contained, interface-based)
pipeline → schema, requirements, compliance, scanner
skillengine → util
skills → database, repository, pgvector
webhook → pgxpool, ssrf
```

---

## 5. Existing Modules

### Core Business Modules
| Module | Location | Purpose |
|--------|----------|---------|
| Auth | `internal/auth/` | JWT generation, password hashing, claims context |
| Middleware | `internal/middleware/` | API key auth, rate limiting, JWT rotation, security |
| Router | `internal/router/` | All HTTP handlers and route definitions |
| Repository | `internal/repository/` | Database access layer (10 repos) |
| Config | `internal/config/` | Viper-based configuration with validation |
| Database | `internal/database/` | Connection pooling, migrations |

### AI/ML Modules
| Module | Location | Purpose |
|--------|----------|---------|
| LLM | `internal/llm/` | Multi-provider LLM routing, health monitoring, circuit breaker |
| Memory | `internal/memory/` | 3-layer memory (working, episodic, semantic) with pgvector |
| Scanner | `internal/scanner/` | Static analysis engine with builtin, bandit, semgrep analyzers |
| Pipeline | `internal/pipeline/` | 4-layer deterministic validation pipeline |
| Skill Engine | `internal/skillengine/` | Skill extraction from findings with ranking |
| Skills | `internal/skills/` | Marketplace registry, RAG search, embeddings |

### Infrastructure Modules
| Module | Location | Purpose |
|--------|----------|---------|
| Security | `internal/security/` | Input sanitization, XSS/SQLi detection, encryption |
| Webhook | `internal/webhook/` | DB-backed webhook dispatch with SSRF protection |
| SSE | `internal/sse/` | Server-Sent Events streaming |
| Telemetry | `internal/telemetry/` | OpenTelemetry + Prometheus setup |
| Rate Limit | `internal/ratelimit/` | Sliding window, token bucket algorithms |
| Rate Guard | `internal/rateguard/` | Rate limit guard middleware |
| Request ID | `internal/requestid/` | Request ID generation and propagation |
| Retry | `internal/retry/` | Retry logic with backoff |
| Compression | `internal/compression/` | HTTP response compression |
| CORS | `internal/cors/` | CORS middleware |
| Validation | `internal/validator/` | Input validation rules |
| Schema | `internal/schema/` | Output schema validation |
| Requirements | `internal/requirements/` | Security requirements resolution |
| Tools | `internal/tools/` | File, terminal, search tool abstractions |
| Agent | `internal/agent/` | Agent state machine and execution |
| Queue | `internal/queue/` | NATS JetStream connection and worker |
| Signing | `internal/signing/` | Request signing |
| Timeout | `internal/timeout/` | Request timeout handling |
| Slogger | `internal/slogger/` | Structured logging middleware |
| Observability | `internal/observability/` | Observability helpers |

---

## 6. Existing Repositories

| Repository | Interface | Methods | Database Table |
|-----------|-----------|---------|----------------|
| UserRepository | `UserRepositoryInterface` | Create, FindByID, FindByEmail, UpdateProfile, UpdatePassword, UpdateEmailVerified, UpdateLastLogin, UpdateRole, Delete, Count, CountActive24h, List | `users` |
| OrganizationRepository | `OrganizationRepositoryInterface` | Create, FindByID, ListByUser, Update, Delete, IsMember, IsOwner, AddMember | `organizations`, `organization_members` |
| ProjectRepository | `ProjectRepositoryInterface` | Create, FindByID, ListByOrg, Update, Delete | `projects` |
| AgentRepository | `AgentRepositoryInterface` | Create, FindByID, ListByProject, Update, Delete | `agents` |
| SessionRepository | `SessionRepositoryInterface` | Create, FindByID, ListByAgent, Update, EndSession | `sessions` |
| EventRepository | `EventRepositoryInterface` | Create, BatchCreate, GetCostByOrg, GetTokensByOrg, GetSessionStatsByOrg, GetTopAgentsByOrg, GetRecentActivity | `events` |
| APIKeyRepository | `APIKeyRepositoryInterface` | Create, FindByHash, ListByUser, Delete | `api_keys` |
| TaskRepository | `TaskRepositoryInterface` | Create, FindByID, ListByProject, UpdateStatus, Complete, Cancel, Delete | `tasks` |
| SkillRepository | `SkillRepositoryInterface` | Create, FindByID, List, Update, Delete, IncrementDownloads, AddRating, ListRatings | `skills`, `skill_ratings` |
| AlertRepository | `AlertRepositoryInterface` | Create, FindByID, ListByUser, Update, Delete | `alerts` |

---

## 7. Existing Middleware

| Middleware | Location | Purpose | Applied To |
|-----------|----------|---------|------------|
| `RealIP` | chi built-in | Extract real client IP | All routes |
| `Logger` | chi built-in | Request logging | All routes |
| `Recoverer` | chi built-in | Panic recovery | All routes |
| `Heartbeat` | chi built-in | `/health` endpoint | All routes |
| `requestid.Middleware` | internal/requestid | Generate/propagate request IDs | All routes |
| `slogger.Middleware` | internal/slogger | Structured logging with request context | All routes |
| `compression.Middleware` | internal/compression | Gzip response compression | All routes |
| `CORS Middleware` | internal/cors | CORS headers | All routes |
| `securityHeadersMiddleware` | internal/router | Security headers (X-Frame-Options, CSP, HSTS) | All routes |
| `TimeoutHandler` | net/http | 30-second request timeout | All routes |
| `RateLimitHeadersMiddleware` | internal/middleware | X-RateLimit-* headers on all responses | All routes |
| `limitBodySize` | internal/router | 2 MiB request body limit | Public + Protected |
| `authRateLimitMiddleware` | internal/router | Rate limit on auth endpoints | Public group |
| `SanitizeMiddleware` | internal/middleware | SQLi/XSS/path traversal detection | Public group |
| `authMiddleware` | internal/router | JWT + API key authentication | Protected group |
| `apiKeyRateLimitMiddleware` | internal/router | Rate limit per API key/user | Protected group |
| `eventsRateLimitMiddleware` | internal/router | Separate rate limit for event ingestion | Events group |
| `adminMiddleware` | internal/router | Admin role check | Admin group |
| `JWTRotationMiddleware` | internal/middleware | Auto-rotate JWT on specific endpoints | Refresh endpoint |
| `RequireJWTRefresh` | internal/middleware | Force token refresh | Profile update |
| `AuthSessionMiddleware` | internal/middleware | Set PostgreSQL session variable | Optional |

---

## 8. Existing APIs

### Route Structure
All routes are under `/api/v1`:

**Public (no auth):**
- `POST /auth/register` — User registration
- `POST /auth/login` — User login
- `POST /auth/forgot-password` — Password reset request
- `POST /auth/reset-password` — Password reset execution
- `GET /auth/verify-email` — Email verification

**Protected (JWT/API key required):**
- User: `GET/PUT /users/me`
- Auth: `POST /auth/refresh`
- Organizations: Full CRUD at `/organizations`
- Projects: Full CRUD at `/projects`
- Agents: Full CRUD at `/projects/{id}/agents` and `/agents/{id}`
- Sessions: CRUD at `/agents/{id}/sessions` and `/sessions/{id}`
- Tasks: Create, List, Get, Cancel, Stream, HITL at `/tasks`
- Memory: Search and Create at `/memory`
- Skills: Full CRUD + Rate + Install + RAG at `/skills`
- Alerts: Full CRUD at `/alerts`
- Billing: Invoices, Checkout, Subscription, Portal at `/billing`
- API Keys: Create, List, Delete at `/api-keys`
- Webhooks: Full CRUD + Stats + Deliveries at `/webhooks`
- Events: Create, Batch at `/sessions/{id}/events`
- Analytics: Cost, Tokens, Sessions, Cost Intel at `/analytics`
- Dashboard: Overview, Activity, Top Agents at `/dashboard`
- Scanner: Scan, Review, Requirements, Validate, Schema, Compliance, Pipeline at root
- Knowledge: Knowledge graph at `/knowledge`
- Skill Engine: Extract at `/skills/extract`
- Confidence: Confidence scoring at `/confidence`
- Attack Graph: Attack graph at `/attack-graph`
- Audit: Trace at `/audit/trace`
- Middleware: Process, Metrics, Patterns at `/middleware`
- Provider: Health, Cost Override at `/providers`
- Tasks Batch: Batch operations at `/tasks/batch`
- WebSocket: `/ws`

**Admin (admin role required):**
- `GET /admin/stats`
- `GET /admin/users`
- `PUT /admin/users/{id}/role`
- `DELETE /admin/users/{id}`

---

## 9. Existing Auth Flow

### JWT Authentication
1. User registers/logs in → server generates JWT with claims (user_id, email, role, org_id)
2. Client sends JWT in `Authorization: Bearer <token>` header
3. `authMiddleware` validates JWT, extracts claims, injects into context
4. Handlers access claims via `auth.ClaimsFromContext(ctx)`

### API Key Authentication
1. User creates API key via `POST /api-keys` → server generates key, stores SHA-256 hash in DB
2. Client sends key via `X-API-Key` header or `Authorization: Bearer va_*` (detected by underscore pattern)
3. `APIKeyAuth.Authenticate()` hashes key, looks up in DB, validates active + not expired
4. Claims injected into context (user_id + role from DB lookup)

### Token Rotation
- JWT rotation on `/auth/refresh` and `/users/me` via `JWTRotationMiddleware`
- New token returned in `X-New-Token` response header
- Forced refresh on profile updates via `RequireJWTRefresh`

### Password Security
- Minimum 12 characters enforced
- bcrypt hashing via `auth.HashPassword()`
- Account lockout after failed attempts (Redis-backed)
- Password reset via email token (time-limited, single-use)

---

## 10. Existing Billing Flow

**Status: Placeholder implementation**

- Stripe integration configured but not fully wired
- `billing_handlers.go` contains placeholder handlers:
  - `listInvoicesHandler` — returns empty list
  - `getInvoiceHandler` — returns 404
  - `createCheckoutHandler` — returns placeholder
  - `getSubscriptionHandler` — returns placeholder
  - `createBillingPortalHandler` — returns placeholder
- Database tables exist: `invoices`, `subscriptions` (with Stripe IDs)
- `StripeConfig` in config holds `SecretKey`, `WebhookSecret`, `SuccessURL`, `CancelURL`
- **No Stripe webhook handler implemented**
- **No actual payment processing**

---

## 11. Existing API Key Flow

1. **Create:** `POST /api-keys` with name → generates `va_*` prefix key, returns plaintext once
2. **List:** `GET /api-keys` → returns key metadata (never returns hash)
3. **Delete:** `DELETE /api-keys/{id}` → soft-deletes key
4. **Authentication:** Key hashed with SHA-256, looked up in `api_keys` table
5. **Rate Limiting:** API key requests rate-limited separately from JWT requests
6. **Scopes:** JSONB `scopes` column exists but not enforced in middleware

---

## 12. Existing Database Schema

### Tables (16 total)

| Table | Purpose | Key Columns |
|-------|---------|-------------|
| `users` | User accounts | id, email, password_hash, name, role, is_active, email_verified |
| `organizations` | Multi-tenant orgs | id, name, slug, owner_id, plan, settings |
| `organization_members` | Org membership | id, organization_id, user_id, role |
| `projects` | Projects within orgs | id, org_id, name, status |
| `agents` | AI agents | id, project_id, name, config, status |
| `sessions` | Agent sessions | id, project_id, agent_id, user_id, status |
| `events` | Session events | id, session_id, event_type, payload, tokens_used, cost_usd |
| `tasks` | Agent tasks | id, project_id, user_id, prompt, status, model, cost |
| `skills` | Marketplace skills | id, name, slug, category, downloads, rating, embedding |
| `skill_ratings` | Skill reviews | id, skill_id, user_id, rating |
| `skill_embeddings` | RAG vectors | id, skill_id, embedding, content_text |
| `skill_installs` | User installations | id, skill_id, user_id |
| `alerts` | Alert rules | id, organization_id, user_id, type, condition, channel |
| `invoices` | Billing invoices | id, organization_id, stripe_invoice_id, amount_usd |
| `subscriptions` | Billing subscriptions | id, organization_id, stripe_subscription_id, plan |
| `api_keys` | API key storage | id, user_id, key_hash, prefix, scopes, is_active |
| `budget_usage` | Budget counters | key, amount, updated_at |
| `memory_episodes` | Episodic memory | id, user_id, content, embedding |
| `memory_patterns` | Semantic memory | id, project_id, pattern_type, content, embedding |
| `webhook_endpoints` | Webhook registrations | id, user_id, url, secret, events |
| `webhook_deliveries` | Delivery results | id, endpoint_id, event_type, status_code, success |

---

## 13. Existing Event System

- Events stored in `events` table with `session_id`, `event_type`, `payload` (JSONB)
- `tokens_used`, `cost_usd`, `latency_ms` for cost tracking
- `embedding vector(1536)` for semantic event search
- Batch ingestion via `POST /sessions/{id}/events/batch`
- Analytics endpoints: cost by org, tokens by org, session stats, top agents, recent activity
- **No real-time event streaming** (SSE only for task updates)
- **No event sourcing pattern** — events are append-only analytics records

---

## 14. Existing WebSocket Usage

- Single WebSocket endpoint: `GET /ws`
- `websocket.go` and `websocket_manager.go` in router package
- Purpose: Real-time bidirectional communication
- **Limited implementation** — mostly structural, not deeply integrated
- No pub/sub event broadcasting
- No connection pooling or room management

---

## 15. Existing Streaming Usage

### SSE (Server-Sent Events)
- `internal/sse/sse.go` provides `Streamer` with thread-safe event sending
- Supports: `token`, `critique`, `done`, `error`, `status` event types
- Used for task streaming: `GET /tasks/{taskID}/stream`
- Proper HTTP headers: `text/event-stream`, `no-cache`, `keep-alive`

### LLM Streaming
- `llm.ModelRouter.StreamWithFailover()` streams tokens from LLM providers
- `StreamResult` channel wraps raw provider output
- Cost estimation from accumulated content
- Health tracking during streaming

---

## 16. Existing LLM Routing

### ModelRouter
- **5-factor complexity scoring:** task type, file count, reasoning, novelty, security tags
- **Complexity tiers:** Simple (≤0.3), Moderate (≤0.6), Complex (≤0.85), Critical (>0.85)
- **Model selection:** Maps complexity to optimal model tier
- **Failover:** Automatic provider fallback with circuit breaker
- **Budget gating:** Pre-flight cost check before provider call
- **Response caching:** Identical requests served from cache
- **Health monitoring:** Real-time provider health tracking with confidence scoring

### Supported Models (18)
- Anthropic: claude-opus-4, claude-sonnet-4, claude-haiku-3.5
- OpenAI: gpt-4.5, gpt-4o, gpt-4o-mini
- DeepSeek: deepseek-r1
- Gemini: gemini-2.5-pro, gemini-2.0-flash
- Mistral: mistral-large-latest, mistral-small-latest
- Groq: llama-3.1-70b-versatile, llama-3.1-8b-instant
- NVIDIA NIM: llama-3.1-405b-instruct, llama-3.1-70b-instruct
- Cohere: command-r-plus, command-r

### Circuit Breaker
- States: Closed → Open → HalfOpen
- Threshold-based failure counting
- Timeout-based recovery
- Thread-safe with RWMutex

---

## 17. Current Folder Structure

```
VigilAgent/
├── cmd/
│   ├── api/main.go           # API server entry point
│   ├── cli/                  # CLI tool (vigil binary)
│   │   ├── main.go           # Root command + subcommands
│   │   ├── init.go           # Project initialization
│   │   ├── chat.go           # Interactive chat sessions
│   │   ├── task.go           # Task management
│   │   ├── skill.go          # Skill management
│   │   ├── usage.go          # Usage/cost analytics
│   │   ├── config.go         # CLI configuration
│   │   └── version.go        # Version info
│   └── migrate/main.go       # Database migration runner
├── internal/
│   ├── agent/                # Agent state machine and execution
│   ├── auth/                 # JWT + API key auth
│   ├── config/               # Viper configuration
│   ├── database/             # Postgres/Redis connections
│   ├── errors/               # Error types
│   ├── llm/                  # LLM provider layer
│   ├── memory/               # Memory system
│   ├── middleware/            # Rate limiter, API key auth
│   ├── queue/                # NATS JetStream
│   ├── repository/           # Data access layer
│   ├── router/               # Chi router + handlers
│   ├── scanner/              # Static analysis engine
│   ├── schema/               # Schema validation
│   ├── security/             # Security hardening
│   ├── server/               # HTTP server wrapper
│   ├── signing/              # Request signing
│   ├── skillengine/          # Skill extraction
│   ├── skills/               # Skills marketplace + RAG
│   ├── slogger/              # Structured logging
│   ├── sse/                  # Server-Sent Events
│   ├── telemetry/            # Prometheus + OpenTelemetry
│   ├── timeout/              # Request timeout
│   ├── tools/                # Tool system
│   ├── util/                 # Utilities
│   ├── validator/            # Input validation
│   └── webhook/              # Webhook system
├── pkg/
│   └── response/             # JSON response helpers
├── migrations/               # SQL migration files
├── configs/                  # YAML config files
├── deploy/                   # Deployment configs
├── k8s/                      # Kubernetes manifests
├── scripts/                  # Utility scripts
├── docs/                     # Documentation
├── extracted/                # Extracted specifications
└── Makefile
```

---

## 18. Major Strengths

1. **Clean layered architecture** — Router → Middleware → Handler → Repository is consistent
2. **Interface-based repositories** — 10 interfaces with compile-time checks enable testability
3. **Comprehensive middleware stack** — Auth, rate limiting, security headers, compression, CORS all properly chained
4. **Multi-provider LLM routing** — Sophisticated complexity-based model selection with failover
5. **Circuit breaker pattern** — Prevents cascade failures across LLM providers
6. **Budget enforcement** — Pre-flight cost gating prevents overspend
7. **4-layer validation pipeline** — Schema → Requirements → Compliance → Static Analysis
8. **RAG-powered skill search** — Vector + BM25 hybrid search with Reciprocal Rank Fusion
9. **Webhook system** — DB-backed with SSRF protection, HMAC signatures, retry logic
10. **Production-ready infrastructure** — Docker, K8s, Prometheus, health checks, graceful shutdown
11. **Security headers** — CSP, HSTS, X-Frame-Options, XSS protection all present
12. **Request ID propagation** — End-to-end tracing via X-Request-Id
13. **Email verification flow** — Complete with token generation and validation
14. **Account lockout** — Redis-backed brute force protection
15. **JWT rotation** — Automatic token refresh on sensitive endpoints

---

## 19. Major Weaknesses

1. **No service layer** — Business logic in handlers creates tight coupling and code duplication
2. **Inconsistent error handling** — Mix of `apperrors.New()` and `response.JSON()` patterns
3. **No pagination on list endpoints** — Most list endpoints return all records
4. **No filtering/sorting** — Query parameters not used for filtering (except org_id)
5. **Placeholder billing** — Stripe integration is stubbed, not functional
6. **Weak input validation** — Manual JSON decode + string checks instead of validation library
7. **No API versioning strategy** — Single `/api/v1` prefix with no deprecation path
8. **No idempotency keys** — Critical for payment and webhook endpoints
9. **No request/response logging** — Only structured logging, no payload capture
10. **No API documentation** — No OpenAPI/Swagger spec
11. **WebSocket underutilized** — Single endpoint, no rooms/channels
12. **No RBAC granularity** — Only admin/user roles, no project-level permissions
13. **No audit trail** — Security-relevant actions not logged to audit table
14. **No rate limit tiers** — All authenticated users get same limits
15. **No API key scopes enforcement** — Scopes column exists but not checked

---

## 20. Missing Components

1. **Service layer** — Dedicated business logic layer between handlers and repositories
2. **API documentation** — OpenAPI 3.0 specification
3. **Request validation library** — Structured validation with tags (e.g., `go-playground/validator`)
4. **Pagination middleware** — Cursor-based or offset pagination
5. **Filtering middleware** — Query parameter parsing for list endpoints
6. **Sorting middleware** — Order-by parameter parsing
7. **Idempotency middleware** — Idempotency key support for POST endpoints
8. **API versioning** — Header-based or URL-based versioning strategy
9. **Deprecation headers** — Sunset/Deprecation response headers
10. **Request/response logging** — Audit trail for all API calls
11. **Rate limit tiers** — Per-plan rate limiting (free/pro/enterprise)
12. **API key scope enforcement** — Check scopes before allowing operations
13. **Organization invitation system** — Invite users to orgs
14. **Project-level RBAC** — Granular permissions per project
15. **Webhook retry queue** — NATS-based retry instead of in-memory timers
16. **Email service** — Production SMTP/SendGrid integration
17. **File upload support** — For code scanning endpoints
18. **GraphQL endpoint** — For complex dashboard queries
19. **OpenTelemetry traces** — Span creation in handlers
20. **Structured error codes** — Machine-readable error codes (not just strings)

---

## 21. Technical Debt

| Item | Severity | Location | Impact |
|------|----------|----------|--------|
| Handlers contain business logic | High | `internal/router/router.go` (2000+ lines) | Maintainability, testability |
| Inconsistent error responses | Medium | All handlers | Client integration difficulty |
| No pagination on lists | High | All list endpoints | Performance at scale |
| Placeholder billing | High | `internal/router/billing_handlers.go` | Revenue blocking |
| No API documentation | High | Missing | Developer experience |
| Manual input validation | Medium | All handlers | Security risk, code duplication |
| WebSocket underutilized | Low | `internal/router/webhook_handlers.go` | Feature gap |
| No audit logging | Medium | Security handlers | Compliance risk |
| API key scopes not enforced | Medium | `internal/middleware/auth.go` | Security gap |
| Single router.go file | High | `internal/router/router.go` | Code organization |

---

## 22. Potential Risks

1. **SQL Injection** — Parameterized queries used consistently (low risk)
2. **XSS** — JSON API responses, but HTML escaping in security package (medium risk)
3. **CSRF** — CSRF middleware exists but not applied to all state-changing endpoints (medium risk)
4. **Rate Limit Bypass** — In-memory rate limits don't work across instances (high risk)
5. **JWT Secret Exposure** — Default secret in config, production check exists (medium risk)
6. **SSRF** — Webhook SSRF validator exists (low risk)
7. **Resource Exhaustion** — No pagination, unbounded list queries (high risk)
8. **LLM Cost Overrun** — Budget guard exists but not enforced on all paths (medium risk)
9. **Data Leakage** — Password hash excluded from JSON (`json:"-"`), but error messages may leak (low risk)
10. **Race Conditions** — Webhook cache has 30s TTL, concurrent dispatch possible (low risk)
