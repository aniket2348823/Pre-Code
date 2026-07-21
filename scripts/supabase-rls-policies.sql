-- ⚠️ DO NOT DEPLOY: These policies require Supabase Auth (app_auth.current_user_id()).
-- VigilAgent uses its own JWT auth. Deploy only after migrating to
-- Supabase Auth or creating a custom JWT verification function.

-- WARNING: These RLS policies use app_auth.current_user_id() which requires Supabase Auth.
-- VigilAgent currently uses its own JWT auth (internal/auth/jwt.go).
-- DO NOT deploy these until migrating to Supabase Auth or creating a
-- custom PostgreSQL function to extract user ID from your app JWT.
-- VigilAgent Row Level Security (RLS) Policies
-- Run this AFTER migrations to secure data access

-- =====================================================
-- Enable RLS on all tables
-- =====================================================
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
ALTER TABLE sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE agents ENABLE ROW LEVEL SECURITY;
ALTER TABLE events ENABLE ROW LEVEL SECURITY;
ALTER TABLE tasks ENABLE ROW LEVEL SECURITY;
ALTER TABLE skills ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_ratings ENABLE ROW LEVEL SECURITY;
ALTER TABLE skill_installs ENABLE ROW LEVEL SECURITY;
ALTER TABLE alerts ENABLE ROW LEVEL SECURITY;
ALTER TABLE api_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_episodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE memory_patterns ENABLE ROW LEVEL SECURITY;
ALTER TABLE budget_usage ENABLE ROW LEVEL SECURITY;

-- =====================================================
-- Users: Can only read/update their own profile
-- =====================================================
CREATE POLICY users_select_own ON users
    FOR SELECT USING (app_auth.current_user_id() = id);

CREATE POLICY users_update_own ON users
    FOR UPDATE USING (app_auth.current_user_id() = id);

-- Admin can see all users
CREATE POLICY users_admin_all ON users
    FOR ALL USING (
        EXISTS (
            SELECT 1 FROM users 
            WHERE id = app_auth.current_user_id() AND role = 'admin'
        )
    );

-- =====================================================
-- Organizations: Members can read, owners can modify
-- =====================================================
CREATE POLICY orgs_select_member ON organizations
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = id AND user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY orgs_insert_own ON organizations
    FOR INSERT WITH CHECK (app_auth.current_user_id() = owner_id);

CREATE POLICY orgs_update_owner ON organizations
    FOR UPDATE USING (app_auth.current_user_id() = owner_id);

CREATE POLICY orgs_delete_owner ON organizations
    FOR DELETE USING (app_auth.current_user_id() = owner_id);

-- =====================================================
-- Organization Members: Members can read, owners can manage
-- =====================================================
CREATE POLICY org_members_select_own ON organization_members
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = organization_members.organization_id 
            AND user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY org_members_insert_owner ON organization_members
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM organizations 
            WHERE id = organization_id AND owner_id = app_auth.current_user_id()
        )
    );

