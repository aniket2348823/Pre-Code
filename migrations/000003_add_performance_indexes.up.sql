-- Performance indexes for common query patterns

-- Users
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_role ON users (role);
CREATE INDEX IF NOT EXISTS idx_users_last_login ON users (last_login_at) WHERE last_login_at IS NOT NULL;

-- Organizations
CREATE INDEX IF NOT EXISTS idx_organizations_slug ON organizations (slug);
CREATE INDEX IF NOT EXISTS idx_organizations_owner ON organizations (owner_id);

-- Organization Members
CREATE INDEX IF NOT EXISTS idx_org_members_user ON organization_members (user_id);
CREATE INDEX IF NOT EXISTS idx_org_members_org ON organization_members (organization_id);

-- Projects
CREATE INDEX IF NOT EXISTS idx_projects_org ON projects (org_id);
CREATE INDEX IF NOT EXISTS idx_projects_status ON projects (status);

-- Agents
CREATE INDEX IF NOT EXISTS idx_agents_project ON agents (project_id);
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents (status);

-- Sessions
CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions (agent_id);
CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions (project_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions (status);

-- Events (analytics queries)
CREATE INDEX IF NOT EXISTS idx_events_session ON events (session_id);
CREATE INDEX IF NOT EXISTS idx_events_type ON events (event_type);
CREATE INDEX IF NOT EXISTS idx_events_created ON events (created_at);
CREATE INDEX IF NOT EXISTS idx_events_cost ON events (cost_usd);

-- Composite index for cost/token analytics queries
CREATE INDEX IF NOT EXISTS idx_events_session_created ON events (session_id, created_at);

-- Tasks
CREATE INDEX IF NOT EXISTS idx_tasks_project ON tasks (project_id);
CREATE INDEX IF NOT EXISTS idx_tasks_user ON tasks (user_id);
CREATE INDEX IF NOT EXISTS idx_tasks_status ON tasks (status);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks (created_at);

-- API Keys
CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys (key_hash) WHERE is_active = TRUE;
CREATE INDEX IF NOT EXISTS idx_api_keys_user ON api_keys (user_id);

-- Skills
CREATE INDEX IF NOT EXISTS idx_skills_category ON skills (category);
CREATE INDEX IF NOT EXISTS idx_skills_published ON skills (is_published) WHERE is_published = TRUE;
CREATE INDEX IF NOT EXISTS idx_skills_downloads ON skills (downloads DESC);

-- Skill Ratings
CREATE INDEX IF NOT EXISTS idx_skill_ratings_skill ON skill_ratings (skill_id);

-- Skill Installations
CREATE INDEX IF NOT EXISTS idx_skill_installations_skill ON skill_installations (skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_installations_user ON skill_installations (user_id);

-- Alerts
CREATE INDEX IF NOT EXISTS idx_alerts_user ON alerts (user_id);
CREATE INDEX IF NOT EXISTS idx_alerts_active ON alerts (is_active) WHERE is_active = TRUE;
