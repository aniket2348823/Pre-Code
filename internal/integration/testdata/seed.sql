-- Seed data for integration tests
-- Uses fixed UUIDs to ensure deterministic test behavior

-- Test users
INSERT INTO users (id, email, password_hash, name, role, is_active)
VALUES
    ('a0000000-0000-0000-0000-000000000001', 'alice@example.com', 'hash_alice', 'Alice Test', 'admin', true),
    ('a0000000-0000-0000-0000-000000000002', 'bob@example.com', 'hash_bob', 'Bob Test', 'user', true),
    ('a0000000-0000-0000-0000-000000000003', 'charlie@example.com', 'hash_charlie', 'Charlie Test', 'user', true)
ON CONFLICT (id) DO NOTHING;

-- Test organization (owner: Alice)
INSERT INTO organizations (id, name, slug, description, owner_id, plan)
VALUES
    ('b0000000-0000-0000-0000-000000000001', 'Test Org', 'test-org', 'Integration test org', 'a0000000-0000-0000-0000-000000000001', 'free')
ON CONFLICT (id) DO NOTHING;

-- Org membership
INSERT INTO organization_members (organization_id, user_id, role)
VALUES
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'admin'),
    ('b0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000002', 'member')
ON CONFLICT (organization_id, user_id) DO NOTHING;

-- Test project
INSERT INTO projects (id, org_id, name, description, status)
VALUES
    ('c0000000-0000-0000-0000-000000000001', 'b0000000-0000-0000-0000-000000000001', 'Test Project', 'Seed project', 'active')
ON CONFLICT (id) DO NOTHING;

-- Test agent
INSERT INTO agents (id, project_id, name, description, config, status)
VALUES
    ('d0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'Test Agent', 'Seed agent', '{"model": "gpt-4"}', 'idle')
ON CONFLICT (id) DO NOTHING;

-- Test session
INSERT INTO sessions (id, project_id, agent_id, user_id, status)
VALUES
    ('e0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'd0000000-0000-0000-0000-000000000001', 'a0000000-0000-0000-0000-000000000001', 'active')
ON CONFLICT (id) DO NOTHING;