CREATE POLICY org_members_delete_owner ON organization_members
    FOR DELETE USING (
        EXISTS (
            SELECT 1 FROM organizations 
            WHERE id = organization_id AND owner_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Projects: Organization members can access
-- =====================================================
CREATE POLICY projects_select_member ON projects
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = projects.org_id 
            AND user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY projects_insert_member ON projects
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = org_id 
            AND user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY projects_update_member ON projects
    FOR UPDATE USING (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = projects.org_id 
            AND user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY projects_delete_member ON projects
    FOR DELETE USING (
        EXISTS (
            SELECT 1 FROM organization_members 
            WHERE organization_id = projects.org_id 
            AND user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Sessions: Project members can access
-- =====================================================
CREATE POLICY sessions_select_member ON sessions
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = sessions.project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY sessions_insert_member ON sessions
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Agents: Project members can access
-- =====================================================
CREATE POLICY agents_select_member ON agents
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = agents.project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY agents_insert_member ON agents
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Events: Session members can access
-- =====================================================
CREATE POLICY events_select_member ON events
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM sessions s
            JOIN projects p ON p.id = s.project_id
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE s.id = events.session_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY events_insert_member ON events
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM sessions s
            JOIN projects p ON p.id = s.project_id
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE s.id = session_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Tasks: Project members can access
-- =====================================================
CREATE POLICY tasks_select_member ON tasks
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = tasks.project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY tasks_insert_member ON tasks
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Skills: Public read, author write
-- =====================================================
CREATE POLICY skills_select_public ON skills
    FOR SELECT USING (is_published = true OR author_id = app_auth.current_user_id());

CREATE POLICY skills_insert_author ON skills
    FOR INSERT WITH CHECK (app_auth.current_user_id() = author_id);

CREATE POLICY skills_update_author ON skills
    FOR UPDATE USING (app_auth.current_user_id() = author_id);

CREATE POLICY skills_delete_author ON skills
    FOR DELETE USING (app_auth.current_user_id() = author_id);

-- =====================================================
-- Skill Ratings: Public read, one per user
-- =====================================================
CREATE POLICY skill_ratings_select_public ON skill_ratings
    FOR SELECT USING (true);

CREATE POLICY skill_ratings_insert_own ON skill_ratings
    FOR INSERT WITH CHECK (app_auth.current_user_id() = user_id);

CREATE POLICY skill_ratings_update_own ON skill_ratings
    FOR UPDATE USING (app_auth.current_user_id() = user_id);

-- =====================================================
-- Alerts: User can access their own
-- =====================================================
CREATE POLICY alerts_select_own ON alerts
    FOR SELECT USING (app_auth.current_user_id() = user_id);

CREATE POLICY alerts_insert_own ON alerts
    FOR INSERT WITH CHECK (app_auth.current_user_id() = user_id);

CREATE POLICY alerts_update_own ON alerts
    FOR UPDATE USING (app_auth.current_user_id() = user_id);

CREATE POLICY alerts_delete_own ON alerts
    FOR DELETE USING (app_auth.current_user_id() = user_id);

-- =====================================================
-- API Keys: User can manage their own
-- =====================================================
CREATE POLICY api_keys_select_own ON api_keys
    FOR SELECT USING (app_auth.current_user_id() = user_id);

CREATE POLICY api_keys_insert_own ON api_keys
    FOR INSERT WITH CHECK (app_auth.current_user_id() = user_id);

CREATE POLICY api_keys_delete_own ON api_keys
    FOR DELETE USING (app_auth.current_user_id() = user_id);

-- =====================================================
-- Memory Episodes: User owns their memories
-- =====================================================
CREATE POLICY memory_episodes_select_own ON memory_episodes
    FOR SELECT USING (app_auth.current_user_id() = user_id);

CREATE POLICY memory_episodes_insert_own ON memory_episodes
    FOR INSERT WITH CHECK (app_auth.current_user_id() = user_id);

-- =====================================================
-- Memory Patterns: Project members can access
-- =====================================================
CREATE POLICY memory_patterns_select_member ON memory_patterns
    FOR SELECT USING (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = memory_patterns.project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

CREATE POLICY memory_patterns_insert_member ON memory_patterns
    FOR INSERT WITH CHECK (
        EXISTS (
            SELECT 1 FROM projects p
            JOIN organization_members om ON om.organization_id = p.org_id
            WHERE p.id = project_id 
            AND om.user_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Budget Usage: Organization owners can access
-- =====================================================
CREATE POLICY budget_usage_select_owner ON budget_usage
    FOR SELECT USING (
        key LIKE 'org:%' AND EXISTS (
            SELECT 1 FROM organizations 
            WHERE id = replace(key, 'org:', '') 
            AND owner_id = app_auth.current_user_id()
        )
    );

-- =====================================================
-- Service Role Bypass (for backend API calls)
-- =====================================================
-- The service_role key bypasses RLS, so backend operations
-- will continue to work. These policies protect data when
-- users access Supabase directly via the client SDK.

-- =====================================================
-- DONE! RLS is now active on all tables
-- =====================================================
