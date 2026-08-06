# Configuration Reference

> Complete reference for all VigilAgent configuration options.

---

## Table of Contents

- [Overview](#overview)
- [Configuration Sources](#configuration-sources)
- [Server Configuration](#server-configuration)
- [Database Configuration](#database-configuration)
- [Redis Configuration](#redis-configuration)
- [NATS Configuration](#nats-configuration)
- [Authentication Configuration](#authentication-configuration)
- [LLM Provider Configuration](#llm-provider-configuration)
- [CORS Configuration](#cors-configuration)
- [Rate Limiting Configuration](#rate-limiting-configuration)
- [IP Filtering Configuration](#ip-filtering-configuration)
- [Logging Configuration](#logging-configuration)
- [Audit Configuration](#audit-configuration)
- [Security Headers Configuration](#security-headers-configuration)
- [Body Size Configuration](#body-size-configuration)
- [Feature Flags](#feature-flags)
- [YAML Configuration Files](#yaml-configuration-files)

---

## Overview

VigilAgent uses [Viper](https://github.com/spf13/viper) for configuration management. Configuration values are resolved in the following priority order (highest first):

1. **Environment variables** (prefixed with `VIGILAGENT_`)
2. **YAML configuration files** (`configs/config.yaml`, `configs/config.prod.yaml`)
3. **Default values** (hardcoded in `internal/config/`)

---

## Configuration Sources

### Environment Variables

All environment variables use the `VIGILAGENT_` prefix. Nested keys use `_` as separator:

```bash
# Example: Server configuration
VIGILAGENT_PORT=8080
VIGILAGENT_ENV=production

# Example: Database configuration
VIGILAGENT_DB_HOST=localhost
VIGILAGENT_DB_PORT=5432
```

### YAML Files

```yaml
# configs/config.yaml
server:
  port: 8080
  env: development

database:
  host: localhost
  port: 5432
```

### .env File

Copy `.env.example` to `.env` for local development:
```bash
cp .env.example .env
```

---

## Server Configuration

| Variable                        | YAML Key               | Type     | Default       | Description                                |
|---------------------------------|------------------------|----------|---------------|--------------------------------------------|
| `VIGILAGENT_PORT`               | `server.port`         | int      | `8080`        | HTTP server listening port                 |
| `VIGILAGENT_HOST`               | `server.host`         | string   | `0.0.0.0`     | HTTP server bind address                   |
| `VIGILAGENT_ENV`                | `server.env`          | string   | `development` | Environment: `development`, `staging`, `production` |
| `VIGILAGENT_BASE_URL`           | `server.base_url`     | string   | `http://localhost:8080` | Public-facing base URL           |
| `VIGILAGENT_RATE_LIMIT_PER_MIN` | `server.rate_limit`   | int      | `60`          | Global rate limit per minute per user      |
| `VIGILAGENT_READ_TIMEOUT`       | `server.read_timeout` | duration | `15s`         | Maximum duration for reading request       |
| `VIGILAGENT_WRITE_TIMEOUT`      | `server.write_timeout`| duration | `15s`         | Maximum duration for writing response      |
| `VIGILAGENT_IDLE_TIMEOUT`       | `server.idle_timeout` | duration | `60s`         | Maximum duration for idle keep-alive       |

---

## Database Configuration

| Variable                              | YAML Key                       | Type     | Default       | Description                            |
|---------------------------------------|--------------------------------|----------|---------------|----------------------------------------|
| `VIGILAGENT_DB_HOST`                  | `database.host`               | string   | `localhost`   | PostgreSQL server hostname             |
| `VIGILAGENT_DB_PORT`                  | `database.port`               | int      | `5432`        | PostgreSQL server port                 |
| `VIGILAGENT_DB_USER`                  | `database.user`               | string   | `vigilagent`  | Database username                      |
| `VIGILAGENT_DB_PASS`                  | `database.password`           | string   | `vigilagent`  | Database password                      |
| `VIGILAGENT_DB_NAME`                  | `database.name`               | string   | `vigilagent`  | Database name                          |
| `VIGILAGENT_DB_SSL_MODE`             | `database.ssl_mode`           | string   | `disable`     | SSL mode: `disable`, `require`, `verify-full` |
| `VIGILAGENT_DB_MAX_OPEN_CONNS`       | `database.max_open_conns`     | int      | `25`          | Maximum open connections in pool       |
| `VIGILAGENT_DB_MAX_IDLE_CONNS`       | `database.max_idle_conns`     | int      | `10`          | Maximum idle connections in pool       |
| `VIGILAGENT_DB_MAX_LIFETIME`         | `database.max_lifetime`       | duration | `5m`          | Maximum connection lifetime            |
| `VIGILAGENT_DB_SLOW_QUERY_THRESHOLD` | `database.slow_query_threshold`| duration | `200ms`       | Queries slower than this are logged    |
| `VIGILAGENT_DB_STATEMENT_TIMEOUT`    | `database.statement_timeout`  | duration | `30s`         | Maximum query execution time           |

> **Important**: PostgreSQL must have the `pgvector` extension installed for the memory system's vector embedding storage.

---

## Redis Configuration

| Variable              | YAML Key        | Type   | Default                  | Description                |
|-----------------------|-----------------|--------|--------------------------|----------------------------|
| `VIGILAGENT_REDIS_URL`| `redis.url`    | string | `redis://localhost:6379` | Redis connection URL       |

Redis is used for:
- Response caching with TTL
- Rate limiting (token bucket and sliding window)
- Session blacklisting
- Idempotency key storage
- Distributed locks

---

## NATS Configuration

| Variable              | YAML Key      | Type   | Default                   | Description                     |
|-----------------------|---------------|--------|---------------------------|---------------------------------|
| `VIGILAGENT_NATS_URL` | `nats.url`   | string | `nats://localhost:4222`   | NATS server connection URL      |

NATS JetStream is used for:
- Asynchronous task execution queue (`vigilagent.tasks.execute`)
- Dead-letter retry support
- Durable consumer subscriptions

---

## Authentication Configuration

| Variable                            | YAML Key                    | Type     | Default       | Description                             |
|-------------------------------------|-----------------------------|----------|---------------|-----------------------------------------|
| `VIGILAGENT_JWT_SECRET`             | `auth.jwt_secret`          | string   | **Required**  | HMAC-SHA256 signing key for JWTs        |
| `VIGILAGENT_JWT_EXPIRATION`         | `auth.jwt_expiration`      | duration | `24h`         | JWT token lifetime                      |
| `VIGILAGENT_JWT_AUDIENCE`           | `auth.jwt_audience`        | string   | `vigilagent`  | Expected JWT audience claim             |
| `VIGILAGENT_JWT_BIND_TO_IP`        | `auth.jwt_bind_to_ip`     | bool     | `false`       | Bind JWT to client IP address           |
| `VIGILAGENT_JWT_BIND_TO_USERAGENT` | `auth.jwt_bind_to_ua`     | bool     | `false`       | Bind JWT to client User-Agent           |
| `VIGILAGENT_API_KEY_PREFIX`         | `auth.api_key_prefix`     | string   | `vgl_sk_`     | Prefix for generated API keys           |

> **Warning**: The `JWT_SECRET` must be changed from any default value before deploying to production. Use a cryptographically random 256-bit (32-byte) value.

---

## LLM Provider Configuration

| Variable                          | YAML Key                 | Type   | Default   | Description                        |
|-----------------------------------|--------------------------|--------|-----------|------------------------------------|
| `VIGILAGENT_OPENAI_API_KEY`       | `llm.openai_key`        | string | —         | OpenAI API key                     |
| `VIGILAGENT_ANTHROPIC_API_KEY`    | `llm.anthropic_key`     | string | —         | Anthropic API key                  |
| `VIGILAGENT_GEMINI_API_KEY`       | `llm.gemini_key`        | string | —         | Google Gemini API key              |
| `VIGILAGENT_OPENROUTER_API_KEY`   | `llm.openrouter_key`    | string | —         | OpenRouter API key                 |
| `VIGILAGENT_MISTRAL_API_KEY`      | `llm.mistral_key`       | string | —         | Mistral AI API key                 |
| `VIGILAGENT_GROQ_API_KEY`         | `llm.groq_key`          | string | —         | Groq API key                       |
| `VIGILAGENT_NVIDIA_NIM_API_KEY`   | `llm.nvidia_nim_key`    | string | —         | NVIDIA NIM API key                 |
| `VIGILAGENT_COHERE_API_KEY`       | `llm.cohere_key`        | string | —         | Cohere API key                     |
| `VIGILAGENT_DEFAULT_MODEL`        | `llm.default_model`     | string | `gpt-4o`  | Default model for new tasks        |
| `VIGILAGENT_BUDGET_PER_TASK`      | `llm.budget_per_task`   | float  | `1.00`    | Maximum cost (USD) per agent task  |
| `VIGILAGENT_MAX_TOKENS`           | `llm.max_tokens`        | int    | `4096`    | Default max tokens for completions |

> At least one LLM provider API key must be configured. The router will only include providers with valid keys.

---

## CORS Configuration

| Variable                          | YAML Key                    | Type     | Default       | Description                          |
|-----------------------------------|-----------------------------|----------|---------------|--------------------------------------|
| `VIGILAGENT_CORS_ALLOW_ORIGINS`   | `cors.allow_origins`       | []string | `["*"]`       | Allowed origin domains               |
| `VIGILAGENT_CORS_ALLOW_METHODS`   | `cors.allow_methods`       | []string | `["GET","POST","PUT","DELETE","OPTIONS"]` | Allowed HTTP methods |
| `VIGILAGENT_CORS_ALLOW_HEADERS`   | `cors.allow_headers`       | []string | `["Authorization","Content-Type","X-API-Key"]` | Allowed request headers |
| `VIGILAGENT_CORS_EXPOSE_HEADERS`  | `cors.expose_headers`      | []string | `[]`          | Headers exposed to browsers          |
| `VIGILAGENT_CORS_ALLOW_CREDENTIALS`| `cors.allow_credentials`  | bool     | `true`        | Allow credentials (cookies, auth)    |
| `VIGILAGENT_CORS_MAX_AGE`        | `cors.max_age`             | int      | `86400`       | Preflight cache duration (seconds)   |

> **Production**: Replace `["*"]` with your specific frontend domains (e.g., `["https://app.yourdomain.com"]`).

---

## Rate Limiting Configuration

Rate limits are configured per-user using Redis-backed algorithms:

| Variable                          | YAML Key                  | Type | Default | Description                     |
|-----------------------------------|---------------------------|------|---------|---------------------------------|
| `VIGILAGENT_RATE_LIMIT_PER_MIN`   | `server.rate_limit`      | int  | `60`    | Requests per minute per user    |

Tier-based rate limits are enforced via RBAC:
| Tier    | Requests/Minute |
|---------|-----------------|
| Free    | 60              |
| Pro     | 300             |
| Admin   | Unlimited       |

---

## IP Filtering Configuration

| Variable                          | YAML Key                    | Type     | Default | Description                          |
|-----------------------------------|-----------------------------|----------|---------|--------------------------------------|
| `VIGILAGENT_IP_ALLOWLIST`         | `ipfilter.allowlist`       | []string | `[]`    | CIDR subnets to allow (empty = all)  |
| `VIGILAGENT_IP_DENYLIST`          | `ipfilter.denylist`        | []string | `[]`    | CIDR subnets to block                |
| `VIGILAGENT_TRUSTED_PROXIES`      | `ipfilter.trusted_proxies` | []string | `[]`    | Proxy IPs for X-Forwarded-For parsing|

---

## Logging Configuration

| Variable                    | YAML Key          | Type   | Default       | Description                          |
|-----------------------------|-------------------|--------|---------------|--------------------------------------|
| `VIGILAGENT_LOG_LEVEL`      | `logging.level`  | string | `info`        | Log level: `debug`, `info`, `warn`, `error` |
| `VIGILAGENT_LOG_FORMAT`     | `logging.format` | string | `json`        | Log format: `json` or `text`         |

### Log Output

- **Format**: Structured JSON (production) or human-readable text (development)
- **Destination**: `stdout` (designed for container log aggregation)
- **Context Fields**: `request_id` and `trace_id` are automatically injected into all log entries from HTTP request context

---

## Audit Configuration

| Variable                              | YAML Key                  | Type     | Default | Description                         |
|---------------------------------------|---------------------------|----------|---------|-------------------------------------|
| `VIGILAGENT_AUDIT_ENABLED`            | `audit.enabled`          | bool     | `true`  | Enable audit logging                |
| `VIGILAGENT_AUDIT_RETENTION_DAYS`     | `audit.retention_days`   | int      | `90`    | Days to retain audit logs           |

---

## Security Headers Configuration

| Variable                              | YAML Key                          | Type | Default | Description                       |
|---------------------------------------|-----------------------------------|------|---------|-----------------------------------|
| `VIGILAGENT_SECURITY_HSTS_ENABLED`    | `security_headers.hsts_enabled`  | bool | `true`  | Enable HSTS header                |
| `VIGILAGENT_SECURITY_CSP_ENABLED`     | `security_headers.csp_enabled`   | bool | `true`  | Enable Content Security Policy    |
| `VIGILAGENT_SECURITY_FRAME_DENY`      | `security_headers.frame_deny`    | bool | `true`  | Enable X-Frame-Options: DENY      |

---

## Body Size Configuration

| Variable                          | YAML Key              | Type | Default  | Description                          |
|-----------------------------------|-----------------------|------|----------|--------------------------------------|
| `VIGILAGENT_MAX_BODY_SIZE`        | `body.max_size`      | int  | `1048576`| Max request body size (bytes, 1MB)   |

---

## Feature Flags

Feature flags are managed in the PostgreSQL `feature_flags` table and cached in-memory with a 5-minute TTL.

### Management via API

```bash
# List all flags
GET /api/v1/feature-flags

# Enable a flag
PUT /api/v1/feature-flags
{"name": "new-scanner-v2", "enabled": true}

# Check a flag
GET /api/v1/feature-flags/check?name=new-scanner-v2

# Remove a flag
DELETE /api/v1/feature-flags?name=new-scanner-v2
```

---

## YAML Configuration Files

### Default Configuration (`configs/config.yaml`)

```yaml
server:
  port: 8080
  host: 0.0.0.0
  env: development
  rate_limit: 60
  read_timeout: 15s
  write_timeout: 15s
  idle_timeout: 60s

database:
  host: localhost
  port: 5432
  user: vigilagent
  password: vigilagent
  name: vigilagent
  ssl_mode: disable
  max_open_conns: 25
  max_idle_conns: 10
  max_lifetime: 5m

redis:
  url: redis://localhost:6379

nats:
  url: nats://localhost:4222

auth:
  jwt_expiration: 24h
  jwt_audience: vigilagent
  api_key_prefix: vgl_sk_

llm:
  default_model: gpt-4o
  budget_per_task: 1.00
  max_tokens: 4096

logging:
  level: debug
  format: text

cors:
  allow_origins: ["*"]
  allow_credentials: true
  max_age: 86400
```

### Production Configuration (`configs/config.prod.yaml`)

```yaml
server:
  env: production
  rate_limit: 300
  read_timeout: 30s
  write_timeout: 30s

database:
  ssl_mode: require
  max_open_conns: 50
  max_idle_conns: 25
  statement_timeout: 30s

auth:
  jwt_bind_to_ip: true
  jwt_bind_to_ua: true

logging:
  level: info
  format: json

cors:
  allow_origins: ["https://app.yourdomain.com"]
```
