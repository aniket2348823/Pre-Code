# CLAUDE.md — VigilAgent

> **Project:** VigilAgent — Autonomous AI Agent Platform
> **Version:** 4.0 | **Last Updated:** June 2026

---

## Project Overview

VigilAgent is an autonomous AI agent platform that enables developers to deploy, orchestrate, and monetize intelligent coding agents. It combines a high-performance Go backend with multi-model LLM orchestration and a community-driven skills marketplace.

**Mission:** Eliminate the gap between human intent and working software by deploying AI agents that understand, write, review, test, and deploy code — autonomously, reliably, and cost-effectively.

---

## Tech Stack

| Layer | Technology | Version |
|-------|-----------|---------|
| **Language** | Go | 1.22+ |
| **HTTP Framework** | `chi` router | v5 |
| **Database** | PostgreSQL + pgvector | 16 |
| **Cache** | Redis | 7 |
| **Message Queue** | NATS JetStream | 2.x |
| **Object Storage** | S3-compatible (MinIO for dev) | — |
| **Observability** | OpenTelemetry + Grafana | — |
| **Container Runtime** | Docker + gVisor | — |
| **CLI Framework** | Cobra | v1.8 |
| **Testing** | testify + testcontainers | v1.9 / v0.30 |

---

## Project Structure

```
vigilagent/
├── cmd/                        # Application entry points
│   ├── api/                    # API server (main.go)
│   ├── cli/                    # CLI tool (main.go)
│   ├── worker/                 # Background workers (main.go)
│   └── migration/              # Database migrations (main.go)
│
├── internal/                   # Private application code
│   ├── agent/                  # Agent orchestration (state machine, executor)
│   ├── api/                    # HTTP handlers, middleware, routes
│   ├── auth/                   # JWT + API key authentication
│   ├── billing/                # Billing, usage tracking, budgets
│   ├── config/                 # Configuration management (env-based)
│   ├── database/               # PostgreSQL, Redis, connection pooling
│   ├── llm/                    # LLM providers, routing, failover
│   ├── memory/                 # Episodic, semantic, working memory
│   ├── skills/                 # Skill registry, installer, executor
│   ├── tools/                  # Tool implementations (file, terminal, search)
│   └── web/                    # Web dashboard (static + handlers)
│
├── pkg/                        # Public library code
│   ├── sdk/                    # Go SDK for external use
│   └── telemetry/              # OpenTelemetry setup (tracer, metrics)
│
├── migrations/                 # Database migrations (golang-migrate)
├── deployments/                # Docker, Kubernetes, Terraform
├── scripts/                    # Build, test, deploy scripts
├── docs/                       # Documentation
└── .github/                    # GitHub Actions workflows
```

---

## Coding Conventions

### Go Style

- **Go 1.22+** with modules (`github.com/vigilagent/vigilagent`)
- **Interface-driven design** — every external dependency (LLM providers, databases, storage) is accessed through Go interfaces
- **Table-driven tests** — use `[]struct{ name, input, expected }` pattern
- **Error handling** — use `AppError` type with codes; wrap errors with `fmt.Errorf("context: %w", err)`
- **Naming** — follow Go conventions: PascalCase exports, camelCase private, acronyms all-caps (URL, HTTP, ID)
- **Package layout** — `internal/` for private code, `pkg/` for public library code
- **No `any` casts** — use concrete types; exception only when truly needed
- **Minimize dependencies** — prefer stdlib; justify every external package

### Interface Patterns

```go
// Every LLM provider implements this interface
type LLMProvider interface {
    Chat(ctx context.Context, req *LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req *LLMRequest) (<-chan *LLMChunk, error)
    HealthCheck(ctx context.Context) error
    Name() string
}

// Every tool implements this interface
type Tool interface {
    Name() string
    Description() string
    Parameters() Schema
    Execute(ctx context.Context, params map[string]interface{}) (*ToolResult, error)
    RequiresHITL(params map[string]interface{}) bool
}
```

### Error Handling

```go
// Use AppError for all application errors
type AppError struct {
    Code    string                 // e.g., "RESOURCE_NOT_FOUND"
    Message string                 // Human-readable message
    Details map[string]interface{} // Optional context
    Err     error                  // Wrapped error
}

// Predefined errors
var (
    ErrNotFound       = &AppError{Code: "RESOURCE_NOT_FOUND", Message: "Resource not found"}
    ErrUnauthorized   = &AppError{Code: "UNAUTHORIZED", Message: "Authentication required"}
    ErrForbidden      = &AppError{Code: "FORBIDDEN", Message: "Insufficient permissions"}
    ErrValidation     = &AppError{Code: "VALIDATION_ERROR", Message: "Invalid request"}
    ErrRateLimit      = &AppError{Code: "RATE_LIMIT_EXCEEDED", Message: "Rate limit exceeded"}
    ErrBudgetExceeded = &AppError{Code: "BUDGET_EXCEEDED", Message: "Budget limit exceeded"}
)
```

