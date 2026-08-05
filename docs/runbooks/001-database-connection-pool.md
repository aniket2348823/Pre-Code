# Runbook: Database Connection Pool Issues

## Symptoms
- Increased latency on API requests
- "too many connections" errors in logs
- Connection timeout errors
- Circuit breaker OPEN state in logs

## Diagnosis

### 1. Check Current Pool Stats
```bash
# Via Prometheus metrics
curl http://localhost:9090/api/v1/query?query=vigilagent_db_pool_open_connections

# Via API endpoint
curl -H "Authorization: Bearer <token>" http://localhost:8080/api/v1/admin/db/pool-stats
```

### 2. Check PostgreSQL Active Connections
```sql
SELECT count(*), state
FROM pg_stat_activity
WHERE datname = 'vigilagent'
GROUP BY state;
```

### 3. Check for Long-Running Queries
```sql
SELECT pid, now() - pg_stat_activity.query_start AS duration, query, state
FROM pg_stat_activity
WHERE (now() - pg_stat_activity.query_start) > interval '5 minutes'
AND datname = 'vigilagent';
```

### 4. Check Circuit Breaker State
```bash
grep -i "circuit breaker" /var/log/vigilagent/*.log | tail -20
```

## Resolution

### High Connection Count
1. Check for connection leaks (unclosed rows/connections)
2. Reduce `database.pool_max_open` if below hardware limits
3. Kill idle connections: `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle' AND datname = 'vigilagent';`

### Circuit Breaker Open
1. Check PostgreSQL health: `pg_isready -h localhost -p 5432`
2. Check network connectivity
3. If DB is healthy, restart the application
4. If DB is down, failover to read replica

### Connection Pool Exhaustion
1. Increase `database.pool_max_open` (max recommended: 50 per CPU core)
2. Reduce `database.pool_max_idle_time` to reclaim idle connections faster
3. Enable connection pooling proxy (PgBouncer/Supabase)

## Prevention
- Monitor `vigilagent_db_pool_*` metrics
- Set up alerts for pool utilization > 80%
- Regular connection pool tuning based on load patterns
