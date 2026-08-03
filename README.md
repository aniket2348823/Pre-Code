# VigilAgent

> AI Agent Management Platform — Real-time monitoring, analytics, and control for AI agents via REST APIs.

## Overview

VigilAgent is a production-grade Go backend that provides:

- **Multi-LLM Routing** — Smart routing across 9+ providers (OpenAI, Anthropic, Gemini, Groq, Mistral, Cohere, NVIDIA, OpenRouter, DeepSeek)
- **Agent Execution Engine** — State machine-based task execution with HITL checkpoints
- **Deterministic Analysis** — Static analysis, security scanning, compliance checking
- **Memory System** — Episodic, semantic (pgvector), and working memory
- **Skills Marketplace** — Install, execute, and sandbox reusable skills
- **Cost Intelligence** — Real-time cost tracking, forecasting, and anomaly detection
- **MCP Server** — Model Context Protocol for Cursor/Cline integration

## Tech Stack

- **Language:** Go 1.26+
- **Router:** chi/v5
- **Database:** PostgreSQL 16 + pgvector
- **Cache:** Redis 7
- **Queue:** NATS JetStream
- **Auth:** JWT + API keys (SHA-256 hashed)
- **Telemetry:** OpenTelemetry + Prometheus

## Quick Start

```bash
# Start infrastructure
docker-compose -f docker-compose.dev.yml up -d

# Run migrations
make migrate

# Start the server
make run

# Run tests
make test-short
```

## API Endpoints

### Public
- `GET /api/v1/health` — Liveness probe
- `GET /api/v1/ready` — Readiness probe
- `GET /api/v1/metrics` — Prometheus metrics
- `POST /api/v1/auth/register` — Create account
- `POST /api/v1/auth/login` — Login

### Protected (JWT)
- `GET /api/v1/users/me` — Current user profile
- `GET /api/v1/users/me/sessions` — User sessions
- `POST /api/v1/tasks` — Create task
- `GET /api/v1/tasks/{id}` — Get task
- `POST /api/v1/scan` — Static analysis scan
- `POST /api/v1/review` — Full code review
- `GET /api/v1/analytics/cost` — Cost analytics
- `GET /api/v1/dashboard/overview` — Dashboard overview

### Admin
- `GET /api/v1/admin/stats` — Platform statistics
- `GET /api/v1/audit/logs` — Audit log viewer

## CLI

```bash
# Initialize project
vigil init my-project

# Start interactive chat
vigil chat --token YOUR_TOKEN

# Create a task
vigil task create "Fix the authentication bug" --project PROJECT_ID

# Install a skill
vigil skill install lint --token YOUR_TOKEN
```

## Configuration

All configuration via environment variables with `VIGILAGENT_` prefix:

```bash
VIGILAGENT_DB_HOST=localhost
VIGILAGENT_DB_PORT=5432
VIGILAGENT_AUTH_JWT_SECRET=your-secret-here
VIGILAGENT_LLM_OPENAI_KEY=sk-...
VIGILAGENT_SENDGRID_API_KEY=SG....
```

See `configs/config.yaml` for all available options.

## Development

```bash
make build          # Build binaries
make test-short     # Run tests
make lint           # Run linter
make check          # fmt + vet + test
```

## License

MIT
