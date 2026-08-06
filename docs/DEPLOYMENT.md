# Deployment Guide

> Complete guide for deploying VigilAgent across all supported environments.

---

## Table of Contents

- [Overview](#overview)
- [Prerequisites](#prerequisites)
- [Environment Configuration](#environment-configuration)
- [Local Development](#local-development)
- [Docker Deployment](#docker-deployment)
- [Kubernetes Deployment](#kubernetes-deployment)
- [Helm Chart Deployment](#helm-chart-deployment)
- [Database Migrations](#database-migrations)
- [MCP Server Distribution](#mcp-server-distribution)
- [VS Code Extension Publishing](#vs-code-extension-publishing)
- [Health Checks & Monitoring](#health-checks--monitoring)
- [Production Checklist](#production-checklist)
- [Troubleshooting](#troubleshooting)

---

## Overview

VigilAgent supports multiple deployment models:

| Model          | Best For                            | Complexity |
|----------------|-------------------------------------|------------|
| Local (`make`) | Development and debugging           | Low        |
| Docker         | Single-server or staging            | Medium     |
| Kubernetes     | Production multi-node clusters      | High       |
| Helm           | Kubernetes with templated configs   | High       |

---

## Prerequisites

| Dependency        | Minimum Version | Purpose                            |
|-------------------|-----------------|------------------------------------|
| Go                | 1.26+           | Building from source               |
| Docker            | 24.0+           | Container builds                   |
| Docker Compose    | 2.20+           | Local infrastructure stack         |
| kubectl           | 1.28+           | Kubernetes deployments             |
| Helm              | 3.12+           | Helm chart deployments             |
| PostgreSQL        | 16              | Primary database (with pgvector)   |
| Redis             | 7.0+            | Caching and rate limiting          |
| NATS              | 2.10+           | Message queue (JetStream enabled)  |

---

## Environment Configuration

### Step 1: Copy the Environment Template

```bash
cp .env.example .env
```

### Step 2: Configure Required Variables

#### Server
```env
VIGILAGENT_PORT=8080
VIGILAGENT_ENV=production          # development | staging | production
VIGILAGENT_BASE_URL=https://api.yourdomain.com
VIGILAGENT_RATE_LIMIT_PER_MIN=300
```

#### Database (PostgreSQL + pgvector)
```env
VIGILAGENT_DB_HOST=localhost
VIGILAGENT_DB_PORT=5432
VIGILAGENT_DB_USER=vigilagent
VIGILAGENT_DB_PASS=your-secure-password
VIGILAGENT_DB_NAME=vigilagent
VIGILAGENT_DB_SSL_MODE=require      # disable | require | verify-full
VIGILAGENT_DB_MAX_OPEN_CONNS=25
VIGILAGENT_DB_MAX_IDLE_CONNS=10
VIGILAGENT_DB_MAX_LIFETIME=5m
VIGILAGENT_DB_SLOW_QUERY_THRESHOLD=200ms
VIGILAGENT_DB_STATEMENT_TIMEOUT=30s
```

#### Redis
```env
VIGILAGENT_REDIS_URL=redis://localhost:6379
```

#### NATS JetStream
```env
VIGILAGENT_NATS_URL=nats://localhost:4222
```

#### Authentication
```env
VIGILAGENT_JWT_SECRET=your-256-bit-secret-key   # MUST be changed in production
VIGILAGENT_JWT_EXPIRATION=24h
VIGILAGENT_JWT_AUDIENCE=vigilagent
VIGILAGENT_JWT_BIND_TO_IP=true
VIGILAGENT_JWT_BIND_TO_USERAGENT=true
VIGILAGENT_API_KEY_PREFIX=vgl_sk_
```

#### LLM Provider API Keys
```env
VIGILAGENT_OPENAI_API_KEY=sk-...
VIGILAGENT_ANTHROPIC_API_KEY=sk-ant-...
VIGILAGENT_GEMINI_API_KEY=AIza...
VIGILAGENT_OPENROUTER_API_KEY=sk-or-...
VIGILAGENT_MISTRAL_API_KEY=...
VIGILAGENT_GROQ_API_KEY=gsk_...
VIGILAGENT_NVIDIA_NIM_API_KEY=...
VIGILAGENT_COHERE_API_KEY=...
VIGILAGENT_DEFAULT_MODEL=gpt-4o
VIGILAGENT_BUDGET_PER_TASK=1.00
VIGILAGENT_MAX_TOKENS=4096
```

#### Logging
```env
VIGILAGENT_LOG_LEVEL=info           # debug | info | warn | error
VIGILAGENT_LOG_FORMAT=json          # json | text
```

### YAML Configuration

Alternatively, use YAML config files in `configs/`:
- `configs/config.yaml` — Default local configuration
- `configs/config.prod.yaml` — Production environment template

---

## Local Development

### Start Infrastructure Services

```bash
docker compose -f docker-compose.dev.yml up -d
```

This starts:
| Service    | Address           |
|------------|-------------------|
| PostgreSQL | `localhost:5432`  |
| Redis      | `localhost:6379`  |
| NATS       | `localhost:4222`  |
| NATS Mgmt  | `localhost:8222`  |

### Run Database Migrations

```bash
make migrate
```

### Start the API Server

```bash
make run
```

Server starts on `http://localhost:8080`.

### Start the LLM Proxy (Optional)

```bash
go run ./cmd/proxy
```

Proxy starts on `http://localhost:9090`.

### Verify

```bash
curl http://localhost:8080/api/v1/health
# {"status":"healthy","timestamp":"..."}

curl http://localhost:8080/api/v1/ready
# {"status":"ready","dependencies":{"postgres":"ok","redis":"ok","nats":"ok"}}
```

---

## Docker Deployment

### Build the Production Image

```bash
make docker-build
```

This executes a multi-stage Docker build:
1. **Builder stage** (`golang:1.26-alpine`): Compiles a static Go binary with CGO disabled and debug symbols stripped
2. **Production stage** (`alpine:3.21`): Minimal image with `ca-certificates` and `tzdata`, running as `nobody`

### Run the Container

```bash
make docker-run
```

Or manually:

```bash
docker run -d \
  --name vigilagent \
  -p 8080:8080 \
  --env-file .env \
  vigilagent:latest
```

### Docker Compose (Full Stack)

For a complete production-like deployment, create a `docker-compose.yml`:

```yaml
version: "3.9"
services:
  api:
    build:
      context: .
      target: prod
    ports:
      - "8080:8080"
    env_file: .env
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      nats:
        condition: service_started
    healthcheck:
      test: ["CMD", "/app/healthcheck", "localhost:8080"]
      interval: 30s
      timeout: 5s
      retries: 3

  postgres:
    image: pgvector/pgvector:pg16
    environment:
      POSTGRES_DB: vigilagent
      POSTGRES_USER: vigilagent
      POSTGRES_PASSWORD: ${VIGILAGENT_DB_PASS}
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U vigilagent"]
      interval: 10s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 3

  nats:
    image: nats:2.10
    command: ["--jetstream"]

volumes:
  pgdata:
```

---

## Kubernetes Deployment

Raw Kubernetes manifests are provided in the `k8s/` directory.

### Apply Manifests

```bash
# Create namespace
kubectl create namespace vigilagent

# Apply configuration
kubectl apply -f k8s/ -n vigilagent
```

### Key Manifests

| File                    | Resource                                |
|-------------------------|-----------------------------------------|
| `deployment.yaml`       | API server deployment with replicas     |
| `service.yaml`          | ClusterIP/LoadBalancer service          |
| `configmap.yaml`        | Non-sensitive configuration             |
| `secret.yaml`           | Sensitive credentials (base64 encoded)  |
| `ingress.yaml`          | Ingress rules for external access       |
| `hpa.yaml`              | Horizontal Pod Autoscaler               |

### Health Probes

The deployment configures liveness and readiness probes:

```yaml
livenessProbe:
  httpGet:
    path: /api/v1/health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /api/v1/ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3
```

---

## Helm Chart Deployment

Helm charts are located in `helm/vigilagent/`.

### Install

```bash
helm install vigilagent ./helm/vigilagent \
  --namespace vigilagent \
  --create-namespace \
  --set config.database.host=postgres.vigilagent.svc \
  --set config.database.password=your-password \
  --set config.auth.jwtSecret=your-secret \
  --set config.llm.openaiKey=sk-...
```

### Upgrade

```bash
helm upgrade vigilagent ./helm/vigilagent \
  --namespace vigilagent \
  -f values-production.yaml
```

### Values Customization

Override defaults in a `values-production.yaml`:

```yaml
replicaCount: 3

resources:
  requests:
    cpu: 500m
    memory: 512Mi
  limits:
    cpu: 2000m
    memory: 2Gi

autoscaling:
  enabled: true
  minReplicas: 3
  maxReplicas: 10
  targetCPUUtilization: 70

config:
  server:
    env: production
    rateLimitPerMin: 300
  database:
    sslMode: require
    maxOpenConns: 50
```

---

## Database Migrations

### Run Migrations

```bash
# Using Make
make migrate

# Using the binary directly
go run ./cmd/migrate up

# Check current version
go run ./cmd/migrate version

# Check migration status
go run ./cmd/migrate status
```

### Migration Files

Migrations are stored in `migrations/` using sequential numbering:

```
migrations/
├── 000001_init_schema.up.sql      # Create all tables
└── 000001_init_schema.down.sql    # Drop all tables (rollback)
```

### Supabase Setup

For Supabase-hosted PostgreSQL, use the provided setup script:

```bash
psql $DATABASE_URL < configs/supabase_setup.sql
```

---

## MCP Server Distribution

The MCP server (`cmd/mcp`) is distributed as a cross-platform npm package.

### Build Cross-Platform Binaries

```bash
make mcp-binary
```

Compiles static Go binaries for:
- `linux/amd64`
- `linux/arm64`
- `darwin/amd64`
- `darwin/arm64`
- `windows/amd64`

### Package for npm

```bash
make mcp-package
```

### Publish to npm

```bash
make mcp-publish
```

### IDE Configuration

After installation, configure in your IDE:

**Cursor / Cline:**
```json
{
  "mcpServers": {
    "vigilagent": {
      "command": "vigilagent-mcp",
      "env": {
        "VIGILAGENT_API_URL": "http://localhost:8080",
        "VIGILAGENT_API_KEY": "vgl_sk_..."
      }
    }
  }
}
```

---

## VS Code Extension Publishing

### Build the Extension

```bash
make extension-package
```

Produces a `.vsix` file in the `dist/` directory.

### Install Locally

```bash
code --install-extension dist/vigilagent-*.vsix
```

### Publish to Marketplace

```bash
make extension-publish
```

---

## Health Checks & Monitoring

### Endpoints

| Endpoint          | Purpose                                    | Auth Required |
|-------------------|--------------------------------------------|---------------|
| `GET /api/v1/health` | Liveness probe — is the process alive?  | No            |
| `GET /api/v1/ready`  | Readiness probe — are dependencies up?  | No            |
| `GET /api/v1/metrics` | Prometheus metrics scrape endpoint     | No            |

### Container Health Check

The `cmd/healthcheck` binary performs a lightweight TCP dial:

```dockerfile
HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
  CMD ["/app/healthcheck", "localhost:8080"]
```

### Prometheus Integration

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: vigilagent
    scrape_interval: 15s
    static_configs:
      - targets: ["vigilagent:8080"]
    metrics_path: /api/v1/metrics
```

### Key Metrics to Monitor

| Metric                            | Alert Threshold               |
|-----------------------------------|-------------------------------|
| `http_request_duration_seconds`   | P99 > 5s                     |
| `auth_failures_total`             | > 100/min (brute force)      |
| `nats_queue_depth`                | > 1000 (backlog)             |
| `active_sessions`                 | > 80% of capacity            |
| `slow_query_duration`             | P95 > 1s                     |
| `tokens_consumed_total`           | Budget threshold exceeded    |

---

## Production Checklist

### Security
- [ ] Change `JWT_SECRET` from default value
- [ ] Enable `JWT_BIND_TO_IP=true`
- [ ] Enable `JWT_BIND_TO_USERAGENT=true`
- [ ] Set `DB_SSL_MODE=require` or `verify-full`
- [ ] Configure IP allowlisting for admin endpoints
- [ ] Enable CORS with production-specific origins only
- [ ] Rotate all LLM API keys from development keys

### Performance
- [ ] Set `DB_MAX_OPEN_CONNS` based on expected load (recommended: 25-50)
- [ ] Configure `RATE_LIMIT_PER_MIN` per tier
- [ ] Enable response compression (Gzip/Brotli)
- [ ] Set appropriate `READ_TIMEOUT`, `WRITE_TIMEOUT`, `IDLE_TIMEOUT`

### Observability
- [ ] Configure Prometheus scraping on `/api/v1/metrics`
- [ ] Set `LOG_FORMAT=json` for structured log aggregation
- [ ] Configure OpenTelemetry exporter for distributed tracing
- [ ] Set up alerting on key metrics

### Infrastructure
- [ ] Run PostgreSQL with pgvector extension enabled
- [ ] Enable NATS JetStream for persistent message delivery
- [ ] Configure Redis with password and TLS in production
- [ ] Enable Docker HEALTHCHECK or Kubernetes probes
- [ ] Set up database backup and recovery procedures

### Deployment
- [ ] Use multi-stage Docker build (production target)
- [ ] Run container as non-root (`nobody`)
- [ ] Set resource limits (CPU/memory) in Kubernetes
- [ ] Configure Horizontal Pod Autoscaler
- [ ] Test rollback procedures

---

## Troubleshooting

### Server Won't Start

| Symptom                          | Cause                           | Fix                                   |
|----------------------------------|---------------------------------|---------------------------------------|
| `connection refused` on DB       | PostgreSQL not running          | `docker compose up -d postgres`       |
| `pgvector extension not found`   | pgvector not installed          | Use `pgvector/pgvector:pg16` image    |
| `NATS connection failed`         | NATS not running                | `docker compose up -d nats`           |
| `JWT_SECRET not set`             | Missing env variable            | Set in `.env` file                    |
| `port already in use`            | Another process on 8080         | Change `PORT` or kill the process     |

### Migration Failures

```bash
# Check current migration version
go run ./cmd/migrate version

# Check migration status
go run ./cmd/migrate status

# Re-run migrations
go run ./cmd/migrate up
```

### Connection Pool Exhaustion

If you see `too many connections` errors:
1. Increase `DB_MAX_OPEN_CONNS`
2. Decrease `DB_MAX_LIFETIME` to recycle connections faster
3. Check for connection leaks using `DB_SLOW_QUERY_THRESHOLD` logging
