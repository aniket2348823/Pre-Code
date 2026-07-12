-- Migration 000006: Add email verification and skill embeddings support

-- Add email_verified column to users table
ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN DEFAULT false NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_email_verified ON users(email_verified);

-- Create skill_embeddings table for RAG vector search
CREATE TABLE IF NOT EXISTS skill_embeddings (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    skill_id UUID NOT NULL UNIQUE REFERENCES skills(id) ON DELETE CASCADE,
    embedding vector(1536),
    content_text TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Create HNSW index for fast vector similarity search
CREATE INDEX IF NOT EXISTS idx_skill_embeddings_hnsw
ON skill_embeddings USING hnsw (embedding vector_cosine_ops)
WITH (m = 16, ef_construction = 64);

-- Create index for full-text search on skills
CREATE INDEX IF NOT EXISTS idx_skills_name_gin
ON skills USING gin(to_tsvector('english', name || ' ' || COALESCE(description, '')));

-- Create trigram index for autocomplete suggestions
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE INDEX IF NOT EXISTS idx_skills_name_trgm
ON skills USING gin(name gin_trgm_ops);
