# VigilAgent — AI Context

## Overview

VigilAgent is an AI agent management platform built with Go. It provides real-time monitoring, analytics, and control for AI agents via REST APIs.

## Tech Stack

- **Language:** Go 1.26+
- **Router:** chi/v5
- **Config:** Viper (YAML + env vars)
- **Database:** PostgreSQL 16 + pgvector
- **Cache:** Redis 7
- **Queue:** NATS JetStream
- **Auth:** JWT + API keys (SHA-256 hashed)
- **CLI:** Cobra for command-line interface

## Project Layout

```
cmd/
  api/main.go           # API server entry point
  cli/                  # CLI tool (vigil binary)
    main.go             # Root command + subcommands
    init.go             # Project initialization
    chat.go             # Interactive chat sessions
    task.go             # Task management (create/list/get/cancel)
    skill.go            # Skill management (list/install)
    usage.go            # Usage/cost analytics
    config.go           # CLI configuration
    version.go          # Version info
  migrate/main.go       # Database migration runner CLI
internal/
  agent/                # Agent state machine and execution loop
    agent.go            # Agent orchestrator (plan → execute → observe)
    state.go            # State machine (8 states, transitions)
  api/contract/         # Request/response types + contract tests
  auth/                 # JWT + API key auth services
  config/               # Viper-based configuration + validation
  database/             # Postgres/Redis connections + migrations
  errors/               # Error types and helpers
  llm/                  # LLM provider layer
    provider.go         # Provider interface, ModelRouter, complexity scoring
    openai.go           # OpenAI adapter
    anthropic.go        # Anthropic adapter (HTTP API)
    health.go           # Health monitor (Healthy/Degraded/Unhealthy/Down)
    circuit_breaker.go  # Circuit breaker pattern (Closed/Open/HalfOpen)
  memory/               # Memory system
    episodic.go         # Episodic memory (past interactions)
    semantic.go         # Semantic memory (pgvector embeddings)
    working.go          # Working memory (per-session context)
    manager.go          # Multi-layer memory manager
  middleware/            # Rate limiter, API key auth middleware
  queue/                # NATS JetStream connection
  repository/           # Data access layer (Postgres)
    user.go             # User CRUD
    organization.go     # Organization + membership
    project.go          # Project CRUD
    agent.go            # Agent CRUD
    session.go          # Session management
    event.go            # Event ingestion + analytics
    apikey.go           # API key CRUD (SHA-256 hashed)
    task.go             # Task CRUD + pagination
    skill.go            # Skill CRUD + ratings + install
    alert.go            # Alert CRUD
  router/               # Chi router, all HTTP handlers
    router.go           # Route setup, auth middleware, core handlers
    tasks.go            # Task handlers (create, get, list, cancel, stream SSE)
    skills_handlers.go  # Skill handlers (8 real implementations)
    alerts_handlers.go  # Alert handlers (5 real implementations)
    billing_handlers.go # Billing handlers (placeholder for Stripe)
    admin_handlers.go   # Admin handlers (placeholder)
    memory_handlers.go  # Memory search + create handlers
    apikeys.go          # API key handlers (create, list, delete)
  server/               # HTTP server wrapper
  skills/               # Skills system
    registry.go         # Skill registry
  telemetry/            # Prometheus metrics + OpenTelemetry
  tools/                # Tool system
    tool.go             # Tool interface, ToolRegistry
    file.go             # File operations (read, write, edit, list)
    terminal.go         # Command execution
    search.go           # Code search (ripgrep)
pkg/
  response/             # JSON response helpers
migrations/             # SQL migration files (numbered up/down)
configs/                # YAML config files
.github/workflows/      # CI pipeline (lint, unit, integration tests)
```

## Conventions

- **Module path:** github.com/vigilagent/vigilagent
- **Go standard layout:** Use internal/ for private packages, pkg/ for public
- **Error handling:** Return error as last value; use pkg/response for HTTP errors
- **Config:** All config via VIGILAGENT_* env vars or configs/config.yaml
- **Imports:** Group: stdlib, external, internal. Use goimports ordering
- **Naming:** Follow Go conventions — no underscores in package names
- **Testing:** Place tests alongside source files (_test.go)
- **Linting:** Pass golangci-lint with .golangci.yml config
- **Build:** CGO_ENABLED=0 for static binaries