### Configuration

- Load from environment variables using `envconfig`
- Apply sensible defaults in `applyDefaults()`
- Validate in `Validate()` method
- Never hardcode secrets; use env vars or HashiCorp Vault

### Database

- Use `pgxpool` for connection pooling (MaxConns: 25, MinConns: 5)
- All entities use UUID primary keys
- Soft deletes with `deleted_at` timestamp
- Audit columns on every table: `created_at`, `updated_at`, `deleted_at`
- JSONB for extensible metadata fields
- pgvector for embeddings (1536 dimensions)
- Time-based partitioning for high-volume tables (tasks, audit_log, usage_records)
- Use `golang-migrate` for schema migrations

### API Design

- RESTful by default; JSON everywhere
- URL-based versioning (`/v1/`, `/v2/`)
- Cursor-based pagination (not offset)
- Idempotency keys on POST/PUT endpoints
- SSE streaming for long-running operations
- Standard error format: `{ "error": { "code", "message", "details", "request_id", "timestamp" } }`

### Security

- JWT with refresh tokens for interactive sessions
- API keys for programmatic access
- TLS 1.3 in transit, AES-256 at rest
- gVisor-based container isolation for skill execution
- Never take destructive actions without HITL approval
- Validate all inputs strictly
- Log API keys as `***` (never in plaintext)
- **Pin all GitHub Actions to SHA-256 hashes** — never use `@latest` or `@main` (R48)
- **Use `pgx` directly, not `database/sql`** — avoids CVE-2025-47907 race condition (R47)
- **Set `CGO_ENABLED=0`** — eliminates CVE-2025-61732 cgo RCE (R46)
- **Parameterized queries only** — pgx `$1, $2` placeholders; never string concatenation (R21)
- **Input sanitization middleware** — strip prompt injection patterns before LLM calls (R06)
- **Supply chain scanning** — `go vuln check` + Trivy on every PR (R13, R48)
- **Zero-trust skill execution** — all third-party skills run in gVisor with network allowlists (R25)
- **Audit log completeness** — every agent action logged with user ID, session ID, microsecond timestamps (R44)

### Compliance (EU AI Act, GDPR, SOC2)

> **⚠️ Hard deadline: EU AI Act enforcement — August 2, 2026** (R28)

- **HITL gates** — mandatory human sign-off for: code writes, git operations, deployments, security fixes (R06, R19)
- **Code provenance** — record which model generated what code, when, and who approved it (R19, R28)
- **Right-to-deletion** — `deleted_at` soft delete; data purge on GDPR/CCPA request (R43)
- **Data minimization** — collect only what's needed; configurable retention per user (R18)
- **Append-only audit logs** — immutable, tamper-evident; 12 months minimum, 7 years for compliance (R44)
- **SOC2 prep starts Month 3** — RBAC, access controls, vendor management, encryption at rest/transit (R45)
- **Prompt injection defense** — zero-trust for agents; treat as "unprivileged insiders" (R06)
- **Security scanning skill** — automated OWASP pattern detection on all generated code (R19)

### Critical Security Rules (from Risk Register P0 risks)

| Risk | ID | Rule |
|------|----|------|
| Prompt injection | R06 | All prompts sanitized; HITL gates on destructive actions |
| Insecure code generation | R19 | SAST/SCA scanning on all generated code before merge |
| API key exposure | R23 | HashiCorp Vault; never log keys; rotate quarterly |
| Unauthorized access | R24 | RBAC with 4 roles; JWT 1h expiry + refresh; row-level security |
| Supply chain attack | R25 | SHA-256 checksums; gVisor isolation; community moderation |
| GDPR non-compliance | R43 | Right-to-deletion; data residency; privacy policy |
| Token cost spiraling | R31 | Hard budget limits per user/project/org; circuit breaker on costs |

---

## Development Commands

```bash
# Build all binaries
make build

# Build with race detector (development)
make dev

# Run unit tests
make test

# Run integration tests (requires PostgreSQL, Redis, NATS)
make test-integration

# Run all tests with coverage report
make test-all

# Lint code
make lint

# Format code
make fmt

# Tidy dependencies
make tidy

# Run database migrations
make migrate

# Build Docker images
make docker

# Security scan
make security

# All checks (CI)
make check
```

