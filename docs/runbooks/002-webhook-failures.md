# Runbook: Webhook Delivery Failures

## Symptoms
- Webhook deliveries failing in logs
- DLQ (Dead Letter Queue) growing
- Circuit breaker OPEN for webhook endpoints
- Users reporting missing notifications

## Diagnosis

### 1. Check Webhook Stats
```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/webhooks/stats
```

### 2. Check DLQ Size
```bash
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/webhooks/dlq
```

### 3. Check Circuit Breaker State
```bash
grep -i "circuit breaker" /var/log/vigilagent/*.log | tail -20
```

### 4. Check Endpoint Health
```sql
SELECT id, url, is_active, last_triggered_at
FROM webhook_endpoints
WHERE is_active = true
ORDER BY last_triggered_at DESC NULLS LAST;
```

## Resolution

### All Endpoints Failing
1. Check outbound network connectivity
2. Check firewall rules (port 443 for HTTPS)
3. Verify DNS resolution is working
4. Check for IP blocking by target servers

### Specific Endpoint Failing
1. Verify endpoint URL is valid and reachable
2. Check if endpoint returns 4xx/5xx errors
3. Verify HMAC signature is being verified correctly
4. Check if endpoint is rate-limiting VigilAgent

### Circuit Breaker Open
1. Wait for automatic recovery (30s default)
2. If persistent, check target endpoint health
3. Manual reset: restart the webhook service
4. Consider increasing threshold for high-traffic endpoints

### DLQ Growing
1. Review DLQ items for patterns (same endpoint, same error)
2. Retry DLQ: `POST /api/v1/webhooks/dlq/retry`
3. Clear stuck items: `DELETE /api/v1/webhooks/dlq/{id}`
4. Investigate root cause before bulk retry

## Prevention
- Monitor webhook delivery success rate
- Set up alerts for DLQ size > 100
- Regular endpoint health checks
- Circuit breaker state logging
