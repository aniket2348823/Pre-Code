# VigilAgent API Contract

Version: 4.0  
Status: Build-ready  
Scope: LLM output-improvement middleware  
Target implementation: Go 1.22+, chi-compatible HTTP API, PostgreSQL/pgvector, Redis, NATS JetStream  
Base URL: `https://api.vigilagent.com/v1`

## 1. Product Boundary

VigilAgent is a middleware layer between a user/application and one or more LLM providers. It does not act as an autonomous coding agent in this contract. Its job is to improve LLM output by optimizing prompts, selecting the best provider/model, enforcing budgets, validating and repairing responses, caching repeated work, tracking quality, and returning transparent metadata to the caller.

Core flow:

```text
User/App Request
  -> auth/rate-limit/budget checks
  -> prompt sanitization and optimization
  -> context compression and cache lookup
  -> model routing and provider failover
  -> LLM call
  -> response validation, repair, and quality scoring
  -> usage/cost recording
  -> final response to user/app
```

## 2. API Conventions

All request and response bodies are JSON unless marked as SSE. Field names use `snake_case`. IDs are UUID strings. Timestamps are RFC3339 UTC. Costs are USD decimals. All protected routes require either JWT or API-key authentication.

Required headers:

| Header | Required | Notes |
| --- | --- | --- |
| `Authorization: Bearer <token-or-api-key>` | protected routes | JWT or `vga_*` API key |
| `Content-Type: application/json` | JSON bodies | required for POST/PATCH |
| `Accept: application/json` | recommended | streaming uses `text/event-stream` |
| `Idempotency-Key` | POST requests | prevents duplicate provider calls |
| `X-Request-ID` | optional | generated if absent |

Standard response headers:

| Header | Meaning |
| --- | --- |
| `X-Request-ID` | request correlation ID |
| `X-RateLimit-Limit` | limit for current window |
| `X-RateLimit-Remaining` | remaining requests |
| `X-RateLimit-Reset` | Unix reset timestamp |
| `Retry-After` | retry delay for `429` or retryable `503` |

Pagination shape:

```json
{
  "data": [],
  "pagination": {
    "has_more": false,
    "next_cursor": null,
    "total_count": 0
  }
}
```

## 3. Error Format

```json
{
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request body validation failed",
    "details": {"field": "messages", "reason": "required"},
    "request_id": "req_abc123",
    "timestamp": "2026-06-30T00:00:00Z"
  }
}
```

| Code | HTTP | Meaning |
| --- | ---: | --- |
| `UNAUTHORIZED` | 401 | missing, invalid, expired, or revoked credential |
| `FORBIDDEN` | 403 | caller lacks required scope or org role |
| `RESOURCE_NOT_FOUND` | 404 | resource is absent or not visible |
| `RESOURCE_CONFLICT` | 409 | duplicate resource or invalid state |
| `IDEMPOTENCY_CONFLICT` | 409 | same key reused with different payload |
| `VALIDATION_ERROR` | 422 | schema or semantic validation failed |
| `BUDGET_EXCEEDED` | 402 | hard budget or quota exceeded |
| `RATE_LIMIT_EXCEEDED` | 429 | rate limit exceeded |
| `PROVIDER_UNAVAILABLE` | 503 | no provider/fallback/cache path available |
| `RESPONSE_VALIDATION_FAILED` | 502 | provider output failed validation/repair |
| `UPSTREAM_TIMEOUT` | 504 | selected provider timed out |

## 4. Authentication

### `POST /auth/register`

Request:

```json
{
  "email": "user@example.com",
  "password": "minimum-12-chars",
  "display_name": "John Doe"
}
```

Response `201 Created`:

```json
{
  "user": {
    "id": "550e8400-e29b-41d4-a716-446655440000",
    "email": "user@example.com",
    "display_name": "John Doe",
    "role": "developer",
    "subscription_tier": "free"
  },
  "access_token": "jwt",
  "refresh_token": "jwt",
  "expires_in": 3600,
  "token_type": "Bearer"
}
```

### `POST /auth/login`

Request:

```json
{"email": "user@example.com", "password": "secure_password"}
```

Response `200 OK` matches the token response above.

### `POST /auth/refresh`

Request:

```json
{"refresh_token": "jwt"}
```

