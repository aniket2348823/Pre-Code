<!-- CI/CD Status Badges -->
<p align="center">
  <a href="https://github.com/aniket2348823/Pre-Code/actions/workflows/deploy.yml"><img src="https://img.shields.io/github/actions/workflow/status/aniket2348823/Pre-Code/deploy.yml?branch=main&style=for-the-badge&label=CI%2FCD&logo=githubactions&logoColor=white" alt="CI/CD"></a>
  <a href="https://github.com/aniket2348823/Pre-Code/actions/workflows/sast.yml"><img src="https://img.shields.io/github/actions/workflow/status/aniket2348823/Pre-Code/sast.yml?branch=main&style=for-the-badge&label=SAST&logo=githubactions&logoColor=white" alt="SAST"></a>
  <a href="https://github.com/aniket2348823/Pre-Code"><img src="https://img.shields.io/badge/VERSION-2.0.0-brightgreen?style=for-the-badge" alt="Version"></a>
  <a href="#license"><img src="https://img.shields.io/badge/LICENSE-MIT-blue?style=for-the-badge" alt="License"></a>
  <a href="https://github.com/aniket2348823/Pre-Code"><img src="https://img.shields.io/badge/MAINTAINED-YES-success?style=for-the-badge" alt="Maintained"></a>
</p>

<!-- Tech Stack Badges -->
<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/GO-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go"></a>
  <a href="https://go-chi.io"><img src="https://img.shields.io/badge/CHI-v5-purple?style=for-the-badge" alt="Chi"></a>
  <a href="https://www.postgresql.org"><img src="https://img.shields.io/badge/POSTGRES-16-336791?style=for-the-badge&logo=postgresql&logoColor=white" alt="PostgreSQL"></a>
  <a href="https://redis.io"><img src="https://img.shields.io/badge/REDIS-7-DC382D?style=for-the-badge&logo=redis&logoColor=white" alt="Redis"></a>
  <a href="https://nats.io"><img src="https://img.shields.io/badge/NATS-JetStream-27AAE1?style=for-the-badge&logo=natsdotio&logoColor=white" alt="NATS"></a>
  <a href="https://www.docker.com"><img src="https://img.shields.io/badge/DOCKER-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker"></a>
  <a href="https://kubernetes.io"><img src="https://img.shields.io/badge/K8S-Helm-326CE5?style=for-the-badge&logo=kubernetes&logoColor=white" alt="Kubernetes"></a>
</p>

<!-- Project Highlights -->
<p align="center">
  <img src="https://img.shields.io/badge/LLM_PROVIDERS-9-FF6F00?style=for-the-badge&logo=openai&logoColor=white" alt="LLM Providers">
  <img src="https://img.shields.io/badge/API_ENDPOINTS-90+-8B5CF6?style=for-the-badge&logo=fastapi&logoColor=white" alt="API Endpoints">
  <img src="https://img.shields.io/badge/TESTS-133_FILES-4CAF50?style=for-the-badge&logo=testcafe&logoColor=white" alt="Tests">
  <img src="https://img.shields.io/badge/PACKAGES-67-E91E63?style=for-the-badge&logo=go&logoColor=white" alt="Packages">
  <img src="https://img.shields.io/badge/PROMETHEUS-Metrics-E6522C?style=for-the-badge&logo=prometheus&logoColor=white" alt="Prometheus">
  <img src="https://img.shields.io/badge/OpenTelemetry-Tracing-F5A800?style=for-the-badge&logo=opentelemetry&logoColor=white" alt="OpenTelemetry">
</p>

# VigilAgent — Enterprise AI Agent Orchestration & Code Intelligence Platform

---

> A multi-LLM routing and autonomous agent execution platform for deterministic code analysis, vulnerability scanning, and AI-powered code review — driven by 9 LLM providers, a parallel analysis pipeline, Human-in-the-Loop governance, MCP server integration, and a real-time streaming dashboard.

---

## 🔗 Table of Contents

