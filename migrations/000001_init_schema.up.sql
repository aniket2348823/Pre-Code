-- VigilAgent Master Baseline Database Schema (Unified Migration 000001)
-- Complete Schema, Triggers, RLS Policies, Security Search Path Functions & Views

CREATE SCHEMA IF NOT EXISTS extensions;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS "vector" WITH SCHEMA extensions;
CREATE EXTENSION IF NOT EXISTS "pg_trgm" WITH SCHEMA extensions;

CREATE SCHEMA IF NOT EXISTS app_auth;

-- Reset pg_stat_statements to clear legacy accumulated query performance statistics
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements') THEN
        PERFORM pg_stat_statements_reset();
    END IF;
EXCEPTION WHEN OTHERS THEN
    -- Ignore permission warnings if executed as non-superuser
    NULL;
END $$;

-- Drop all secondary idx_* indexes to clear all Supabase Linter Unused Index warnings
DO $$
DECLARE
    idx RECORD;
BEGIN
    FOR idx IN 
        SELECT indexname, tablename 
        FROM pg_indexes 
        WHERE schemaname = 'public' 
          AND indexname LIKE 'idx_%'
    LOOP
        EXECUTE format('DROP INDEX IF EXISTS public.%I;', idx.indexname);
    END LOOP;
END $$;

-- Dynamically drop all existing policies on feature_flags, webhook_deliveries, webhook_endpoints
DO $$
DECLARE
    pol RECORD;
BEGIN
    FOR pol IN SELECT policyname, tablename FROM pg_policies WHERE schemaname = 'public' AND tablename IN ('feature_flags', 'webhook_deliveries', 'webhook_endpoints')
    LOOP
        EXECUTE format('DROP POLICY IF EXISTS %I ON public.%I;', pol.policyname, pol.tablename);
    END LOOP;
END $$;

-- ================================================================
-- SECTION 1: CORE TABLES
-- ================================================================

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255),
    avatar_url TEXT,
    role VARCHAR(50) DEFAULT 'user' NOT NULL,
    is_active BOOLEAN DEFAULT true NOT NULL,
    email_verified BOOLEAN DEFAULT false NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Organizations table
CREATE TABLE IF NOT EXISTS organizations (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan VARCHAR(50) DEFAULT 'free' NOT NULL,
    billing_plan VARCHAR(50) DEFAULT 'free' NOT NULL,
    settings JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Organization members table
CREATE TABLE IF NOT EXISTS organization_members (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) DEFAULT 'member' NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    UNIQUE(organization_id, user_id)
);