Response `200 OK`:

```json
{
  "access_token": "jwt",
  "refresh_token": "rotated-jwt",
  "expires_in": 3600,
  "token_type": "Bearer"
}
```

Refresh tokens are single-use. Reuse returns `401 UNAUTHORIZED` and revokes the token family.

### API Keys

API keys are passed as bearer tokens. `POST /api-keys` returns the raw key once.

```json
{
  "name": "prod-proxy",
  "scopes": ["llm:invoke", "usage:read"],
  "expires_at": "2027-06-30T00:00:00Z"
}
```

## 5. Core LLM Middleware API

### `POST /chat/completions`

Creates a non-streaming improved LLM response. This is the primary middleware endpoint.

Request:

```json
{
  "conversation_id": "optional-existing-conversation-id",
  "messages": [
    {"role": "system", "content": "Answer clearly and cite assumptions."},
    {"role": "user", "content": "Explain database indexing to a junior developer."}
  ],
  "intent": "explanation",
  "response_format": {
    "type": "text"
  },
  "routing": {
    "mode": "balanced",
    "allowed_providers": ["anthropic", "openai", "google", "ollama"],
    "preferred_model": null,
    "allow_fallback": true,
    "allow_cache": true
  },
  "optimization": {
    "sanitize_prompt": true,
    "compress_context": true,
    "dedupe_instructions": true,
    "add_clarifying_context": true,
    "quality_target": 0.85
  },
  "budget": {
    "max_cost": 0.25,
    "max_input_tokens": 20000,
    "max_output_tokens": 4096,
    "hard_limit": true
  },
  "metadata": {
    "app": "customer-support",
    "user_segment": "free"
  }
}
```

Validation:

| Field | Rule |
| --- | --- |
| `messages` | required, 1-200 messages |
| `messages[].role` | `system`, `user`, `assistant`, `tool` |
| `messages[].content` | required, 1-200000 chars |
| `intent` | optional: `chat`, `coding`, `analysis`, `explanation`, `summarization`, `translation`, `extraction`, `creative`, `classification` |
| `routing.mode` | `cheap`, `fast`, `balanced`, `quality`, `local`, `custom` |
| `budget.max_output_tokens` | default `8192`, max determined by selected model |

Response `200 OK`:

```json
{
  "id": "cmp_01j2example",
  "conversation_id": "conv_01j2example",
  "object": "chat.completion",
  "created_at": "2026-06-30T00:00:00Z",
  "model": "claude-sonnet-4-20250514",
  "provider": "anthropic",
  "routing_decision": {
    "mode": "balanced",
    "reason": "moderate complexity; best quality/cost score",
    "confidence": 0.88,
    "fallbacks": [
      {"provider": "openai", "model": "gpt-4o"},
      {"provider": "google", "model": "gemini-2.5-pro"}
    ]
  },
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Database indexes are lookup structures that help the database find rows faster..."
      },
      "finish_reason": "stop"
    }
  ],
  "quality": {
    "score": 0.91,
    "checks": {
      "schema_valid": true,
      "instruction_following": 0.94,
      "clarity": 0.9,
      "completeness": 0.89,
      "safety": 1.0
    },
    "repair_attempts": 0
  },
  "optimization": {
    "cache_hit": false,
    "prompt_tokens_removed": 318,
    "compression_applied": true,
    "sanitization_flags": []
  },
  "usage": {
    "input_tokens": 1240,
    "output_tokens": 420,
    "total_tokens": 1660,
    "cost": 0.01002,
    "estimated_savings": 0.0048
  }
}
```

### `POST /chat/completions/stream`

Same request as `/chat/completions`, but returns SSE.

Events:

| Event | Payload |
| --- | --- |
| `routing_decision` | selected provider/model/reason/fallbacks |
| `optimization_applied` | sanitization, compression, cache status |
| `token` | incremental assistant content |
| `quality_check` | final quality score and validation result |
| `usage` | final token/cost usage |
| `done` | completion ID and finish reason |
| `error` | standard error object |

### `POST /responses/validate`

Validates an LLM response against expected format and quality rules without making a provider call.

Request:

```json
{
  "input_messages": [
    {"role": "user", "content": "Return JSON with name and age."}
  ],
  "response": "{\"name\":\"Asha\",\"age\":30}",
  "response_format": {
    "type": "json_schema",
    "json_schema": {
      "type": "object",
      "required": ["name", "age"],
      "properties": {
        "name": {"type": "string"},
        "age": {"type": "number"}
      }
    }
  },
  "quality_target": 0.85
}
```

Response:

```json
{
  "valid": true,
  "quality": {
    "score": 0.93,
    "schema_valid": true,
    "instruction_following": 0.95,
    "clarity": 0.9,
    "safety": 1.0
  },
  "violations": [],
  "suggested_repair": null
}
```

### `POST /responses/repair`

Attempts to repair malformed or low-quality output. It may use a cheap model when deterministic repair fails.

Request:

```json
{
  "original_response": "name: Asha, age: thirty",
  "response_format": {"type": "json"},
  "repair_mode": "deterministic_first",
  "max_repair_attempts": 2
}
```

Response:

```json
{
  "repaired": true,
  "response": "{\"name\":\"Asha\",\"age\":30}",
  "repair_attempts": 1,
  "method": "llm_repair",
  "quality_score": 0.9
}
```

## 6. Prompt And Context APIs

### `POST /prompts/optimize`

Optimizes a prompt before a provider call.

```json
{
  "messages": [
    {"role": "user", "content": "Explain this. Explain this. Make it short."}
  ],
  "target_model": "claude-sonnet-4-20250514",
  "options": {
    "dedupe": true,
    "sanitize": true,
    "compress": true,
    "target_tokens": 4000
  }
}
```

Response:

```json
{
  "messages": [
    {"role": "user", "content": "Explain this briefly."}
  ],
  "input_tokens_before": 14,
  "input_tokens_after": 5,
  "tokens_removed": 9,
  "sanitization_flags": [],
  "cache_key": "prompt:cache:abc123"
}
```

### `POST /context/compress`

Compresses a long conversation while preserving system instructions and recent turns.

```json
{
  "messages": [],
  "target_tokens": 12000,
  "preserve_last_messages": 10
}
```

## 7. Routing And Provider APIs

### `POST /routing/decide`

Returns the provider/model decision without invoking the LLM.

```json
{
  "messages": [{"role": "user", "content": "Summarize this contract."}],
  "intent": "summarization",
  "mode": "balanced",
  "budget": {"max_cost": 0.10},
  "required_capabilities": ["long_context"]
}
```

Response:

```json
{
  "provider": "anthropic",
  "model": "claude-sonnet-4-20250514",
  "complexity": "moderate",
  "estimated_cost": 0.043,
  "estimated_latency_ms": 1800,
  "confidence": 0.86,
  "reason": "best quality/cost score for summarization with long context",
  "fallbacks": [
    {"provider": "openai", "model": "gpt-4o", "estimated_cost": 0.052}
  ]
}
```

### `GET /providers`

Lists configured providers and models, excluding secrets.

### `GET /providers/health`

Returns health, latency, error rate, and circuit-breaker state.

```json
{
  "providers": [
    {
      "provider": "anthropic",
      "status": "healthy",
      "latency_p95_ms": 2200,
      "error_rate": 0.003,
      "circuit_state": "closed",
      "last_checked_at": "2026-06-30T00:00:00Z"
    }
  ]
}
```

## 8. Cache APIs

### `GET /cache/stats`

```json
{
  "prompt_cache_hit_rate": 0.42,
  "semantic_cache_hit_rate": 0.18,
  "estimated_savings": 34.22,
  "entries": 1842
}
```

### `DELETE /cache/entries/{cache_key}`

Deletes a cache entry owned by the caller/org.

### `POST /cache/invalidate`

Invalidates by model, tenant, namespace, or date range.

```json
{"model": "claude-sonnet-4-20250514", "reason": "model_updated"}
```