---

## Testing Conventions

### Test Structure

```go
// Table-driven tests (Go convention)
func TestClassifyComplexity(t *testing.T) {
    tests := []struct {
        name     string
        task     *Task
        expected Complexity
    }{
        {name: "simple task", task: &Task{Type: "formatting"}, expected: ComplexitySimple},
        {name: "complex task", task: &Task{Type: "architecture", FilesChanged: 15}, expected: ComplexityComplex},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := router.classifyComplexity(tt.task)
            if got != tt.expected {
                t.Errorf("classifyComplexity() = %v, want %v", got, tt.expected)
            }
        })
    }
}
```

### Mocking

- Use `testify/mock` for interface mocking
- Create mock implementations in `*_test.go` files or dedicated `mock_*.go` files
- Always verify mock expectations with `mock.AssertExpectations(t)`

### Integration Tests

- Use `testcontainers-go` for PostgreSQL (pgvector/pgvector:pg16) and Redis
- Guard with `testing.Short()` skip
- Run migrations before testing repositories
- Clean up containers with `defer container.Terminate(ctx)`

### Coverage Requirements

| Package | Minimum Coverage |
|---------|-----------------|
| `agent/` | 85% |
| `llm/` | 80% |
| `tools/` | 85% |
| `memory/` | 80% |
| `skills/` | 80% |
| `api/` | 75% |
| `billing/` | 90% |

---

## Key Architecture Patterns

### Agent State Machine

```
pending → planning → executing → waiting_hitl → reviewing → completed
                     ↓                          ↓
                   failed ←────────────────── failed
                     ↓
                  cancelled
```

### LLM Routing

```
Task → Classify Complexity → Get Healthy Providers → Filter by Capabilities
    → Rank by Cost-Effectiveness → Select Primary + 3 Fallbacks
    → Check Budget → Execute with Failover → Cache Response
```

### Memory Recall (Cascading)

```
Query → Working Memory (in-session) → Semantic Cache (Redis)
    → Episodic Memory (PostgreSQL) → Semantic Memory (pgvector)
```

### Tool Execution (Sandboxed)

```
Tool Request → Check Permissions → Check Budget → Prepare gVisor Sandbox
    → Execute in Sandbox → Capture Output → Track Cost → Store in Memory
```

---

## LLM Provider Pricing (Reference)

| Provider | Model | Input (per 1M tokens) | Output (per 1M tokens) |
|----------|-------|----------------------|------------------------|
| Anthropic | Claude Opus 4 | $15 | $75 |
| Anthropic | Claude Sonnet 4 | $3 | $15 |
| Anthropic | Claude Haiku 3.5 | $0.80 | $4 |
| OpenAI | GPT-4.5 | $15 | $60 |
| OpenAI | GPT-4o | $2.50 | $10 |
| OpenAI | GPT-4o-mini | $0.15 | $0.60 |
| Google | Gemini 2.5 Pro | $1.25 | $10 |
| Google | Gemini 2.0 Flash | $0.075 | $0.30 |
| DeepSeek | R1 | $0.55 | $2.19 |

---

## Model Routing Rules

| Task Complexity | Best Model | Why |
|----------------|-----------|-----|
| Simple (formatting, renaming, docs) | GPT-4o-mini / Claude Haiku | Fast, cheap, exact |
| Moderate (bug fixes, features) | Claude Sonnet / GPT-4o | Balanced quality |
| Complex (architecture, multi-file) | Claude Opus / GPT-4.5 | Best reasoning |
| Critical (security, production) | Claude Opus + HITL | Must be correct |

---

## Common Workflows

### Adding a New Tool

1. Create `internal/tools/your_tool.go`
2. Implement the `Tool` interface (Name, Description, Parameters, Execute, RequiresHITL)
3. Register in `internal/tools/registry.go`
4. Add unit tests in `internal/tools/your_tool_test.go`
5. Add integration test with testcontainers if it touches external services

### Adding a New LLM Provider

1. Create `internal/llm/your_provider.go`
2. Implement the `LLMProvider` interface (Chat, Stream, HealthCheck, Name)
3. Add to provider registry in `internal/llm/router.go`
4. Add pricing to cost tracker
5. Add health check configuration
6. Write tests with mock HTTP server

### Adding a New API Endpoint

1. Define request/response types in `internal/api/handler.go`
2. Implement handler function
3. Register route in `internal/api/routes.go` (under auth middleware)
4. Add to OpenAPI spec in `docs/openapi.json`
5. Write handler test with `httptest`
6. Write integration test with testcontainers

### Adding a New Database Table