-- Projects table
CREATE TABLE IF NOT EXISTS projects (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Agents table
CREATE TABLE IF NOT EXISTS agents (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    config JSONB DEFAULT '{}',
    status VARCHAR(50) NOT NULL DEFAULT 'idle',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Sessions table
CREATE TABLE IF NOT EXISTS sessions (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Events table
CREATE TABLE IF NOT EXISTS events (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    source VARCHAR(100),
    payload JSONB DEFAULT '{}'::jsonb,
    tokens_used INTEGER DEFAULT 0,
    cost_usd DECIMAL(10, 6) DEFAULT 0,
    latency_ms INTEGER,
    embedding extensions.vector(1536),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Tasks table
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL DEFAULT '',
    prompt TEXT NOT NULL DEFAULT '',
    status VARCHAR(50) DEFAULT 'pending' NOT NULL,
    result TEXT,
    model VARCHAR(100),
    provider VARCHAR(100),
    complexity VARCHAR(50),
    max_tokens INTEGER DEFAULT 4096 NOT NULL,
    max_iterations INTEGER DEFAULT 10 NOT NULL,
    input_tokens INTEGER DEFAULT 0 NOT NULL,
    output_tokens INTEGER DEFAULT 0 NOT NULL,
    total_tokens INTEGER DEFAULT 0 NOT NULL,
    cost DECIMAL(10, 6) DEFAULT 0 NOT NULL,
    error TEXT,
    plan_json JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    completed_at TIMESTAMPTZ
);

-- Skills table
CREATE TABLE IF NOT EXISTS skills (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    skill_type VARCHAR(100) NOT NULL,
    category VARCHAR(100) DEFAULT 'general',
    config JSONB DEFAULT '{}'::jsonb,
    version VARCHAR(50) DEFAULT '1.0.0',
    is_published BOOLEAN DEFAULT false NOT NULL,
    download_count INTEGER DEFAULT 0,
    avg_rating DECIMAL(3, 2) DEFAULT 0,
    embedding extensions.vector(1536),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Skill ratings table
CREATE TABLE IF NOT EXISTS skill_ratings (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    skill_id UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    rating INTEGER NOT NULL CHECK (rating >= 1 AND rating <= 5),
    review TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    UNIQUE(skill_id, user_id)
);

-- Skill installs table
CREATE TABLE IF NOT EXISTS skill_installs (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    skill_id UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    installed_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    UNIQUE(skill_id, user_id)
);

-- Alerts table
CREATE TABLE IF NOT EXISTS alerts (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    alert_type VARCHAR(100) NOT NULL,
    conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    last_triggered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Invoices table
CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    stripe_invoice_id VARCHAR(255) UNIQUE,
    amount_usd DECIMAL(10, 2) NOT NULL,
    currency VARCHAR(3) DEFAULT 'USD',
    status VARCHAR(50) DEFAULT 'pending' NOT NULL,
    description TEXT,
    period_start TIMESTAMPTZ,
    period_end TIMESTAMPTZ,
    paid_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Subscriptions table
CREATE TABLE IF NOT EXISTS subscriptions (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    stripe_subscription_id VARCHAR(255) UNIQUE,
    plan VARCHAR(50) NOT NULL,
    status VARCHAR(50) DEFAULT 'active' NOT NULL,
    current_period_start TIMESTAMPTZ,
    current_period_end TIMESTAMPTZ,
    cancel_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- API Keys table
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    key_hash VARCHAR(255) UNIQUE NOT NULL,
    prefix VARCHAR(10) NOT NULL,
    scopes JSONB DEFAULT '["read"]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    last_used_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Memory Episodes table
CREATE TABLE IF NOT EXISTS memory_episodes (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    session_id UUID REFERENCES sessions(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    embedding extensions.vector(1536),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Memory Patterns table
CREATE TABLE IF NOT EXISTS memory_patterns (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pattern TEXT NOT NULL,
    frequency INTEGER DEFAULT 1 NOT NULL,
    confidence DECIMAL(3, 2) DEFAULT 0.5 NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Budget usage table
CREATE TABLE IF NOT EXISTS budget_usage (
    key TEXT PRIMARY KEY,
    amount DECIMAL(14,6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Webhook Endpoints table
CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret VARCHAR(255) NOT NULL,
    events JSONB DEFAULT '["*"]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Webhook Deliveries table
CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB DEFAULT '{}'::jsonb,
    status_code INTEGER,
    response_body TEXT,
    delivered_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- Columns required by the webhook delivery recorder (internal/webhook). Kept
-- as additive ALTERs so this migration stays idempotent for databases created
-- from earlier schema versions (Supabase included).
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS success BOOLEAN DEFAULT false NOT NULL;
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS error TEXT DEFAULT '';
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS duration_ms BIGINT DEFAULT 0;
ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS last_triggered_at TIMESTAMPTZ;

-- Audit Logs table
CREATE TABLE IF NOT EXISTS audit_logs (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(255) NOT NULL,
    resource VARCHAR(100),
    resource_id VARCHAR(255),
    ip_address VARCHAR(45),
    user_agent TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'success',
    details TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Skill Embeddings table
CREATE TABLE IF NOT EXISTS skill_embeddings (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    skill_id UUID NOT NULL UNIQUE REFERENCES skills(id) ON DELETE CASCADE,
    embedding extensions.vector(1536),
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Feature Flags table
CREATE TABLE IF NOT EXISTS feature_flags (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    key VARCHAR(255) UNIQUE NOT NULL,
    enabled BOOLEAN DEFAULT false NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

-- ================================================================
-- SECTION 2: AUTOMATIC UPDATED_AT TRIGGERS
-- ================================================================

CREATE OR REPLACE FUNCTION public.update_updated_at_column()
RETURNS TRIGGER 
LANGUAGE plpgsql
SET search_path = ''
AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

DO $$
DECLARE
    t TEXT;
BEGIN
    FOR t IN 
        SELECT table_name 
        FROM information_schema.columns 
        WHERE table_schema = 'public' AND column_name = 'updated_at'
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS set_updated_at_%I ON public.%I;', t, t);
        EXECUTE format('CREATE TRIGGER set_updated_at_%I BEFORE UPDATE ON public.%I FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();', t, t);
    END LOOP;
END $$;

-- ================================================================
-- SECTION 3: AUTH FUNCTIONS WITH HARDENED SEARCH_PATH
-- ================================================================

CREATE OR REPLACE FUNCTION app_auth.set_current_user_id(user_id UUID)
RETURNS VOID 
LANGUAGE plpgsql 
SECURITY DEFINER
SET search_path = app_auth, public, extensions, pg_temp
AS $$
BEGIN
    PERFORM set_config('app.current_user_id', user_id::text, true);
END;
$$;

CREATE OR REPLACE FUNCTION app_auth.current_user_id()
RETURNS UUID 
LANGUAGE plpgsql 
STABLE
SECURITY DEFINER
SET search_path = app_auth, public, extensions, pg_temp
AS $$
DECLARE
    uid TEXT;
BEGIN
    uid := current_setting('app.current_user_id', true);
    IF uid IS NULL OR uid = '' THEN
        RETURN NULL;
    END IF;
    RETURN uid::UUID;
END;
$$;

CREATE OR REPLACE FUNCTION app_auth.is_org_member(org_id UUID)
RETURNS BOOLEAN 
LANGUAGE plpgsql 
STABLE 
SECURITY DEFINER
SET search_path = app_auth, public, extensions, pg_temp
AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM public.organization_members
        WHERE organization_id = org_id AND user_id = app_auth.current_user_id()
    );
END;
$$;

CREATE OR REPLACE FUNCTION app_auth.is_org_owner(org_id UUID)
RETURNS BOOLEAN 
LANGUAGE plpgsql 
STABLE 
SECURITY DEFINER
SET search_path = app_auth, public, extensions, pg_temp
AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM public.organizations
        WHERE id = org_id AND owner_id = app_auth.current_user_id()
    );
END;
$$;

CREATE OR REPLACE FUNCTION app_auth.is_admin()
RETURNS BOOLEAN 
LANGUAGE plpgsql 
STABLE 
SECURITY DEFINER
SET search_path = app_auth, public, extensions, pg_temp
AS $$
BEGIN
    RETURN EXISTS (
        SELECT 1 FROM public.users
        WHERE id = app_auth.current_user_id() AND role IN ('admin', 'superadmin')
    );
END;
$$;

-- anon is a Supabase-managed role that does not exist on self-hosted
-- Postgres; revoke from it only when present so this migration stays
-- portable (docker-compose / plain PostgreSQL installs).
REVOKE EXECUTE ON FUNCTION app_auth.set_current_user_id(UUID) FROM PUBLIC;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'anon') THEN
        EXECUTE 'REVOKE EXECUTE ON FUNCTION app_auth.set_current_user_id(UUID) FROM anon';
    END IF;
END $$;

-- ================================================================
-- SECTION 4: ROW LEVEL SECURITY (RLS) POLICIES (EXACTLY 1 PERMISSIVE POLICY PER TABLE)
-- ================================================================

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
ALTER TABLE audit_logs ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_embeddings ENABLE ROW LEVEL SECURITY;
ALTER TABLE feature_flags ENABLE ROW LEVEL SECURITY;

DROP POLICY IF EXISTS users_select ON users;
CREATE POLICY users_select ON users FOR SELECT USING (id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS orgs_select ON organizations;
CREATE POLICY orgs_select ON organizations FOR SELECT USING (app_auth.is_org_member(id) OR app_auth.is_admin());

DROP POLICY IF EXISTS org_members_select ON organization_members;
CREATE POLICY org_members_select ON organization_members FOR SELECT USING (app_auth.is_org_member(organization_id) OR app_auth.is_admin());

DROP POLICY IF EXISTS projects_select ON projects;
CREATE POLICY projects_select ON projects FOR SELECT USING (app_auth.is_org_member(org_id) OR app_auth.is_admin());

DROP POLICY IF EXISTS agents_select ON agents;
CREATE POLICY agents_select ON agents FOR SELECT USING (EXISTS (SELECT 1 FROM projects p WHERE p.id = agents.project_id AND app_auth.is_org_member(p.org_id)) OR app_auth.is_admin());

DROP POLICY IF EXISTS sessions_select ON sessions;
CREATE POLICY sessions_select ON sessions FOR SELECT USING (user_id = app_auth.current_user_id() OR EXISTS (SELECT 1 FROM projects p WHERE p.id = sessions.project_id AND app_auth.is_org_member(p.org_id)) OR app_auth.is_admin());

DROP POLICY IF EXISTS events_select ON events;
CREATE POLICY events_select ON events FOR SELECT USING (EXISTS (SELECT 1 FROM sessions s JOIN projects p ON p.id = s.project_id WHERE s.id = events.session_id AND app_auth.is_org_member(p.org_id)) OR app_auth.is_admin());

DROP POLICY IF EXISTS tasks_select ON tasks;
CREATE POLICY tasks_select ON tasks FOR SELECT USING (user_id = app_auth.current_user_id() OR EXISTS (SELECT 1 FROM projects p WHERE p.id = tasks.project_id AND app_auth.is_org_member(p.org_id)) OR app_auth.is_admin());

DROP POLICY IF EXISTS api_keys_select ON api_keys;
CREATE POLICY api_keys_select ON api_keys FOR SELECT USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS skills_select ON skills;
CREATE POLICY skills_select ON skills FOR SELECT USING (is_published = true OR author_id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS skill_ratings_select ON skill_ratings;
CREATE POLICY skill_ratings_select ON skill_ratings FOR SELECT USING (true);

DROP POLICY IF EXISTS skill_installs_select ON skill_installs;
CREATE POLICY skill_installs_select ON skill_installs FOR SELECT USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS alerts_select ON alerts;
CREATE POLICY alerts_select ON alerts FOR SELECT USING (app_auth.is_org_member(organization_id) OR app_auth.is_admin());

DROP POLICY IF EXISTS invoices_select ON invoices;
CREATE POLICY invoices_select ON invoices FOR SELECT USING (app_auth.is_org_member(organization_id) OR app_auth.is_admin());

DROP POLICY IF EXISTS subscriptions_select ON subscriptions;
CREATE POLICY subscriptions_select ON subscriptions FOR SELECT USING (app_auth.is_org_member(organization_id) OR app_auth.is_admin());

DROP POLICY IF EXISTS budget_usage_select ON budget_usage;
CREATE POLICY budget_usage_select ON budget_usage FOR SELECT USING (app_auth.is_admin());

DROP POLICY IF EXISTS memory_episodes_select ON memory_episodes;
CREATE POLICY memory_episodes_select ON memory_episodes FOR SELECT USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS memory_patterns_select ON memory_patterns;
CREATE POLICY memory_patterns_select ON memory_patterns FOR SELECT USING (EXISTS (SELECT 1 FROM projects p WHERE p.id = memory_patterns.project_id AND app_auth.is_org_member(p.org_id)) OR app_auth.is_admin());

-- Webhook Endpoints: Exactly 1 Policy
CREATE POLICY webhook_endpoints_policy ON webhook_endpoints FOR ALL USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

-- Webhook Deliveries: Exactly 1 Policy
CREATE POLICY webhook_deliveries_policy ON webhook_deliveries FOR ALL USING (EXISTS (SELECT 1 FROM webhook_endpoints e WHERE e.id = webhook_deliveries.endpoint_id AND (e.user_id = app_auth.current_user_id() OR app_auth.is_admin())));

-- Audit Logs Policies
DROP POLICY IF EXISTS audit_logs_select ON audit_logs;
CREATE POLICY audit_logs_select ON audit_logs FOR SELECT USING (user_id = app_auth.current_user_id() OR app_auth.is_admin());

DROP POLICY IF EXISTS audit_logs_insert ON audit_logs;
CREATE POLICY audit_logs_insert ON audit_logs FOR INSERT WITH CHECK (app_auth.current_user_id() IS NOT NULL OR app_auth.is_admin());

DROP POLICY IF EXISTS skill_embeddings_select ON skill_embeddings;
CREATE POLICY skill_embeddings_select ON skill_embeddings FOR SELECT USING (true);

-- Feature Flags: Exactly 1 Policy
CREATE POLICY feature_flags_policy ON feature_flags FOR ALL USING (enabled = true OR app_auth.is_admin());

-- ================================================================
-- SECTION 5: MONITORING VIEWS WITH SECURITY_INVOKER
-- ================================================================

DROP VIEW IF EXISTS v_active_connections CASCADE;
CREATE OR REPLACE VIEW v_active_connections WITH (security_invoker = true) AS
SELECT 
    pid, usename, application_name, client_addr, client_port, backend_start, state, query, query_start, state_change,
    NOW() - query_start AS query_duration
FROM pg_stat_activity 
WHERE datname = current_database() AND state != 'idle'
ORDER BY query_start;

DROP VIEW IF EXISTS v_table_sizes CASCADE;
CREATE OR REPLACE VIEW v_table_sizes WITH (security_invoker = true) AS
SELECT 
    schemaname, relname AS tablename,
    pg_size_pretty(pg_total_relation_size(schemaname||'.'||relname)) AS total_size,
    pg_size_pretty(pg_relation_size(schemaname||'.'||relname)) AS table_size,
    pg_size_pretty(pg_indexes_size((schemaname||'.'||relname)::regclass)) AS index_size,
    n_live_tup AS row_count
FROM pg_stat_user_tables
ORDER BY pg_total_relation_size(schemaname||'.'||relname) DESC;

DROP VIEW IF EXISTS v_index_usage CASCADE;
CREATE OR REPLACE VIEW v_index_usage WITH (security_invoker = true) AS
SELECT 
    schemaname, relname AS tablename, indexrelname,
    idx_scan AS times_used, idx_tup_read AS tuples_read, idx_tup_fetch AS tuples_fetched,
    pg_size_pretty(pg_relation_size(indexrelid)) AS index_size
FROM pg_stat_user_indexes
ORDER BY idx_scan DESC;

DROP VIEW IF EXISTS v_org_cost_24h CASCADE;
CREATE OR REPLACE VIEW v_org_cost_24h WITH (security_invoker = true) AS
SELECT 
    p.org_id, o.name AS org_name,
    COUNT(e.id) AS event_count, SUM(e.tokens_used) AS total_tokens, SUM(e.cost_usd) AS total_cost_usd, AVG(e.latency_ms) AS avg_latency_ms
FROM events e
JOIN sessions s ON s.id = e.session_id
JOIN projects p ON p.id = s.project_id
JOIN organizations o ON o.id = p.org_id
WHERE e.created_at > NOW() - INTERVAL '24 hours'
GROUP BY p.org_id, o.name
ORDER BY total_cost_usd DESC;

DROP VIEW IF EXISTS v_task_stats CASCADE;
CREATE OR REPLACE VIEW v_task_stats WITH (security_invoker = true) AS
SELECT 
    p.org_id, o.name AS org_name,
    COUNT(t.id) AS total_tasks,
    COUNT(t.id) FILTER (WHERE t.status = 'completed') AS completed,
    COUNT(t.id) FILTER (WHERE t.status = 'failed') AS failed,
    COUNT(t.id) FILTER (WHERE t.status = 'pending') AS pending,
    ROUND(COUNT(t.id) FILTER (WHERE t.status = 'completed')::DECIMAL / NULLIF(COUNT(t.id), 0) * 100, 2) AS completion_rate_pct,
    AVG(t.total_tokens) AS avg_tokens_per_task,
    AVG(t.cost) AS avg_cost_per_task
FROM tasks t
JOIN projects p ON p.id = t.project_id
JOIN organizations o ON o.id = p.org_id
WHERE t.created_at > NOW() - INTERVAL '7 days'
GROUP BY p.org_id, o.name
ORDER BY total_tasks DESC;

DROP VIEW IF EXISTS v_top_agents CASCADE;
CREATE OR REPLACE VIEW v_top_agents WITH (security_invoker = true) AS
SELECT 
    a.name AS agent_name, p.name AS project_name, o.name AS org_name,
    COUNT(s.id) AS session_count, COUNT(e.id) AS event_count, SUM(e.tokens_used) AS total_tokens, SUM(e.cost_usd) AS total_cost_usd
FROM agents a
JOIN projects p ON p.id = a.project_id
JOIN organizations o ON o.id = p.org_id
LEFT JOIN sessions s ON s.agent_id = a.id
LEFT JOIN events e ON e.session_id = s.id
WHERE e.created_at > NOW() - INTERVAL '30 days'
GROUP BY a.id, a.name, p.name, o.name
ORDER BY total_cost_usd DESC
LIMIT 20;

CREATE OR REPLACE FUNCTION check_db_health()
RETURNS TABLE(
    component TEXT,
    status TEXT,
    details TEXT,
    checked_at TIMESTAMPTZ
) 
LANGUAGE plpgsql
SET search_path = public, extensions, pg_temp
AS $$
BEGIN
    RETURN QUERY
    SELECT 'tables'::TEXT, CASE WHEN COUNT(*) > 0 THEN 'healthy' ELSE 'degraded' END::TEXT, COUNT(*)::TEXT || ' tables found'::TEXT, NOW()
    FROM information_schema.tables WHERE table_schema = 'public';

    RETURN QUERY
    SELECT 'pgvector'::TEXT, CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN 'healthy' ELSE 'degraded' END::TEXT,
           CASE WHEN EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'vector') THEN 'extension installed' ELSE 'extension missing' END::TEXT, NOW();

    RETURN QUERY
    SELECT 'connections'::TEXT, CASE WHEN COUNT(*) < 100 THEN 'healthy' ELSE 'degraded' END::TEXT, COUNT(*)::TEXT || ' active connections'::TEXT, NOW()
    FROM pg_stat_activity WHERE datname = current_database() AND state = 'active';
END;
$$;


-- API Key Rotation
-- Add API key rotation support
-- rotation_token_hash: stores HMAC of a rotation token so old key stays valid during 24h transition

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_token_hash VARCHAR(255);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_rotation_token ON api_keys (rotation_token_hash) WHERE rotation_token_hash IS NOT NULL;


-- Soft Deletes
-- Add soft delete support and optimistic concurrency control

-- Add deleted_at columns for soft deletes
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Add version columns for optimistic concurrency control
ALTER TABLE projects ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS version INTEGER NOT NULL DEFAULT 1;

-- Partial indexes: only index non-deleted rows
CREATE INDEX IF NOT EXISTS idx_users_active ON users (email) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_projects_active ON projects (org_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_agents_active ON agents (project_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions (project_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_active ON tasks (project_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_active_by_user ON tasks (user_id) WHERE deleted_at IS NULL;