## Database

- PostgreSQL 16 with pgvector extension for vector similarity search
- Migrations use plain SQL in migrations/ (numbered, up/down)
- Connection pooling via database/sql with configurable max conns
- Vector indexes use IVFFlat (requires data before index creation)

## Auth Model

- **JWT:** Standard Bearer token auth for session-based access
- **API Keys:** SHA-256 hashed keys with prefix (va_*), DB-backed lookup
- Password policy: minimum 12 characters
- Production mode rejects default JWT secret

## Agent System

- **State Machine:** 8 states (Pending → Planning → Executing → Reviewing → Completed/Failed/Cancelled)
- **Task Execution:** Plan → Execute → Observe → Decide loop (max 20 iterations)
- **HITL:** Human-in-the-loop checkpoints for sensitive operations
- **Tool Registry:** Extensible tool system (file, terminal, search operations)

## LLM Routing

- **Complexity Classification:** 5-factor scoring (task type, file count, reasoning, novelty, security)
- **Model Selection:** Maps complexity to optimal model (simple → gpt-4o-mini, complex → claude-opus-4)
- **Failover:** Automatic provider fallback with circuit breaker
- **Health Monitoring:** Real-time provider health tracking

## Memory System

- **Episodic:** Past interactions and task history
- **Semantic:** Codebase patterns via pgvector embeddings
- **Working:** Per-session context and current task state
- **Manager:** Multi-layer recall with cascading retrieval

## Running Locally

```bash
# Start infrastructure
docker-compose -f docker-compose.dev.yml up -d

# Run the server
go run ./cmd/api

# Run migrations
go run ./cmd/migrate up

# Run tests (short mode, no DB required)
go test -short ./...

# Run tests with race detector
go test -short -race ./...

# Lint
golangci-lint run

# Vet
go vet ./...
```

## Build Commands

```bash
make build          # Build both binaries
make test-short     # Run short tests
make test-race      # Run tests with race detector
make test-cover     # Run tests with coverage report
make lint           # Run golangci-lint
make vet            # Run go vet
make tidy           # Run go mod tidy
make check          # Run all checks (fmt, vet, test, lint)
```

## CLI Usage

```bash
# Initialize project
vigil init my-project

# Start interactive chat
vigil chat --token YOUR_TOKEN

# Create a task
vigil task create "Fix the authentication bug" --project PROJECT_ID

# List tasks
vigil task list --project PROJECT_ID

# Install a skill
vigil skill install lint --token YOUR_TOKEN

# View usage
vigil usage --token YOUR_TOKEN
```

## Key Patterns

- All protected handlers extract auth claims from context
- CRUD handlers follow: validate input → check permissions → execute → respond
- Organization membership checked before resource access
- API keys: create returns plaintext once, list never returns hashes, delete revokes
- SSE streaming for real-time task updates with proper goroutine cleanup
- Agent execution runs in background goroutines with fresh context

## Sprint Status

- **Sprint 1 (Weeks 1-4):** ✅ Complete — Infrastructure, auth, CRUD handlers, CI
- **Sprint 2 (Weeks 5-8):** ✅ Complete — Agent engine, LLM providers, tool system, task API
- **Sprint 3 (Weeks 9-12):** ✅ Complete — Memory system, skills, remaining API handlers
- **Sprint 4 (Weeks 13-16):** ✅ Complete — CLI tool, tests, verification

## Test Coverage

11 packages with passing tests:
- internal/agent (state machine transitions)
- internal/api/contract (request/response validation)
- internal/auth (JWT, API keys, password hashing)
- internal/config (validation, DSN, Address)
- internal/database (migration version parsing)
- internal/errors (error types)
- internal/llm (price table, complexity classification, routing)
- internal/repository (organization tests)
- internal/router (handler tests)
- internal/tools (tool registry)
- pkg/response (JSON response helpers)
