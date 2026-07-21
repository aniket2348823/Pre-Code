-- Rollback Migration 000006

-- Drop skill embeddings table and indexes
DROP INDEX IF EXISTS idx_skills_name_trgm;
DROP INDEX IF EXISTS idx_skills_name_gin;
DROP INDEX IF EXISTS idx_skill_embeddings_hnsw;
DROP TABLE IF EXISTS skill_embeddings;

-- Remove email_verified column from users
DROP INDEX IF EXISTS idx_users_email_verified;
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
