-- VigilAgent Database Schema Down Migration

DROP TRIGGER IF EXISTS update_subscriptions_updated_at ON subscriptions;
DROP TRIGGER IF EXISTS update_alerts_updated_at ON alerts;
DROP TRIGGER IF EXISTS update_skills_updated_at ON skills;
DROP TRIGGER IF EXISTS update_agents_updated_at ON agents;
DROP TRIGGER IF EXISTS update_projects_updated_at ON projects;
DROP TRIGGER IF EXISTS update_organizations_updated_at ON organizations;
DROP TRIGGER IF EXISTS update_users_updated_at ON users;

DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS skill_installs;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS alerts;
DROP TABLE IF EXISTS skill_ratings;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS organization_members;
DROP TABLE IF EXISTS organizations;
DROP TABLE IF EXISTS users;

DROP EXTENSION IF EXISTS "vector";
DROP EXTENSION IF EXISTS "uuid-ossp";