## 9. Conversations

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/conversations` | list conversation metadata |
| `GET` | `/conversations/{conversation_id}` | get messages and usage |
| `DELETE` | `/conversations/{conversation_id}` | soft-delete conversation |

Conversation storage exists to preserve user context across calls. It must not imply autonomous task execution.

## 10. Usage, Budgets, And Billing

### `GET /usage`

Query params: `period=24h|7d|30d|month`, `group_by=model|provider|app|day`.

Response:

```json
{
  "period": {"start": "2026-06-01T00:00:00Z", "end": "2026-06-30T23:59:59Z"},
  "requests": 1200,
  "tokens": {"input": 900000, "output": 420000, "total": 1320000},
  "cost": {"total": 42.31, "estimated_savings": 18.70},
  "quality": {"average_score": 0.88, "validation_failure_rate": 0.021},
  "cache": {"hit_rate": 0.31}
}
```

### `GET /budgets`

Returns active user/org/app budgets.

### `POST /budgets`

Creates a budget.

```json
{
  "scope": "user",
  "scope_id": "550e8400-e29b-41d4-a716-446655440000",
  "period": "monthly",
  "limit": 50.0,
  "alert_threshold": 0.8,
  "hard_limit": true
}
```

## 11. Feedback And Quality Learning

### `POST /feedback`

Records user/application feedback for future routing and quality evaluation.

```json
{
  "completion_id": "cmp_01j2example",
  "rating": 4,
  "label": "helpful",
  "comment": "Clear answer, but needed more examples.",
  "expected_output": null
}
```

Response:

```json
{"id": "fb_01j2example", "recorded_at": "2026-06-30T00:00:00Z"}
```

## 12. Webhooks

Webhook events:

| Event | Meaning |
| --- | --- |
| `completion.created` | LLM middleware request completed |
| `completion.failed` | all provider/cache paths failed |
| `budget.warning` | soft budget threshold crossed |
| `budget.exceeded` | hard budget blocked request |
| `provider.degraded` | provider health changed |
| `quality.validation_failed` | response failed schema/quality checks |

Webhook deliveries include `VigilAgent-Signature: sha256=<hmac>` and `VigilAgent-Timestamp`.

## 13. Rate Limits And Quotas

Monthly task/request limits follow the product pricing docs. Rate limits are burst controls, not billing quotas.

| Tier | Request burst | Monthly middleware requests | Notes |
| --- | ---: | ---: | --- |
| free | 60/min | 100 | aligned with free plan |
| pro | 300/min | 2000 | aligned with pro plan |
| team | 600/min | 5000 per seat | aligned with team plan |
| enterprise | custom | custom | contract-specific |

## 14. Go Route Map

```go
r.Route("/v1", func(r chi.Router) {
    r.Get("/health", healthHandler.Check)
    r.Post("/auth/register", authHandler.Register)
    r.Post("/auth/login", authHandler.Login)
    r.Post("/auth/refresh", authHandler.Refresh)

    r.Group(func(r chi.Router) {
        r.Use(auth.Middleware())
        r.Use(rateLimiter.Middleware())

        r.Post("/chat/completions", completionHandler.Create)
        r.Post("/chat/completions/stream", completionHandler.Stream)
        r.Post("/responses/validate", responseHandler.Validate)
        r.Post("/responses/repair", responseHandler.Repair)
        r.Post("/prompts/optimize", promptHandler.Optimize)
        r.Post("/context/compress", contextHandler.Compress)
        r.Post("/routing/decide", routingHandler.Decide)
        r.Get("/providers", providerHandler.List)
        r.Get("/providers/health", providerHandler.Health)
        r.Get("/cache/stats", cacheHandler.Stats)
        r.Delete("/cache/entries/{key}", cacheHandler.Delete)
        r.Post("/cache/invalidate", cacheHandler.Invalidate)
        r.Get("/conversations", conversationHandler.List)
        r.Get("/conversations/{id}", conversationHandler.Get)
        r.Delete("/conversations/{id}", conversationHandler.Delete)
        r.Get("/usage", usageHandler.Summary)
        r.Get("/budgets", budgetHandler.List)
        r.Post("/budgets", budgetHandler.Create)
        r.Post("/feedback", feedbackHandler.Create)
        r.Post("/webhooks", webhookHandler.Create)
        r.Get("/webhooks", webhookHandler.List)
        r.Delete("/webhooks/{id}", webhookHandler.Delete)
    })
})
```

## 15. Compatibility Policy

Non-breaking changes include adding optional request fields, response fields, new routes, or new enum values. Breaking changes require `/v2`, including field removals, type changes, stricter validation on existing fields, or authentication semantics changes.