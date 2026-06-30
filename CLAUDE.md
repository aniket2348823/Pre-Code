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
- **Auth:** JWT (planned)

## Project Layout

```
main.go                 # Entry point
internal/
  config/               # Viper-based configuration
  server/               # HTTP server wrapper
  router/               # Chi router with all routes
pkg/response/           # JSON response helpers
migrations/             # SQL migration files
configs/                # YAML config files
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

## Database

- PostgreSQL 16 with pgvector extension for vector similarity search
- Migrations use plain SQL in migrations/ (numbered, up/down)
- Connection pooling via database/sql with configurable max conns
- Vector indexes use IVFFlat (requires data before index creation)

## Running Locally

```bash
# Start infrastructure
docker-compose -f docker-compose.dev.yml up -d

# Run the server
go run main.go

# Run tests
go test ./...

# Lint
golangci-lint run
```

## Key Patterns

- All handlers currently return 501 Not Implemented — ready for Phase 2
- Middleware stack: RequestID, RealIP, Logger, Recoverer, Timeout, CORS
- Auth middleware and admin middleware are stubs (pass-through)
- Rate limiting middleware on events endpoints is a stub
