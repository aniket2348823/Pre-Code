-- ================================================================
-- VigilAgent — Full Supabase Setup Script
-- Run this in Supabase SQL Editor AFTER creating the database.
-- Run each section separately if the editor has statement limits.
-- ================================================================

-- Required extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ================================================================
-- SECTION 1: App Auth Schema (for custom JWT ↔ RLS bridge)
-- ================================================================

-- Create app_auth schema for RLS helper functions
CREATE SCHEMA IF NOT EXISTS app_auth;

-- Set the current user ID for the session (called by Go auth middleware)
CREATE OR REPLACE FUNCTION app_auth.set_current_user_id(user_id TEXT)
RETURNS VOID AS $$
BEGIN
    PERFORM set_config('app.current_user_id', user_id, true);
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Get the current user ID (used by RLS policies) — returns UUID
CREATE OR REPLACE FUNCTION app_auth.current_user_id()
RETURNS UUID AS $$
BEGIN
    RETURN current_setting('app.current_user_id', true)::UUID;
END;
$$ LANGUAGE plpgsql STABLE;

-- Check if user is a member of an org
CREATE OR REPLACE FUNCTION app_auth.is_org_member(org_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM organizations WHERE id = org_id AND owner_id = app_auth.current_user_id()
        UNION
        SELECT 1 FROM organization_members WHERE organization_id = org_id AND user_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Check if user is owner of an org
CREATE OR REPLACE FUNCTION app_auth.is_org_owner(org_id UUID)
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM organizations WHERE id = org_id AND owner_id = app_auth.current_user_id()
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- Check if user is admin
CREATE OR REPLACE FUNCTION app_auth.is_admin()
RETURNS BOOLEAN AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM users WHERE id = app_auth.current_user_id() AND role = 'admin'
    );
END;
$$ LANGUAGE plpgsql STABLE SECURITY DEFINER;

-- ================================================================
-- SECTION 1b: Webhook Tables (must exist before RLS)
-- ================================================================

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    url TEXT NOT NULL,
    secret TEXT,
    events JSONB NOT NULL DEFAULT '["*"]'::jsonb,
    is_active BOOLEAN DEFAULT true,
    last_triggered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_active ON webhook_endpoints(is_active);

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

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_endpoint ON webhook_deliveries(endpoint_id);
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_created ON webhook_deliveries(created_at);

-- ================================================================
-- SECTION 2: RLS Policies
-- ================================================================

-- Enable RLS on all tables
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE events ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE skills ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_ratings ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_installs ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_usage ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_episodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_patterns ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;

-- ── Users ──────────────────────────────────────────────────
-- Can read own profile, admins can read all
CREATE POLICY users_select ON users FOR SELECT
    USING (id = app_auth.current_user_id() OR app_auth.is_admin());

-- ── Organizations ───────────────────────────────────────────
-- Members can read, owners can modify
CREATE POLICY orgs_select ON organizations FOR SELECT
    USING (app_auth.is_org_member(id));

CREATE POLICY orgs_insert ON organizations FOR INSERT
    WITH CHECK (owner_id = app_auth.current_user_id());

CREATE POLICY orgs_update ON organizations FOR UPDATE
    USING (app_auth.is_org_owner(id));

CREATE POLICY orgs_delete ON organizations FOR DELETE
    USING (app_auth.is_org_owner(id));

-- ── Organization Members ────────────────────────────────────
-- Org members can read membership list
CREATE POLICY org_members_select ON organization_members FOR SELECT
    USING (app_auth.is_org_member(organization_id));

CREATE POLICY org_members_insert ON organization_members FOR INSERT
    WITH CHECK (app_auth.is_org_owner(organization_id));

-- ── Projects ────────────────────────────────────────────────
-- Org members can read/write
CREATE POLICY projects_select ON projects FOR SELECT
    USING (app_auth.is_org_member(org_id));

CREATE POLICY projects_insert ON projects FOR INSERT
    WITH CHECK (app_auth.is_org_member(org_id));

CREATE POLICY projects_update ON projects FOR UPDATE
    USING (app_auth.is_org_member(org_id));

CREATE POLICY projects_delete ON projects FOR DELETE
    USING (app_auth.is_org_member(org_id));