- [Overview](#overview)
- [Key Capabilities](#key-capabilities)
- [Architecture](#architecture)
- [Tech Stack](#tech-stack)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Infrastructure Setup](#infrastructure-setup)
  - [Database Migration](#database-migration)
  - [Running the Server](#running-the-server)
- [Project Structure](#project-structure)
- [Binaries](#binaries)
- [LLM Provider Support](#llm-provider-support)
- [API Overview](#api-overview)
- [MCP Server Integration](#mcp-server-integration)
- [VS Code Extension](#vs-code-extension)
- [Browser Extension](#browser-extension)
- [Configuration](#configuration)
- [Testing](#testing)
- [Deployment](#deployment)
- [Security](#security)
- [Contributing](#contributing)
- [License](#license)

---

## Overview

**VigilAgent** is a production-ready platform that sits between developers and Large Language Models. Instead of blindly trusting raw LLM output, VigilAgent intercepts every response and runs it through a **parallel analysis pipeline** — a deterministic static analysis engine and an LLM-powered critique engine operating simultaneously — to catch hallucinations, security vulnerabilities, and logical errors before code ever reaches the developer.

The platform exposes a RESTful API, an MCP (Model Context Protocol) server for IDE integration, a VS Code extension, a browser extension, and a CLI — making it accessible from any developer workflow.

---

## Key Capabilities

### Multi-LLM Routing Engine
Route requests across **9 LLM providers** with automatic failover, circuit breaking, health monitoring, and cost-optimized model selection:
- OpenAI (GPT-4o, GPT-4o-mini)
- Anthropic (Claude Sonnet 4, Claude Opus 4)
- Google Gemini (1.5 Pro, Flash)
- Groq (Llama 3 70B/8B)
- Mistral (Large, Codestral)
- DeepSeek (V3, R1)
- Cohere (Command R/R+)
- NVIDIA NIM
- OpenRouter (unified gateway)

### AI Agent Execution Engine
An **8-state machine** (Pending → Planning → Executing → Reviewing → Completed) drives autonomous agent workflows with:
- Iterative Plan → Execute → Observe → Review loops (up to 20 iterations)
- **Human-in-the-Loop (HITL)** checkpoints requiring explicit human approval before critical actions
- Real-time progress streaming via Server-Sent Events (SSE)
- Token usage tracking and cost accumulation per task

### Deterministic Code Analysis
Static analysis pipeline integrating multiple scanning engines:
- Built-in regex-based vulnerability patterns
- Semgrep rule integration
- Bandit security analyzer
- Credential leak detection (AWS keys, GitHub PATs, Slack tokens, JWTs, private keys)
- Compliance checking against SOC2, HIPAA, and PCI-DSS standards
- Attack graph generation for dependency threat visualization

### Multi-Tier Memory System
- **Episodic Memory**: Conversation history and session context
- **Semantic Memory**: pgvector-powered vector embeddings for similarity search
- **Working Memory**: Short-term task context within active agent sessions

### Skills Marketplace
- JSON/YAML skill manifest parsing with metadata, tags, and permission requirements
- Package management with tarball/zip extraction and checksum verification
- RAG-powered skill search using pgvector embeddings
- Sandboxed skill execution runtime

### Cost Intelligence
- Real-time token cost tracking per model and provider
- Project budget enforcement with configurable limits
- Cost forecasting and anomaly detection
- Optimization recommendations for model routing decisions

### Observability
- OpenTelemetry distributed tracing with Prometheus metrics export
- Structured JSON logging via Go's `log/slog`
- 13+ pre-registered Prometheus metrics (request duration, token consumption, auth failures, HITL checkpoints, queue depth, slow queries)
- Dedicated `/metrics` endpoint for Grafana/Prometheus scraping

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Client Layer                             │
│  VS Code Extension │ Browser Extension │ CLI │ REST Clients     │
└──────────┬──────────────────┬──────────────────┬────────────────┘
           │                  │                  │
           ▼                  ▼                  ▼
┌─────────────────────────────────────────────────────────────────┐
│                      API Gateway (chi/v5)                       │
│  Auth │ Rate Limit │ CORS │ Compression │ Tracing │ Audit      │
├─────────────────────────────────────────────────────────────────┤
│                     Domain Handlers                             │
│  Auth │ System │ Org │ Skills │ Scan │ Tasks │ Memory │ Billing │
├─────────────────────────────────────────────────────────────────┤
│                      Core Services                              │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────────────┐    │
│  │ LLM      │  │ Agent Engine │  │ Scanner Pipeline       │    │
│  │ Router   │  │ (8-State SM) │  │ (Regex+Semgrep+Bandit) │    │
│  │ 9 Provs  │  │ HITL Gates   │  │ Credential Detector    │    │
│  └──────────┘  └──────────────┘  └────────────────────────┘    │
│  ┌──────────┐  ┌──────────────┐  ┌────────────────────────┐    │
│  │ Skills   │  │ Cost Intel   │  │ Memory (Episodic +     │    │
│  │ Engine   │  │ Forecasting  │  │ Semantic pgvector)     │    │
│  └──────────┘  └──────────────┘  └────────────────────────┘    │
├─────────────────────────────────────────────────────────────────┤
│                     Infrastructure                              │
│  PostgreSQL 16 + pgvector │ Redis 7 │ NATS JetStream           │
└─────────────────────────────────────────────────────────────────┘
```

> For a detailed architecture breakdown, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

---

## Tech Stack

| Layer            | Technology                                          |
|------------------|-----------------------------------------------------|
| Language         | Go 1.26+                                            |
| HTTP Router      | chi/v5                                               |
| Database         | PostgreSQL 16 + pgvector                             |
| Cache            | Redis 7                                              |
| Message Queue    | NATS JetStream                                       |
| Authentication   | JWT (golang-jwt/v5) + API Keys (SHA-256)            |
| LLM SDKs        | go-openai, google/genai, mcp-go                     |
| Observability    | OpenTelemetry + Prometheus                           |
| CLI Framework    | Cobra + Viper                                        |
| Testing          | testify + httptest + table-driven tests              |
| Containerization | Multi-stage Docker + Helm + Kubernetes              |
| CI/CD            | GitHub Actions                                       |

---

## Getting Started

### Prerequisites

- **Go** 1.26 or later
- **Docker** & **Docker Compose** (for infrastructure services)
- **Make** (build automation)
- **PostgreSQL 16** with the `pgvector` extension
- **Redis 7**
- **NATS 2.10+** with JetStream enabled

### Infrastructure Setup

Start the local development stack (PostgreSQL + pgvector, Redis, NATS):

```bash
docker compose -f docker-compose.dev.yml up -d
```

This provisions:
| Service    | Address              | Details                          |
|------------|----------------------|----------------------------------|
| PostgreSQL | `127.0.0.1:5432`     | Database `vigilagent`, pgvector enabled |
| Redis      | `127.0.0.1:6379`     | Cache and rate limiting store    |
| NATS       | `127.0.0.1:4222`     | JetStream message queue          |
| NATS Mgmt  | `127.0.0.1:8222`     | NATS management dashboard        |

### Database Migration

Copy the environment template and configure your database credentials:

```bash
cp .env.example .env
# Edit .env with your database credentials
```

Run the schema migration:

```bash
make migrate
```

### Running the Server

```bash
make run
```

The API server starts on **port 8080**. Verify it's running:

```bash
curl http://localhost:8080/api/v1/health
```

### CLI Usage

Build and use the `vigil` CLI for managing agents, tasks, and skills:

```bash
go build -o vigil ./cmd/cli
./vigil agent create --project <project-id> --name "Code Reviewer"
./vigil task submit --agent <agent-id> --prompt "Review this pull request"
```

---

## Project Structure

```
.
├── cmd/                          # Application entry points (8 binaries)
│   ├── api/                      # Main API server (port 8080)
│   ├── proxy/                    # LLM proxy gateway (port 9090)
│   ├── mcp/                      # MCP stdio server for IDE integration
│   ├── migrate/                  # Database migration runner
│   ├── cli/                      # Command-line client (vigil)
│   ├── healthcheck/              # Container health probe binary
│   ├── loadtest/                 # Concurrent HTTP load testing engine
│   └── bench/                    # LLM cost optimization benchmark harness
│
├── internal/                     # Core application packages (67 packages)
│   ├── agent/                    # 8-state machine agent execution engine
│   ├── api/                      # DTOs, request/response models, validation
│   ├── auth/                     # JWT, API keys, bcrypt, RBAC
│   ├── cache/                    # Redis-backed caching with TTL
│   ├── compliance/               # SOC2, HIPAA, PCI-DSS rule engine
│   ├── config/                   # Viper configuration loader
│   ├── configdrift/              # Configuration drift detection
│   ├── contextbuilder/           # LLM prompt assembly & token budgeting
│   ├── costintel/                # Cost analytics, forecasting, anomalies
│   ├── database/                 # PostgreSQL (pgx), Redis, migrations
│   ├── featureflags/             # Runtime feature toggles (PostgreSQL-backed)
│   ├── knowledge/                # RAG ingestion, chunking, embeddings
│   ├── llm/                      # Multi-provider LLM router (9 providers)
│   ├── mcp/                      # MCP server implementation
│   ├── memory/                   # Episodic + semantic + working memory
│   ├── middleware/               # Auth, rate limit, CORS, compression, audit
│   ├── pipeline/                 # Multi-stage code validation pipeline
│   ├── proxy/                    # LLM reverse proxy with SSE streaming
│   ├── queue/                    # NATS JetStream job queue & workers
│   ├── repository/               # PostgreSQL data access layer
│   ├── router/                   # HTTP route registration & domain handlers
│   ├── scanner/                  # SAST engine (regex + Semgrep + Bandit)
│   ├── security/                 # AES-256-GCM encryption, credential scanning
│   ├── skills/                   # Skills marketplace & RAG search
│   ├── sse/                      # Server-Sent Events streaming
│   ├── tools/                    # Agent tool registry & sandbox execution
│   ├── webhook/                  # Webhook publishing & delivery queue
│   └── websocket/                # Real-time WebSocket connections
│
├── pkg/                          # Reusable shared Go packages
├── migrations/                   # SQL migration files
├── configs/                      # YAML configs & setup scripts
├── deploy/                       # Deployment templates
├── helm/                         # Helm charts for Kubernetes
├── k8s/                          # Kubernetes manifests
├── scripts/                      # Build & load test scripts
├── docs/                         # Extended documentation
├── mcp-server/                   # MCP TypeScript npm wrapper
├── vscode-extension/             # VS Code extension source
├── browser-extension/            # Browser extension source
├── Dockerfile                    # Multi-stage Docker build
├── docker-compose.dev.yml        # Local dev infrastructure
├── Makefile                      # Build, test, lint, deploy targets
└── go.mod                        # Go module definition
```

---

## Binaries

| Binary          | Source           | Port  | Purpose                                                    |
|-----------------|------------------|-------|------------------------------------------------------------|
| `vigil-api`     | `cmd/api`        | 8080  | Core REST API server with all business logic               |
| `vigil-proxy`   | `cmd/proxy`      | 9090  | High-throughput LLM reverse proxy with SSE streaming       |
| `vigil-mcp`     | `cmd/mcp`        | stdio | MCP JSON-RPC server for IDE integration (Cursor, Cline)    |
| `vigil-migrate` | `cmd/migrate`    | —     | Database schema migration runner                           |
| `vigil`         | `cmd/cli`        | —     | CLI client for agents, tasks, skills management            |
| `healthcheck`   | `cmd/healthcheck`| —     | Lightweight TCP health probe for containers                |
| `loadtest`      | `cmd/loadtest`   | —     | Concurrent HTTP load tester (up to 30K requests)           |
| `bench`         | `cmd/bench`      | —     | LLM cost optimization benchmark harness                    |

---

## LLM Provider Support

| Provider     | Models                          | Features                          |
|--------------|----------------------------------|-----------------------------------|
| OpenAI       | GPT-4o, GPT-4o-mini             | Streaming, function calling       |
| Anthropic    | Claude Sonnet 4, Opus 4, Haiku 3.5 | Streaming, extended context    |
| Google       | Gemini 1.5 Pro, Flash           | Streaming, multimodal             |
| Groq         | Llama 3 70B, 8B                 | Ultra-fast inference              |
| Mistral      | Large, Codestral                | Code-specialized models           |
| DeepSeek     | V3, R1                          | Reasoning-optimized               |
| Cohere       | Command R, R+                   | Enterprise RAG-optimized          |
| NVIDIA NIM   | Custom deployments              | Self-hosted inference             |
| OpenRouter   | 100+ models                     | Unified gateway access            |

All providers include **circuit breaking**, **health monitoring**, **automatic failover**, and **response caching**.

---

## API Overview

The API serves all endpoints under the `/api/v1/` prefix. Full documentation is available at `/api/v1/docs` (Swagger UI) when the server is running.

### Public Endpoints
| Method | Endpoint                              | Description                    |
|--------|---------------------------------------|--------------------------------|
| GET    | `/api/v1/health`                      | Liveness probe                 |
| GET    | `/api/v1/ready`                       | Readiness probe                |
| POST   | `/api/v1/auth/register`               | User registration              |
| POST   | `/api/v1/auth/login`                  | Account login                  |
| GET    | `/api/v1/providers`                   | List available LLM providers   |

### Core Protected Endpoints (JWT / API Key)
| Method | Endpoint                              | Description                    |
|--------|---------------------------------------|--------------------------------|
| POST   | `/api/v1/tasks`                       | Submit a new agent task        |
| GET    | `/api/v1/tasks/{taskID}/stream`       | SSE stream of task progress    |
| POST   | `/api/v1/scan`                        | Run static security analysis   |
| POST   | `/api/v1/review`                      | Full code review pipeline      |
| POST   | `/api/v1/memory`                      | Store a memory entry           |
| POST   | `/api/v1/memory/search`               | Semantic vector search         |
| POST   | `/api/v1/hitl/decide`                 | Approve/reject HITL checkpoint |
| GET    | `/api/v1/analytics/cost-intel`        | Cost intelligence dashboard    |

> For the complete API reference with all 90+ endpoints, see [docs/API_REFERENCE.md](docs/API_REFERENCE.md).

---

## MCP Server Integration

VigilAgent ships an MCP (Model Context Protocol) server for seamless integration with AI-powered IDEs like **Cursor** and **Cline**.

```json
{
  "mcpServers": {
    "vigilagent": {
      "command": "vigil-mcp",
      "env": {
        "VIGILAGENT_API_URL": "http://localhost:8080",
        "VIGILAGENT_API_KEY": "your-api-key"
      }
    }
  }
}
```

The MCP server communicates via **stdio** using JSON-RPC, keeping `stdout` clean for protocol messages and routing logs to `stderr`.

---

## VS Code Extension

A dedicated VS Code extension is available in the `vscode-extension/` directory providing:
- ChatGPT-style chat interface within the editor
- Workspace-aware context injection
- Real-time task streaming

Build and install:
```bash
make extension-package    # Produces .vsix file
make extension-publish    # Publishes to VS Code Marketplace
```

---

## Browser Extension

A companion browser extension in `browser-extension/` enables:
- Quick code analysis from any webpage
- Integration with the VigilAgent API

---

## Configuration

VigilAgent uses [Viper](https://github.com/spf13/viper) for configuration management. Configuration is loaded from environment variables (prefixed with `VIGILAGENT_`) or YAML files in `configs/`.

Copy the template and customize:

```bash
cp .env.example .env
```

### Key Configuration Groups

| Group       | Variables                                              | Description                           |
|-------------|--------------------------------------------------------|---------------------------------------|
| Server      | `PORT`, `ENV`, `BASE_URL`, `RATE_LIMIT_PER_MIN`       | Server binding and rate limits        |
| Database    | `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME` | PostgreSQL connection                 |
| Auth        | `JWT_SECRET`, `JWT_EXPIRATION`, `API_KEY_PREFIX`       | Authentication settings               |
| LLM         | `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`| Provider API keys                     |
| Redis       | `REDIS_URL`                                            | Cache and rate limit store            |
| NATS        | `NATS_URL`                                             | Message queue connection              |
| Observability| `LOG_LEVEL`, `LOG_FORMAT`                             | Logging configuration                 |

> For the complete configuration reference, see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Testing

VigilAgent maintains **133 test files** across **47 packages** using Go's standard testing framework with [testify](https://github.com/stretchr/testify) assertions.

```bash
make test              # Run all unit tests
make test-short        # Run tests with -short flag
make test-race         # Run tests with race detector
make test-cover        # Generate coverage report
make test-integration  # Run integration tests
make bench             # Run benchmarks
```

> For the complete testing guide, see [docs/TESTING.md](docs/TESTING.md).

---

## Deployment

### Docker

```bash
make docker-build      # Build production image
make docker-run        # Run container on port 8080
```

### Kubernetes

Helm charts and raw manifests are provided:

```bash
helm install vigilagent ./helm/vigilagent
# or
kubectl apply -f k8s/
```

> For the complete deployment guide, see [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md).

---

## Security

VigilAgent implements defense-in-depth security:

- **Authentication**: JWT with IP/User-Agent binding + SHA-256 API key hashing
- **Encryption**: AES-256-GCM with PBKDF2 key derivation (100K iterations)
- **Input Sanitization**: Null byte removal, path traversal prevention, XSS escaping
- **Credential Scanning**: Automated detection and redaction of secrets in code
- **Security Headers**: HSTS, CSP, X-Frame-Options, Permissions-Policy
- **Rate Limiting**: Redis-backed token bucket and sliding window algorithms
- **IP Filtering**: CIDR-based allowlisting/denylisting with trusted proxy support
- **Account Lockout**: Progressive lockout after failed authentication attempts

> For the complete security documentation, see [docs/SECURITY.md](docs/SECURITY.md).

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding standards, and contribution guidelines.

---

## License

Copyright © 2025-2026 VigilAgent. All rights reserved.
