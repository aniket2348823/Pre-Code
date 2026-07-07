-- VigilAgent Supabase Monitoring & Observability
-- Run this AFTER migrations and RLS policies

-- =====================================================
-- 1. Database Performance Views
-- =====================================================

-- Active connections view
CREATE OR REPLACE VIEW v_active_connections AS
SELECT 
    pid,
    usename,
    application_name,
    client_addr,
    client_port,
    backend_start,
    state,
    query,
    query_start,
    state_change,
    NOW() - query_start AS query_duration
FROM pg_stat_activity 
WHERE datname = current_database()
  AND state != 'idle'
ORDER BY query_start;

-- Table sizes view
CREATE OR REPLACE VIEW v_table_sizes AS
SELECT 
    schemaname,
    tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||tablename)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||tablename)) AS table_size,
    pg_size_pretty(pg_indexes_size((schemaname||'.'||tablename)::regclass)) AS index_size,
    n_live_tup AS row_count
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(schemaname||'.'||tablename) DESC;

-- Index usage view
CREATE OR REPLACE VIEW v_index_usage AS
SELECT 
    schemaname,
    tablename,
    indexrelname,
    idx_scan AS times_used,
    idx_tup_read AS tuples_read,
    idx_tup_fetch AS tuples_fetched,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

-- =====================================================
-- 2. Application Monitoring Views
-- =====================================================

-- Cost per organization (last 24h)
CREATE OR REPLACE VIEW v_org_cost_24h AS
SELECT 
    p.org_id,
    o.name AS org_name,
    COUNT(e.id) AS event_count,
    SUM(e.tokens_used) AS total_tokens,
    SUM(e.cost_usd) AS total_cost_usd,
    AVG(e.latency_ms) AS avg_latency_ms
FROM events e
JOIN sessions s ON s.id = e.session_id
JOIN projects p ON p.id = s.project_id
JOIN organizations o ON o.id = p.org_id
WHERE e.created_at > NOW() - INTERVAL '24 hours'
GROUP BY p.org_id, o.name
ORDER BY total_cost_usd DESC;

-- Task completion rates
CREATE OR REPLACE VIEW v_task_stats AS
SELECT 
    p.org_id,
    o.name AS org_name,
    COUNT(t.id) AS total_tasks,
    COUNT(t.id) FILTER (WHERE t.status = 'completed') AS completed,
    COUNT(t.id) FILTER (WHERE t.status = 'failed') AS failed,
    COUNT(t.id) FILTER (WHERE t.status = 'pending') AS pending,
    ROUND(
        COUNT(t.id) FILTER (WHERE t.status = 'completed')::DECIMAL / 
        NULLIF(COUNT(t.id), 0) * 100, 2
    ) AS completion_rate_pct,
    AVG(t.total_tokens) AS avg_tokens_per_task,
    AVG(t.cost) AS avg_cost_per_task
FROM tasks t
JOIN projects p ON p.id = t.project_id
JOIN organizations o ON o.id = p.org_id
WHERE t.created_at > NOW() - INTERVAL '7 days'
GROUP BY p.org_id, o.name
ORDER BY total_tasks DESC;

-- Top agents by usage
CREATE OR REPLACE VIEW v_top_agents AS
SELECT 
    a.name AS agent_name,
    p.name AS project_name,
    o.name AS org_name,
    COUNT(s.id) AS session_count,
    COUNT(e.id) AS event_count,
    SUM(e.tokens_used) AS total_tokens,
    SUM(e.cost_usd) AS total_cost_usd
FROM agents a
JOIN projects p ON p.id = a.project_id
JOIN organizations o ON o.id = p.org_id
LEFT JOIN sessions s ON s.agent_id = a.id
LEFT JOIN events e ON e.session_id = s.id
WHERE e.created_at > NOW() - INTERVAL '30 days'
GROUP BY a.id, a.name, p.name, o.name
ORDER BY total_cost_usd DESC
LIMIT 20;

-- =====================================================
-- 3. Health Check Function
-- =====================================================

-- Function to check database health
CREATE OR REPLACE FUNCTION check_db_health()
RETURNS TABLE(
    component TEXT,
    status TEXT,
    details TEXT,
    checked_at TIMESTAMPTZ
) AS $$
BEGIN
    -- Check if tables exist
    RETURN QUERY
    SELECT 
        'tables'::TEXT,
        CASE WHEN COUNT(*) > 0 THEN 'healthy' ELSE 'degraded' END::TEXT,
        COUNT(*)::TEXT || ' tables found'::TEXT,
        NOW()
    FROM information_schema.tables 
    WHERE table_schema = 'public';
    
    -- Check pgvector extension
    RETURN QUERY
    SELECT 
        'pgvector'::TEXT,
        CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector') 
             THEN 'healthy' ELSE 'degraded' END::TEXT,
        CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector') 
             THEN 'extension installed' ELSE 'extension missing' END::TEXT,
        NOW();
    
    -- Check recent activity
    RETURN QUERY
    SELECT 
        'recent_activity'::TEXT,
        CASE WHEN COUNT(*) > 0 THEN 'healthy' ELSE 'degraded' END::TEXT,
        COUNT(*)::TEXT || ' events in last hour'::TEXT,
        NOW()
    FROM events 
    WHERE created_at > NOW() - INTERVAL '1 hour';
    
    -- Check connection count
    RETURN QUERY
    SELECT 
        'connections'::TEXT,
        CASE WHEN COUNT(*) < 100 THEN 'healthy' ELSE 'degraded' END::TEXT,
        COUNT(*)::TEXT || ' active connections'::TEXT,
        NOW()
    FROM pg_stat_activity 
    WHERE datname = current_database() 
      AND state = 'active';
END;
$$ LANGUAGE plpgsql;

-- =====================================================
-- 4. Webhook Configuration Tables (DB-backed persistence)
-- These tables back the webhook engine in internal/webhook/webhook.go.
-- Endpoints and delivery results are stored here for persistence across restarts.
-- =====================================================

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    url TEXT NOT NULL,
    secret TEXT,
    events JSONB NOT NULL DEFAULT '["*"]',
    is_active BOOLEAN DEFAULT true,
    last_triggered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE INDEX idx_webhook_endpoints_active ON webhook_endpoints(is_active);

-- Webhook delivery log
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB,
    status_code INTEGER,
    success BOOLEAN DEFAULT false,
    error TEXT,
    duration_ms INTEGER,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE INDEX idx_webhook_deliveries_endpoint ON webhook_deliveries(endpoint_id);
CREATE INDEX idx_webhook_deliveries_created ON webhook_deliveries(created_at);

-- Trigger for webhook endpoint updated_at
CREATE TRIGGER update_webhook_endpoints_updated_at 
    BEFORE UPDATE ON webhook_endpoints 
    FOR EACH ROW 
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- 5. RLS for new tables
-- =====================================================
ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;

-- Webhook endpoints: admin only
    FOR ALL USING (
        EXISTS (
            SELECT 1 FROM users 
            WHERE id = app_auth.current_user_id() AND role = 'admin'
        )
    );
-- Webhook deliveries: admin only
    FOR ALL USING (
        EXISTS (
            SELECT 1 FROM users 
            WHERE id = app_auth.current_user_id() AND role = 'admin'
        )
    );

-- =====================================================
-- DONE! Monitoring and webhooks configured
-- =====================================================