1. Create migration: `migrations/NNNNNN_create_table.up.sql` and `.down.sql`
2. Define Go struct in `internal/database/`
3. Implement repository (CRUD operations)
4. Add to test database reset script
5. Write integration test with testcontainers

---

## Environment Variables

```bash
# Database
DATABASE_URL=postgres://vigil:vigil@localhost:5432/vigilagent?sslmode=disable

# Redis
REDIS_URL=redis://localhost:6379

# NATS
NATS_URL=nats://localhost:4222

# LLM Providers
OPENAI_API_KEY=sk-...
ANTHROPIC_API_KEY=sk-ant-...
GOOGLE_API_KEY=AI...

# Auth
JWT_SECRET=your-secret-key
JWT_EXPIRY=1h

# Server
SERVER_ADDR=:8080
SERVER_READ_TIMEOUT=30s
SERVER_WRITE_TIMEOUT=60s

# Billing
STRIPE_SECRET_KEY=sk_test_...
STRIPE_WEBHOOK_SECRET=whsec_...

# S3 (for skill packages, code snapshots)
S3_BUCKET=vigilagent-storage
S3_REGION=us-east-1
S3_ENDPOINT=http://localhost:9000  # MinIO for dev
```

---

## Git Conventions

- **Branch naming:** `feat/short-description`, `fix/short-description`, `chore/short-description`
- **Commit messages:** Conventional Commits format — `feat(agent): add multi-step task execution`
- **PR requirements:** Tests pass, lint clean, 1 review approval, coverage not decreased
- **Main branch:** Always deployable; protected with required reviews

---

## Key Design Principles

1. **Interface-Driven** — Every external dependency is accessed through Go interfaces
2. **Fail-Safe by Default** — Agents never take destructive actions without HITL approval
3. **Cost-Aware** — Every LLM call tracks tokens and cost; agents have configurable budgets
4. **Observable** — Every action is traceable, every decision explainable, every token accounted for
5. **Horizontally Scalable** — Stateless API servers, stateful agents, distributed by design
6. **Graceful Degradation** — If primary LLM is unavailable, fall back to cached/local/queued retry

---

## Project Documents

| Document | Location | Purpose |
|----------|----------|---------|
| Master Project Prompt | `optimized/00-master-project-prompt.md` | Single source of truth |
| PRD | `optimized/01-PRD.md` | Product requirements |
| Architecture | `optimized/02-architecture.md` | System design |
| Database Schema | `optimized/03-schema.md` | Data model |
| API Contract | `optimized/04-api-contract.md` | API specification |
| Test Plan | `optimized/05-test-plan.md` | Testing strategy |
| LLM Strategy | `optimized/06-llm-strategy.md` | LLM routing & providers |
| Token Cost Optimization | `optimized/07-token-cost-optimization.md` | Cost optimization |
| Skills Marketplace | `optimized/08-skills-marketplace.md` | Marketplace design |
| Go Build Guide | `optimized/09-golang-build-guide.md` | Build patterns |
| Database Strategy | `optimized/10-database-strategy.md` | DB patterns & scaling |
| AI Continuation Prompt | `optimized/11-ai-continuation-prompt.md` | Session persistence |
| Comparison Analysis | `optimized/12-pdf-vs-md-comparison.md` | Doc comparison |
| Project Roadmap | `optimized/13-project-roadmap.md` | 6-month roadmap |
| Reconciliation Report | `optimized/14-reconciliation-report.md` | Cross-doc consistency analysis |
| Build Readiness Assessment | `optimized/15-build-readiness-assessment.md` | Pre-build checklist & verdict |
| **Risk Register** | `optimized/16-risk-register.md` | **48 risks with mitigations, owners, timeline** |

---

## Risk-Aware Development

When implementing features, always check the **Risk Register** (`optimized/16-risk-register.md`) for:

1. **P0 risks** (score 15-25) — must begin mitigation in Sprint 1; blocks Sprint 2
2. **Security risks** — 5 of 10 P0 risks are security-related; never skip HITL gates
3. **Compliance deadlines** — EU AI Act (Aug 2026) requires HITL audit trails
4. **Go-specific CVEs** — always use pgx, set CGO_ENABLED=0, pin actions to SHA-256
5. **Cost controls** — hard budget limits per user/project; circuit breaker on costs
6. **pgvector limits** — benchmark at 100K vectors before launch; plan for 10M threshold

Review cadence: Risk register reviewed bi-weekly. Security review monthly. Compliance review quarterly.

---

*This CLAUDE.md is the primary context file for AI-assisted development on VigilAgent. Keep it updated as the project evolves.*