-- ── Agents ──────────────────────────────────────────────────
-- Via project → org membership
CREATE POLICY agents_select ON agents FOR SELECT
    USING (EXISTS (SELECT 1 FROM projects WHERE id = agents.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY agents_insert ON agents FOR INSERT
    WITH CHECK (EXISTS (SELECT 1 FROM projects WHERE id = agents.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY agents_update ON agents FOR UPDATE
    USING (EXISTS (SELECT 1 FROM projects WHERE id = agents.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY agents_delete ON agents FOR DELETE
    USING (EXISTS (SELECT 1 FROM projects WHERE id = agents.project_id AND app_auth.is_org_member(org_id)));

-- ── Sessions ────────────────────────────────────────────────
-- Via project → org membership
CREATE POLICY sessions_select ON sessions FOR SELECT
    USING (EXISTS (SELECT 1 FROM projects WHERE id = sessions.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY sessions_insert ON sessions FOR INSERT
    WITH CHECK (EXISTS (SELECT 1 FROM projects WHERE id = sessions.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY sessions_update ON sessions FOR UPDATE
    USING (EXISTS (SELECT 1 FROM projects WHERE id = sessions.project_id AND app_auth.is_org_member(org_id)));

-- ── Events ──────────────────────────────────────────────────
-- Via session → project → org membership
CREATE POLICY events_select ON events FOR SELECT
    USING (EXISTS (
        SELECT 1 FROM sessions s
        JOIN projects p ON p.id = s.project_id
        WHERE s.id = events.session_id AND app_auth.is_org_member(p.org_id)
    ));

CREATE POLICY events_insert ON events FOR INSERT
    WITH CHECK (EXISTS (
        SELECT 1 FROM sessions s
        JOIN projects p ON p.id = s.project_id
        WHERE s.id = events.session_id AND app_auth.is_org_member(p.org_id)
    ));

-- ── Tasks ───────────────────────────────────────────────────
-- Via project → org membership
CREATE POLICY tasks_select ON tasks FOR SELECT
    USING (EXISTS (SELECT 1 FROM projects WHERE id = tasks.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY tasks_insert ON tasks FOR INSERT
    WITH CHECK (EXISTS (SELECT 1 FROM projects WHERE id = tasks.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY tasks_update ON tasks FOR UPDATE
    USING (EXISTS (SELECT 1 FROM projects WHERE id = tasks.project_id AND app_auth.is_org_member(org_id)));

-- ── API Keys ────────────────────────────────────────────────
-- Own keys only, admins can read all
CREATE POLICY api_keys_select ON api_keys FOR SELECT
    USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

CREATE POLICY api_keys_insert ON api_keys FOR INSERT
    WITH CHECK (user_id = app_auth.current_user_id());

CREATE POLICY api_keys_delete ON api_keys FOR DELETE
    USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

-- ── Skills ──────────────────────────────────────────────────
-- All authenticated users can read, authors can modify
CREATE POLICY skills_select ON skills FOR SELECT
    USING (app_auth.current_user_id() IS NOT NULL);

CREATE POLICY skills_insert ON skills FOR INSERT
    WITH CHECK (author_id = app_auth.current_user_id());

CREATE POLICY skills_update ON skills FOR UPDATE
    USING (author_id = app_auth.current_user_id() OR app_auth.is_admin());

CREATE POLICY skills_delete ON skills FOR DELETE
    USING (author_id = app_auth.current_user_id() OR app_auth.is_admin());

-- ── Skill Ratings ───────────────────────────────────────────
-- All authenticated users can read, own ratings for insert
CREATE POLICY skill_ratings_select ON skill_ratings FOR SELECT
    USING (app_auth.current_user_id() IS NOT NULL);

CREATE POLICY skill_ratings_insert ON skill_ratings FOR INSERT
    WITH CHECK (user_id = app_auth.current_user_id());

-- ── Skill Installs ──────────────────────────────────────────
-- Users can see their own installs
CREATE POLICY skill_installs_select ON skill_installs FOR SELECT
    USING (user_id = app_auth.current_user_id());

CREATE POLICY skill_installs_insert ON skill_installs FOR INSERT
    WITH CHECK (user_id = app_auth.current_user_id());

-- ── Alerts ──────────────────────────────────────────────────
-- Alerts are owned by user_id (no project_id exists on this table)
CREATE POLICY alerts_select ON alerts FOR SELECT
    USING (user_id = app_auth.current_user_id());

CREATE POLICY alerts_insert ON alerts FOR INSERT
    WITH CHECK (user_id = app_auth.current_user_id());

CREATE POLICY alerts_update ON alerts FOR UPDATE
    USING (user_id = app_auth.current_user_id());

CREATE POLICY alerts_delete ON alerts FOR DELETE
    USING (user_id = app_auth.current_user_id());

-- ── Invoices ────────────────────────────────────────────────
-- Via organization membership
CREATE POLICY invoices_select ON invoices FOR SELECT
    USING (app_auth.is_org_member(organization_id));

CREATE POLICY invoices_insert ON invoices FOR INSERT
    WITH CHECK (app_auth.is_org_owner(organization_id));

-- ── Subscriptions ───────────────────────────────────────────
-- Via organization membership
CREATE POLICY subscriptions_select ON subscriptions FOR SELECT
    USING (app_auth.is_org_member(organization_id));

CREATE POLICY subscriptions_insert ON subscriptions FOR INSERT
    WITH CHECK (app_auth.is_org_owner(organization_id));

CREATE POLICY subscriptions_update ON subscriptions FOR UPDATE
    USING (app_auth.is_org_owner(organization_id));

-- ── Budget Usage ────────────────────────────────────────────
-- Admins only
CREATE POLICY budget_usage_select ON budget_usage FOR SELECT
    USING (app_auth.is_admin());

CREATE POLICY budget_usage_insert ON budget_usage FOR INSERT
    WITH CHECK (app_auth.is_admin());

-- ── Memory Episodes ─────────────────────────────────────────
-- Own memories only
CREATE POLICY memory_episodes_select ON memory_episodes FOR SELECT
    USING (user_id = app_auth.current_user_id());

CREATE POLICY memory_episodes_insert ON memory_episodes FOR INSERT
    WITH CHECK (user_id = app_auth.current_user_id());

-- ── Memory Patterns ─────────────────────────────────────────
-- Via project → org membership
CREATE POLICY memory_patterns_select ON memory_patterns FOR SELECT
    USING (EXISTS (SELECT 1 FROM projects WHERE id = memory_patterns.project_id AND app_auth.is_org_member(org_id)));

CREATE POLICY memory_patterns_insert ON memory_patterns FOR INSERT
    WITH CHECK (EXISTS (SELECT 1 FROM projects WHERE id = memory_patterns.project_id AND app_auth.is_org_member(org_id)));

-- ── Webhook Endpoints ───────────────────────────────────────
-- Admins only
CREATE POLICY webhook_endpoints_admin ON webhook_endpoints
    FOR ALL USING (app_auth.is_admin());

-- ── Webhook Deliveries ──────────────────────────────────────
-- Admins only
CREATE POLICY webhook_deliveries_admin ON webhook_deliveries
    FOR ALL USING (app_auth.is_admin());

-- ================================================================
-- SECTION 3: Monitoring Views
-- ================================================================

-- Active connections view
CREATE OR REPLACE VIEW v_active_connections AS
SELECT
    pid,
    usename,
    application_name,
    client_addr,
    state,
    query,
    query_start,
    NOW() - query_start AS query_duration
FROM pg_stat_activity
WHERE datname = current_database()
  AND state != 'idle'
ORDER BY query_start;

-- Table sizes view
CREATE OR REPLACE VIEW v_table_sizes AS
SELECT
    schemaname,
    relname AS tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||relname)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||relname)) AS table_size,
    pg_size_pretty(pg_indexes_size((schemaname||'.'||relname)::regclass)) AS index_size,
    n_live_tup AS row_count
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(schemaname||'.'||relname) DESC;

-- Index usage view
CREATE OR REPLACE VIEW v_index_usage AS
SELECT
    schemaname,
    relname AS tablename,
    indexrelname,
    idx_scan AS times_used,
    idx_tup_read AS tuples_read,
    idx_tup_fetch AS tuples_fetched,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

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

-- Health check function
CREATE OR REPLACE FUNCTION check_db_health()
RETURNS TABLE(
    component TEXT,
    status TEXT,
    details TEXT,
    checked_at TIMESTAMPTZ
) AS $$
BEGIN
    RETURN QUERY
    SELECT
        'tables'::TEXT,
        CASE WHEN COUNT(*) > 0 THEN 'healthy' ELSE 'degraded' END::TEXT,
        COUNT(*)::TEXT || ' tables found'::TEXT,
        NOW()
    FROM information_schema.tables
    WHERE table_schema = 'public';

    RETURN QUERY
    SELECT
        'pgvector'::TEXT,
        CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')
             THEN 'healthy' ELSE 'degraded' END::TEXT,
        CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector')
             THEN 'extension installed' ELSE 'extension missing' END::TEXT,
        NOW();

    RETURN QUERY
    SELECT
        'recent_activity'::TEXT,
        CASE WHEN COUNT(*) > 0 THEN 'healthy' ELSE 'degraded' END::TEXT,
        COUNT(*)::TEXT || ' events in last hour'::TEXT,
        NOW()
    FROM events
    WHERE created_at > NOW() - INTERVAL '1 hour';

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

-- ================================================================
