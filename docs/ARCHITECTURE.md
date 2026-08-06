# Architecture

> Deep-dive into VigilAgent's system architecture, data flow, and design decisions.

---

## Table of Contents

- [System Overview](#system-overview)
- [High-Level Architecture](#high-level-architecture)
- [Request Lifecycle](#request-lifecycle)
- [Binary Architecture](#binary-architecture)
- [Domain Handler Design](#domain-handler-design)
- [LLM Routing Engine](#llm-routing-engine)
- [Agent Execution Engine](#agent-execution-engine)
- [Parallel Analysis Pipeline](#parallel-analysis-pipeline)
- [Memory Architecture](#memory-architecture)
- [Queue & Async Processing](#queue--async-processing)
- [Data Layer](#data-layer)
- [Middleware Stack](#middleware-stack)
- [Observability Architecture](#observability-architecture)
- [Security Architecture](#security-architecture)
- [Design Principles](#design-principles)

---

## System Overview

VigilAgent is a **Go monorepo** that compiles into **8 independent binaries**, each serving a distinct operational concern. The core binary (`vigil-api`) hosts all business logic behind a `chi/v5` HTTP router, backed by PostgreSQL 16 (with pgvector for embeddings), Redis 7 (caching and rate limiting), and NATS JetStream (async job processing).

The system follows a **domain-driven design** where the `internal/` directory contains 67 focused packages, each owning a single business capability. Cross-cutting concerns (auth, tracing, compression) are handled by a layered middleware stack.

---

## High-Level Architecture

```
                    ┌──────────────┐
                    │   Clients    │
                    │ VS Code, CLI │
                    │ Browser, SDK │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐
                    │  API Gateway │  ← vigil-api (port 8080)
                    │   chi/v5     │
                    └──────┬───────┘
                           │
              ┌────────────┼────────────┐
              │            │            │
     ┌────────▼──────┐ ┌──▼────────┐ ┌─▼──────────┐
     │  Middleware    │ │  Domain   │ │  Background │
     │  Stack (15+)  │ │  Handlers │ │  Workers    │
     │               │ │  (7 areas)│ │  (NATS)     │
     └────────┬──────┘ └──┬────────┘ └─┬──────────┘
              │            │            │
     ┌────────▼────────────▼────────────▼──────────┐
     │              Core Services                   │
     ├──────────────┬──────────────┬────────────────┤
     │  LLM Router  │ Agent Engine │ Scanner Engine │
     │  (9 provs)   │ (8-state SM) │ (3 analyzers) │
     ├──────────────┼──────────────┼────────────────┤
     │  Skills Eng  │  Cost Intel  │   Memory Mgr   │
     │  (RAG+Exec)  │ (Forecast)   │  (3-tier)      │
     └──────────────┴──────────────┴────────────────┘
              │            │            │
     ┌────────▼────────────▼────────────▼──────────┐
     │            Infrastructure Layer              │
     ├──────────────┬──────────────┬────────────────┤
     │ PostgreSQL   │   Redis 7   │     NATS       │
     │ 16+pgvector  │   Cache     │   JetStream    │
     └──────────────┴──────────────┴────────────────┘
```

---

## Request Lifecycle

Every inbound HTTP request passes through the following pipeline:

```
Client Request
    │
    ▼
┌─────────────────────────────────────────────────────┐
│ 1. Request ID Generation (X-Request-ID)             │
│ 2. Structured Logging (slog context injection)      │
│ 3. OpenTelemetry Tracing (span creation)            │
│ 4. Gzip/Brotli Compression                         │
│ 5. CORS Handling (preflight + headers)              │
│ 6. Security Headers (HSTS, CSP, X-Frame-Options)   │
│ 7. IP Filtering (allowlist/denylist check)          │
│ 8. Rate Limiting (Redis token bucket)               │
│ 9. Body Size Limiting                               │
│ 10. JWT / API Key Authentication                    │
│ 11. RBAC Permission Check                           │
│ 12. Audit Logging (write to audit_logs table)       │
│ 13. Idempotency Check (Redis lock for POST/PUT)     │
└──────────────────────┬──────────────────────────────┘
                       │
                       ▼
              Domain Handler Logic
                       │
                       ▼
              Response (JSON + headers)
```

---

## Binary Architecture

VigilAgent compiles into 8 binaries from the `cmd/` directory. Each binary is a self-contained executable with its own `main.go`:

### vigil-api (`cmd/api`)
The primary application server. Initializes the full dependency injection container (`internal/server`), mounts all routes, and starts the HTTP server with graceful shutdown on SIGINT/SIGTERM.

**Initialization sequence:**
1. Load Viper configuration (YAML + env vars)
2. Initialize structured logging (JSON/text based on env)
3. Connect to PostgreSQL with pgx connection pool
4. Connect to Redis
5. Connect to NATS JetStream
6. Initialize OpenTelemetry tracer + Prometheus registry
7. Build dependency container (`server.New()`)
8. Register all HTTP routes with middleware
9. Start HTTP listener on `:8080`
10. Block on OS signal, then graceful shutdown with 15s timeout

### vigil-proxy (`cmd/proxy`)
A high-throughput LLM reverse proxy operating on port 9090. Designed for direct LLM access without the full API overhead. Routes completion requests to the appropriate provider based on model selection. Runs background health checks on all configured providers. Supports TLS termination and SSE streaming with no global write timeout.

### vigil-mcp (`cmd/mcp`)
A stdio-based MCP (Model Context Protocol) server that speaks JSON-RPC. Designed for integration with AI-powered IDEs (Cursor, Cline, VS Code). Reads `VIGILAGENT_API_URL` and `VIGILAGENT_API_KEY` from environment. Logs are routed to `stderr` to keep `stdout` reserved for MCP protocol messages.

### vigil-migrate (`cmd/migrate`)
Standalone database migration runner. Supports `up`, `version`, and `status` subcommands. Uses embedded `.sql` migration files from the `migrations/` directory.

### vigil (`cmd/cli`)
Cobra-based CLI client for managing agents, tasks, skills, and configuration. Communicates with the vigil-api REST API.

### healthcheck (`cmd/healthcheck`)
Minimal binary that performs a TCP dial against the API server. Returns exit code 0 for healthy, 1 for unhealthy. Used as the Docker `HEALTHCHECK` command without leaking internal error details.

### loadtest (`cmd/loadtest`)
Custom concurrent HTTP load tester. Configurable worker pool (default 1,000 workers, up to 30,000 requests). Reports throughput (RPS) and latency percentiles (P50, P95, P99). Uses HTTP keep-alive for connection reuse.

### bench (`cmd/bench`)
LLM cost optimization benchmark harness. Operates in two modes:
- **Record mode**: Captures live model API calls into fixture files
- **Replay mode**: Replays captured workloads against the cost-based router to measure savings vs. baseline models

---

## Domain Handler Design

The `internal/router/` package implements a domain-driven handler architecture. Each handler file owns a specific business domain and registers its own routes:

### Auth Domain (`auth_handlers.go`)
Manages the complete authentication lifecycle:
- User registration with email verification
- Login/logout with JWT token issuance
- Password reset flow (forgot → reset)
- Token refresh and session management
- Profile updates and password changes

### System Domain (`system_handlers.go`)
Platform operations and administration:
- Health and readiness probes
- Swagger/OpenAPI documentation serving
- Prometheus metrics exposure
- Admin statistics and user management
- Audit log retention cleanup

### Organization Domain (`org_handlers.go`)
Multi-tenant organization management:
- Organization CRUD operations
- Project CRUD within organizations
- Agent CRUD within projects
- Session CRUD within agents
- Team member invitation workflow

### Skills Domain (`skills_handlers.go`)
Skills marketplace operations:
- Skill listing, creation, updating, deletion
- Skill rating and review system
- Skill installation and extraction
- RAG-powered semantic skill search

### Scan Domain (`scan_handlers.go`, `review_handler.go`)
Code analysis and security scanning:
- Static analysis scanning
- Full code review pipeline
- Requirements validation
- Schema verification
- Compliance checking (SOC2, HIPAA, PCI-DSS)
- Knowledge base ingestion
- Confidence scoring
- Attack graph generation
- Audit tracing

### Additional Domains
- **Tasks** (`tasks.go`): Task submission, cancellation, batch operations, SSE streaming
- **HITL** (`hitl_handlers.go`): Human-in-the-Loop checkpoint management
- **Memory** (`memory_handlers.go`): Episodic and semantic memory operations
- **Alerts** (`alerts_handlers.go`): Alert CRUD and notification management
- **Webhooks** (`webhook_handlers.go`): Webhook registration, delivery tracking, replay
- **Batch** (`batch.go`): Batch API operations
- **Export** (`export_handlers.go`): Data export and import
- **WebSocket** (`websocket.go`): Real-time bidirectional communication

---

## LLM Routing Engine

The `internal/llm/` package implements a production-grade multi-provider routing system:

```
                   ┌──────────────────┐
                   │  Provider Engine │
                   │  (provider.go)   │
                   └────────┬─────────┘
                            │
              ┌─────────────┼─────────────┐
              │             │             │
     ┌────────▼───┐  ┌─────▼─────┐  ┌───▼────────┐
     │  Circuit   │  │  Health   │  │  Response  │
     │  Breaker   │  │  Monitor  │  │  Cache     │
     └────────┬───┘  └─────┬─────┘  └───┬────────┘
              │             │             │
     ┌────────▼─────────────▼─────────────▼────────┐
     │              Provider Drivers                │
     ├─────────┬──────────┬──────────┬─────────────┤
     │ OpenAI  │Anthropic │ Gemini   │   Groq      │
     │ Mistral │ DeepSeek │ Cohere   │  NVIDIA NIM │
     │         │          │OpenRouter│             │
     └─────────┴──────────┴──────────┴─────────────┘
```

### Provider Engine (`provider.go`)
Central routing table that selects the optimal provider and model based on:
- Model availability and health status
- Cost optimization (cheapest model meeting requirements)
- Fallback chain (automatic failover to next healthy provider)
- Load balancing across providers

### Circuit Breaker (`circuit_breaker.go`)
Per-provider circuit breaker that trips after consecutive failures, preventing cascading failures. Automatically resets after a configurable cool-down period.

### Health Monitor (`health.go`)
Background goroutine that periodically checks each provider's health status. Updates the routing table to exclude unhealthy providers from request routing.

### Response Cache (`cache.go`)
Content-addressed cache layer that stores LLM responses. Identical prompts hitting the same model return cached results, reducing cost and latency.

### Price Database (`models.go`)
Comprehensive token pricing table for all supported models. Tracks input/output/cached token costs per 1K tokens. Used by the cost intelligence engine for real-time cost tracking and optimization recommendations.

---

## Agent Execution Engine

The `internal/agent/` package implements an autonomous task execution system:

```
┌──────────┐    ┌──────────┐    ┌───────────┐    ┌────────────┐
│ Pending  │───▶│ Planning │───▶│ Executing │───▶│ Reviewing  │
└──────────┘    └──────────┘    └─────┬─────┘    └──────┬─────┘
                                      │                  │
                                      ▼                  ▼
                               ┌─────────────┐   ┌─────────────┐
                               │WaitingHITL  │   │ Completed   │
                               └──────┬──────┘   └─────────────┘
                                      │                  │
                              Approved│          ┌───────┘
                                      ▼          │
                               ┌─────────────┐   │  ┌──────────┐
                               │ Executing   │◀──┘  │  Failed  │
                               └─────────────┘      └──────────┘
```

### State Machine (8 States, 10 Events)

| State          | Description                                             |
|----------------|---------------------------------------------------------|
| `Pending`      | Task created, awaiting processing                       |
| `Planning`     | LLM generating execution plan                           |
| `Executing`    | Agent executing plan steps                              |
| `WaitingHITL`  | Blocked on human approval                               |
| `Reviewing`    | Agent self-reviewing results                            |
| `Completed`    | Task finished successfully                              |
| `Failed`       | Task terminated due to error                            |
| `Cancelled`    | Task cancelled by user or system                        |

### Execution Loop
The agent runs an iterative **Plan → Execute → Observe → Review** loop with a maximum of 20 iterations per task. Each iteration:
1. **Plan**: LLM generates next steps based on current context
2. **Execute**: Agent executes the planned steps using registered tools
3. **Observe**: Results are collected and added to working memory
4. **Review**: LLM evaluates whether the task is complete

If a step requires HITL approval, the state machine transitions to `WaitingHITL` and blocks until a human decision is received via the `/hitl/decide` endpoint.

### Token & Cost Tracking
Every agent execution tracks:
- `InputTokens`: Total input tokens consumed across all LLM calls
- `OutputTokens`: Total output tokens generated
- `ToolTokens`: Tokens consumed by tool execution prompts
- Accumulated cost in USD based on the model pricing table

---

## Parallel Analysis Pipeline

The core differentiator — every LLM response passes through two parallel analysis engines:

```
                    Raw LLM Output
                         │
              ┌──────────┴──────────┐
              │                     │
     ┌────────▼────────┐  ┌────────▼────────┐
     │  Deterministic  │  │  LLM Critique   │
     │    Engine       │  │    Engine        │
     │                 │  │                  │
     │ • Regex SAST    │  │ • Self-review    │
     │ • Semgrep       │  │ • Optimization   │
     │ • Bandit        │  │ • Logic check    │
     │ • Credential    │  │ • Best practices │
     │   scanning      │  │                  │
     │ • Compliance    │  │                  │
     │   (SOC2/HIPAA)  │  │                  │
     └────────┬────────┘  └────────┬────────┘
              │                     │
              └──────────┬──────────┘
                         │
                  Aggregated Result
                  (Findings + Score)
```

Both engines execute concurrently using Go's goroutines and `sync.WaitGroup`. Results are merged into a unified response with:
- Security findings with severity levels
- Confidence scores
- Optimization suggestions
- Compliance status

---

## Memory Architecture

The `internal/memory/` package implements a three-tier memory system:

| Tier              | Storage              | Purpose                                    | Retrieval Method    |
|-------------------|----------------------|--------------------------------------------|---------------------|
| **Episodic**      | PostgreSQL           | Conversation history and session context   | Chronological query |
| **Semantic**      | PostgreSQL + pgvector| Long-term knowledge as vector embeddings   | Cosine similarity   |
| **Working**       | In-memory            | Short-term task context within sessions    | Direct access       |

### Vector Embedding Storage
The `memory_entries` table includes a pgvector `embedding` column for storing dense vector representations of text. The `POST /api/v1/memory/search` endpoint performs cosine similarity search across stored embeddings to retrieve semantically relevant context.

---

## Queue & Async Processing

The `internal/queue/` package implements asynchronous task processing via NATS JetStream:

```
┌──────────┐     ┌─────────────┐     ┌──────────────┐
│ API      │────▶│ NATS Stream │────▶│ Worker Pool  │
│ Handler  │     │ vigilagent  │     │ (consumers)  │
│ POST     │     │             │     │              │
│ /tasks   │     │ Subject:    │     │ Process task │
└──────────┘     │ vigilagent  │     │ Run agent    │
                 │ .tasks      │     │ Stream SSE   │
                 │ .execute    │     └──────────────┘
                 └─────────────┘
```

- **Stream**: `vigilagent`
- **Subject**: `vigilagent.tasks.execute`
- **ACK Policy**: Explicit (worker must ACK after successful processing)
- **Retry**: Up to 3 retries with dead-letter support
- **Payload**: `TaskID`, `ProjectID`, `UserID`, `Prompt`, `MaxTokens`, `MaxIterations`, `Priority`

---

## Data Layer

### PostgreSQL Schema

The migration `000001_init_schema.up.sql` creates the following tables:

| Table               | Purpose                                    |
|---------------------|--------------------------------------------|
| `users`             | User accounts with bcrypt password hashes  |
| `organizations`     | Multi-tenant organization records          |
| `org_members`       | Organization membership with roles         |
| `projects`          | Projects within organizations              |
| `agents`            | AI agent configurations                    |
| `agent_sessions`    | Agent conversation sessions                |
| `tasks`             | Submitted task records                     |
| `skills`            | Skill marketplace entries                  |
| `skill_ratings`     | User ratings for skills                    |
| `installed_skills`  | Skills installed per organization          |
| `memory_entries`    | Memory with pgvector embeddings            |
| `audit_logs`        | System audit trail                         |
| `alerts`            | Alert definitions and triggers             |
| `invoices`          | Billing invoice records                    |
| `api_keys`          | SHA-256 hashed API keys                    |
| `webhooks`          | Webhook endpoint registrations             |
| `webhook_deliveries`| Webhook delivery attempts and status       |

### Repository Pattern
The `internal/repository/` package implements the data access layer using the repository pattern. Each entity has a dedicated repository with CRUD operations, soft-delete support, and PostgreSQL-specific query optimizations.

### Connection Management
The `internal/database/` package manages connection pools:
- **PostgreSQL**: `pgxpool` with configurable max open/idle connections, lifetime, statement timeout, and slow query detection
- **Redis**: `go-redis` client for caching and rate limiting
- **Observability**: Query tracing and latency metrics integrated via OpenTelemetry

---

## Middleware Stack

The `internal/middleware/` package provides 15+ HTTP middlewares wired in a specific order by `middleware_wiring.go`:

| Middleware        | Package              | Purpose                                         |
|-------------------|----------------------|-------------------------------------------------|
| Request ID        | `requestid`          | Generate and propagate correlation IDs          |
| Tracing           | `tracing`            | OpenTelemetry span creation                     |
| Compression       | `compression`        | Gzip/Brotli response compression                |
| CORS              | `cors`               | Cross-origin request handling                   |
| Security Headers  | `security`           | HSTS, CSP, X-Frame-Options                     |
| IP Filter         | `ipfilter`           | CIDR-based IP allowlist/denylist                |
| Rate Limiting     | `ratelimit`          | Redis token bucket + sliding window            |
| Body Size Limit   | `middleware`         | Prevent oversized request bodies                |
| Authentication    | `auth`               | JWT validation and API key verification         |
| RBAC              | `scopes`             | Role-based permission enforcement               |
| Audit             | `audit`              | Write request metadata to audit log             |
| Idempotency       | `idempotency`        | Redis-backed request deduplication              |
| HITL              | `hitl`               | Human-in-the-Loop checkpoint injection          |
| Lockout           | `lockout`            | Progressive account lockout                     |
| Blacklist         | `blacklist`          | Token and session blacklisting                  |
| Caching           | `caching`            | HTTP response caching with ETag support         |
| Health Check      | `healthcheck`        | Bypass auth for health probe endpoints          |

---

## Observability Architecture

```
┌─────────────────────────────────────────────┐
│              Application Code               │
│                                             │
│  slog.InfoContext(ctx, "msg", attrs...)     │
│  otel.Tracer("name").Start(ctx, "span")    │
│  telemetry.HTTPRequestsTotal.Inc()         │
└──────────┬──────────────┬───────────────────┘
           │              │
    ┌──────▼──────┐ ┌─────▼──────┐
    │ Structured  │ │ OTel SDK   │
    │ JSON Logs   │ │ Traces +   │
    │ (stdout)    │ │ Metrics    │
    └──────┬──────┘ └─────┬──────┘
           │              │
    ┌──────▼──────┐ ┌─────▼──────┐
    │ Log         │ │ Prometheus │
    │ Aggregator  │ │ /metrics   │
    │ (ELK/Loki) │ │ Grafana    │
    └─────────────┘ └────────────┘
```

### Pre-Registered Prometheus Metrics

| Metric                          | Type      | Description                           |
|---------------------------------|-----------|---------------------------------------|
| `task_processing_seconds`       | Histogram | Agent task execution duration         |
| `tokens_consumed_total`         | Counter   | Total LLM tokens consumed            |
| `active_sessions`               | Gauge     | Currently active agent sessions       |
| `http_requests_total`           | Counter   | Total HTTP requests by status/method  |
| `http_request_duration_seconds` | Histogram | HTTP request latency distribution     |
| `auth_failures_total`           | Counter   | Failed authentication attempts        |
| `hitl_checkpoints_total`        | Counter   | HITL checkpoints triggered            |
| `nats_queue_depth`              | Gauge     | NATS JetStream pending messages       |
| `slow_query_duration`           | Histogram | Database slow query latency           |
| `llm_tokens`                    | Counter   | LLM tokens by provider/model         |
| `dropped_spans`                 | Counter   | OpenTelemetry dropped spans           |
| `verification_confidence`       | Histogram | Code verification confidence scores   |

---

## Security Architecture

See [SECURITY.md](../SECURITY.md) for the complete security documentation. Key architectural decisions:

1. **Defense in Depth**: Every request passes through 13+ middleware layers before reaching business logic
2. **Zero Trust Authentication**: JWT tokens support IP and User-Agent binding to prevent token theft
3. **Secrets at Rest**: API keys are stored as SHA-256 hashes, never in plaintext
4. **Encryption**: AES-256-GCM with PBKDF2 (100K iterations) for sensitive data
5. **Credential Detection**: Automated scanning for leaked AWS keys, GitHub PATs, Slack tokens, private keys, and more
6. **Compliance Engine**: Built-in rule engine for SOC2, HIPAA, and PCI-DSS validation

---

## Design Principles

1. **Domain-Driven Boundaries**: Each `internal/` package owns exactly one business capability. No package exceeds a single responsibility.

2. **Interface-Driven Dependencies**: Core services depend on interfaces, not concrete implementations. This enables testing with mock implementations and future swappability.

3. **Fail-Safe Defaults**: Rate limiters, circuit breakers, and health checks are enabled by default. The system degrades gracefully rather than failing catastrophically.

4. **Observability First**: Every meaningful operation emits structured logs, traces, and metrics. The system is designed to be debuggable in production.

5. **Configuration as Code**: All configuration flows through Viper with environment variable overrides. No hardcoded secrets or magic values.

6. **Concurrency Safety**: All shared state is protected by `sync.RWMutex` or channel-based coordination. The test suite includes race detector runs (`make test-race`).
