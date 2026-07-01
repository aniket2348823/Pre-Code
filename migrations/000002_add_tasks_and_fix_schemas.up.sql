-- VigilAgent Schema Migration 000002
-- Add tasks table and fix schema mismatches

-- Tasks table
CREATE TABLE IF NOT EXISTS tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prompt TEXT NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    result TEXT,
    model VARCHAR(100),
    provider VARCHAR(100),
    complexity VARCHAR(50),
    max_tokens INTEGER DEFAULT 8192,
    max_iterations INTEGER DEFAULT 20,
    input_tokens INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0,
    total_tokens INTEGER DEFAULT 0,
    cost DECIMAL(10, 6) DEFAULT 0,
    error TEXT,
    plan_json JSONB,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_tasks_project_id ON tasks(project_id);
CREATE INDEX idx_tasks_user_id ON tasks(user_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_created_at ON tasks(created_at);

-- Add trigger for tasks updated_at
CREATE TRIGGER update_tasks_updated_at BEFORE UPDATE ON tasks FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Fix skills table to match repository schema
-- The repository expects: author, downloads, rating, rating_count, permissions, is_verified
-- Current schema has: author_id, download_count, avg_rating, skill_type
ALTER TABLE skills ADD COLUMN IF NOT EXISTS author VARCHAR(255);
ALTER TABLE skills ADD COLUMN IF NOT EXISTS downloads INTEGER DEFAULT 0;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS rating DECIMAL(3, 2) DEFAULT 0;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS rating_count INTEGER DEFAULT 0;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS permissions JSONB DEFAULT '[]'::jsonb;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS is_verified BOOLEAN DEFAULT false;

-- Fix skill_installs to match repository schema (skill_installations)
-- The repository expects: project_id, status, config, installed_at
ALTER TABLE skill_installs ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE SET NULL;
ALTER TABLE skill_installs ADD COLUMN IF NOT EXISTS status VARCHAR(50) DEFAULT 'installed';
ALTER TABLE skill_installs ADD COLUMN IF NOT EXISTS config JSONB;

-- Fix alerts table to match repository schema
-- The repository expects: user_id, name, type, condition, channel, is_active
-- Current schema has: organization_id, name, description, alert_type, conditions, channels
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS name VARCHAR(255);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS type VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS condition JSONB DEFAULT '{}'::jsonb;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS channel VARCHAR(100);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS is_active BOOLEAN DEFAULT true;

-- Memory tables for the memory system
CREATE TABLE IF NOT EXISTS memory_episodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    content TEXT NOT NULL,
    embedding vector(1536),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE INDEX idx_memory_episodes_user ON memory_episodes(user_id);
CREATE INDEX idx_memory_episodes_project ON memory_episodes(project_id);

CREATE TABLE IF NOT EXISTS memory_patterns (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    pattern_type VARCHAR(100) NOT NULL,
    content TEXT NOT NULL,
    embedding vector(1536),
    confidence DECIMAL(3, 2) DEFAULT 0,
    usage_count INTEGER DEFAULT 0,
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);

CREATE INDEX idx_memory_patterns_project ON memory_patterns(project_id);
CREATE INDEX idx_memory_patterns_type ON memory_patterns(pattern_type);

CREATE TRIGGER update_memory_patterns_updated_at BEFORE UPDATE ON memory_patterns FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
