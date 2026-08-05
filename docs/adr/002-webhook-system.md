# ADR-002: Webhook Delivery System with Circuit Breaker

## Status
Accepted

## Date
2024-02-01

## Context
VigilAgent needs to notify external systems about events (scan completion, alerts, budget thresholds). The system must be reliable, handle failures gracefully, and prevent cascade failures.

## Decision
Implement a webhook delivery system with:

### Core Features
- **DB-backed storage**: PostgreSQL for endpoints and delivery history
- **HMAC-SHA256 signatures**: For payload verification
- **SSRF protection**: DNS resolution + private IP blocking
- **Event filtering**: Per-endpoint event type subscriptions

### Reliability Features
- **Circuit Breaker**: Per-endpoint circuit breaker (5 failures → open, 30s reset → half-open)
- **Dead Letter Queue (DLQ)**: Failed deliveries queued for manual retry
- **Exponential backoff**: 1s, 2s, 4s retry delays
- **Response body limits**: 1KB max to prevent OOM

### Monitoring
- Delivery success/failure rates via Prometheus
- DLQ size and age tracking
- Circuit breaker state changes logged

## Consequences

### Positive
- Circuit breaker prevents cascade failures when endpoints are down
- DLQ ensures no events are permanently lost
- HMAC signatures prevent webhook spoofing
- SSRF protection prevents abuse

### Negative
- Circuit breaker state is per-engine instance (not distributed)
- DLQ retry logic adds complexity
- HMAC signature verification adds latency to endpoint registration

## Alternatives Considered
1. **Message queue (NATS/Kafka)**: Rejected - adds operational complexity for webhook-specific features
2. **Third-party service (Svix)**: Rejected - vendor lock-in, cost
3. **Fire-and-forget**: Rejected - no delivery guarantees

## References
- Circuit Breaker Pattern: https://martinfowler.com/bliki/CircuitBreaker.html
- Webhook best practices: https://docs.svix.com/receiving/introduction
