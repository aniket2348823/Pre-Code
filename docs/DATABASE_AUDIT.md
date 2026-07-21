# VigilAgent — Database Audit

> **Database:** PostgreSQL 16 with pgvector extension
> **Migrations:** 6 numbered migration files (up/down)
> **Driver:** pgx/v5 with connection pooling

---

## 1. Schema Overview

### Tables (21 total)

| Table | Migration | Purpose | Rows Est. |
|-------|-----------|---------|-----------|
| `users` | 000001 | User accounts | Low |
| `organizations` | 000001 | Multi-tenant organizations | Low |
| `organization_members` | 000001 | Org membership | Low |
| `projects` | 000001 | Projects within orgs | Medium |
| `agents` | 000001 | AI agents | Medium |
| `sessions` | 000001 | Agent sessions | High |
| `events` | 000001 | Session events | High |
| `tasks` | 000002 | Agent tasks | High |
| `skills` | 000001 | Marketplace skills | Low |
| `skill_ratings` | 000001 | Skill reviews | Low |
| `skill_embeddings` | 000006 | RAG vectors | Low |
| `skill_installs` | 000001 | User installations | Low |
| `alerts` | 000001 | Alert rules | Low |
| `invoices` | 000001 | Billing invoices | Low |
| `subscriptions` | 000001 | Billing subscriptions | Low |
| `api_keys` | 000001 | API key storage | Low |
| `budget_usage` | 000004 | Budget counters | Low |
| `memory_episodes` | 000002 | Episodic memory | Medium |
| `memory_patterns` | 000002 | Semantic memory | Medium |
| `webhook_endpoints` | 000001 | Webhook registrations | Low |
| `webhook_deliveries` | 000001 | Delivery results | High |

---

## 2. Table Details

### users
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255),
    avatar_url TEXT,
    role VARCHAR(50) DEFAULT 'user' NOT NULL,
    is_active BOOLEAN DEFAULT true NOT NULL,
    email_verified BOOLEAN DEFAULT false NOT NULL,  -- Added in 000006
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_users_email` ON (email) — UNIQUE
- `idx_users_role` ON (role)
- `idx_users_last_login` ON (last_login_at) WHERE last_login_at IS NOT NULL
- `idx_users_email_verified` ON (email_verified)

**Constraints:**
- `email` UNIQUE
- `role` DEFAULT 'user'
- `is_active` DEFAULT true
- `email_verified` DEFAULT false

**Issues:**
- ⚠️ No index on `is_active` for filtering active users
- ⚠️ `email_verified` index not used in queries

---

### organizations
```sql
CREATE TABLE organizations (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan VARCHAR(50) DEFAULT 'free' NOT NULL,
    settings JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_organizations_slug` ON (slug) — UNIQUE
- `idx_organizations_owner` ON (owner_id)

**Foreign Keys:**
- `owner_id` → `users(id)` ON DELETE CASCADE

**Issues:**
- ⚠️ No index on `plan` for plan-based queries
- ⚠️ No composite index on `(owner_id, created_at)` for sorted lists

---

### organization_members
```sql
CREATE TABLE organization_members (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role VARCHAR(50) DEFAULT 'member' NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    UNIQUE(organization_id, user_id)
);
```

**Indexes:**
- `idx_org_members_org` ON (organization_id)
- `idx_org_members_user` ON (user_id)
- UNIQUE constraint on (organization_id, user_id)

**Foreign Keys:**
- `organization_id` → `organizations(id)` ON DELETE CASCADE
- `user_id` → `users(id)` ON DELETE CASCADE

---

### projects
```sql
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    org_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_projects_org_id` ON (org_id)
- `idx_projects_org` ON (org_id) — Duplicate in 000003
- `idx_projects_status` ON (status)

**Foreign Keys:**
- `org_id` → `organizations(id)` ON DELETE CASCADE

**Issues:**
- ⚠️ Duplicate index: `idx_projects_org_id` and `idx_projects_org`
- ⚠️ No composite index on `(org_id, created_at)` for sorted lists

---

### agents
```sql
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    config JSONB DEFAULT '{}',
    status VARCHAR(50) NOT NULL DEFAULT 'idle',
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_agents_project_id` ON (project_id)
- `idx_agents_project` ON (project_id) — Duplicate in 000003
- `idx_agents_status` ON (status)

**Foreign Keys:**
- `project_id` → `projects(id)` ON DELETE CASCADE

**Issues:**
- ⚠️ Duplicate index: `idx_agents_project_id` and `idx_agents_project`

---

### sessions
```sql
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id UUID,
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_sessions_project_id` ON (project_id)
- `idx_sessions_user_id` ON (user_id)
- `idx_sessions_agent` ON (agent_id) — Added in 000003
- `idx_sessions_project` ON (project_id) — Duplicate in 000003
- `idx_sessions_user` ON (user_id) — Duplicate in 000003
- `idx_sessions_status` ON (status) — Added in 000003

**Foreign Keys:**
- `project_id` → `projects(id)` ON DELETE CASCADE
- `user_id` → `users(id)` ON DELETE SET NULL
- ⚠️ `agent_id` has NO foreign key constraint

**Issues:**
- 🔴 **Missing FK**: `agent_id` references `agents(id)` but has no constraint
- ⚠️ Duplicate indexes on `project_id` and `user_id`

---

### events
```sql
CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    source VARCHAR(100),
    payload JSONB DEFAULT '{}'::jsonb,
    tokens_used INTEGER DEFAULT 0,
    cost_usd DECIMAL(10, 6) DEFAULT 0,
    latency_ms INTEGER,
    embedding vector(1536),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_events_session` ON (session_id)
- `idx_events_type` ON (event_type)
- `idx_events_created` ON (created_at)
- `idx_events_cost` ON (cost_usd) — Added in 000003
- `idx_events_session_created` ON (session_id, created_at) — Composite

**Foreign Keys:**
- `session_id` → `sessions(id)` ON DELETE CASCADE

**Issues:**
- ⚠️ `embedding vector(1536)` — 1536-dimension vectors are expensive
- ⚠️ No index on `embedding` for vector search (IVFFlat requires data first)
- ⚠️ High table size — consider partitioning by `created_at`

---

### tasks
```sql
CREATE TABLE tasks (
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
```

**Indexes:**
- `idx_tasks_project_id` ON (project_id)
- `idx_tasks_user_id` ON (user_id)
- `idx_tasks_status` ON (status)
- `idx_tasks_created_at` ON (created_at)
- `idx_tasks_project` ON (project_id) — Duplicate in 000003
- `idx_tasks_user` ON (user_id) — Duplicate in 000003
- `idx_tasks_created` ON (created_at) — Duplicate in 000003

**Foreign Keys:**
- `project_id` → `projects(id)` ON DELETE CASCADE
- `user_id` → `users(id)` ON DELETE CASCADE

**Issues:**
- ⚠️ Triple duplicate indexes on `project_id`, `user_id`, `created_at`
- ⚠️ No index on `model` or `provider` for analytics queries
- ⚠️ `prompt TEXT` can be very large — consider storing in separate table

---

### skills
```sql
CREATE TABLE skills (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) UNIQUE NOT NULL,
    description TEXT,
    author_id UUID REFERENCES users(id) ON DELETE SET NULL,
    skill_type VARCHAR(100) NOT NULL,
    config JSONB DEFAULT '{}'::jsonb,
    version VARCHAR(50) DEFAULT '1.0.0',
    is_published BOOLEAN DEFAULT false NOT NULL,
    download_count INTEGER DEFAULT 0,
    avg_rating DECIMAL(3, 2) DEFAULT 0,
    embedding vector(1536),
    -- Added in 000002:
    author VARCHAR(255),
    downloads INTEGER DEFAULT 0,
    rating DECIMAL(3, 2) DEFAULT 0,
    rating_count INTEGER DEFAULT 0,
    permissions JSONB DEFAULT '[]'::jsonb,
    is_verified BOOLEAN DEFAULT false,
    -- Added in migration:
    category VARCHAR(100),
    manifest JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMestAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_skills_slug` ON (slug) — UNIQUE
- `idx_skills_author` ON (author_id)
- `idx_skills_type` ON (skill_type)
- `idx_skills_published` ON (is_published)
- `idx_skills_category` ON (category)
- `idx_skills_downloads` ON (downloads DESC)
- `idx_skills_name_gin` ON GIN(to_tsvector(...)) — Full-text search
- `idx_skills_name_trgm` ON GIN(name gin_trgm_ops) — Trigram search

**Issues:**
- 🔴 **Schema Drift**: `download_count` vs `downloads`, `avg_rating` vs `rating`
- 🔴 **Redundant columns**: Both old and new naming conventions exist
- ⚠️ `embedding vector(1536)` on main table — should be in `skill_embeddings`

---

### skill_embeddings
```sql
CREATE TABLE skill_embeddings (
    id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
    skill_id UUID NOT NULL UNIQUE REFERENCES skills(id) ON DELETE CASCADE,
    embedding vector(1536),
    content_text TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Indexes:**
- `idx_skill_embeddings_hnsw` ON HNSW (embedding vector_cosine_ops)

**Foreign Keys:**
- `skill_id` → `skills(id)` ON DELETE CASCADE

---

### alerts
```sql
CREATE TABLE alerts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    organization_id UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    alert_type VARCHAR(100) NOT NULL,
    conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    channels JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    last_triggered_at TIMESTAMPTZ,
    -- Added in 000002:
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(100),
    condition JSONB DEFAULT '{}'::jsonb,
    channel VARCHAR(100),
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Issues:**
- 🔴 **Schema Drift**: `alert_type` vs `type`, `conditions` vs `condition`, `channels` vs `channel`
- 🔴 **Redundant columns**: Both old and new naming conventions exist

---

### invoices
```sql
CREATE TABLE invoices (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
```

**Indexes:**
- `idx_invoices_org` ON (organization_id)
- `idx_invoices_status` ON (status)

---

### subscriptions
```sql
CREATE TABLE subscriptions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
```

**Indexes:**
- `idx_subscriptions_org` ON (organization_id)
- `idx_subscriptions_status` ON (status)

---

### api_keys
```sql
CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
```

**Indexes:**
- `idx_api_keys_hash` ON (key_hash) WHERE is_active = TRUE
- `idx_api_keys_user` ON (user_id)

**Issues:**
- ⚠️ Partial index on `key_hash` only covers active keys — inactive key lookups scan full table

---

### budget_usage
```sql
CREATE TABLE budget_usage (
    key TEXT PRIMARY KEY,
    amount DECIMAL(14,6) NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Notes:**
- Keys namespaced: `org:<uuid>` and `task:<uuid>`
- Simple key-value store for budget tracking

---

### memory_episodes
```sql
CREATE TABLE memory_episodes (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    project_id UUID REFERENCES projects(id) ON DELETE SET NULL,
    session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    content TEXT NOT NULL,
    embedding vector(1536),
    metadata JSONB DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_memory_episodes_user` ON (user_id)
- `idx_memory_episodes_project` ON (project_id)

**Issues:**
- ⚠️ No index on `embedding` for vector search
- ⚠️ No index on `session_id` for session-based queries

---

### memory_patterns
```sql
CREATE TABLE memory_patterns (
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
```

**Indexes:**
- `idx_memory_patterns_project` ON (project_id)
- `idx_memory_patterns_type` ON (pattern_type)

---

### webhook_endpoints
```sql
CREATE TABLE webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,  -- Added in 000005
    url VARCHAR(2048) NOT NULL,
    secret VARCHAR(255),
    events JSONB NOT NULL DEFAULT '[]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    last_triggered_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Indexes:**
- `idx_webhook_endpoints_user` ON (user_id)

---

### webhook_deliveries
```sql
CREATE TABLE webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    status_code INTEGER,
    success BOOLEAN NOT NULL,
    error TEXT,
    duration_ms BIGINT,
    retry_count INTEGER DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
```

**Issues:**
- ⚠️ No index on `endpoint_id` for delivery lookups
- ⚠️ High table size — consider partitioning or TTL-based cleanup

---

## 3. Relationships

```
users (1) ──< organization_members >── (1) organizations
users (1) ──< api_keys
users (1) ──< alerts
users (1) ──< tasks
users (1) ──< memory_episodes
organizations (1) ──< projects
organizations (1) ──< alerts
organizations (1) ──< invoices
organizations (1) ──< subscriptions
projects (1) ──< agents
projects (1) ──< sessions
projects (1) ──< tasks
projects (1) ──< memory_patterns
agents (1) ──< sessions (MISSING FK)
sessions (1) ──< events
sessions (1) ──< memory_episodes
skills (1) ──< skill_ratings
skills (1) ──< skill_installs
skills (1) ──< skill_embeddings
webhook_endpoints (1) ──< webhook_deliveries
```

---

## 4. Missing Indexes

| Table | Column | Query Pattern | Recommendation |
|-------|--------|---------------|----------------|
| `sessions` | `agent_id` | `WHERE agent_id = $1` | Add index (FK also missing) |
| `events` | `embedding` | Vector similarity search | Add IVFFlat/HNSW index |
| `memory_episodes` | `embedding` | Vector similarity search | Add IVFFlat/HNSW index |
| `memory_patterns` | `embedding` | Vector similarity search | Add IVFFlat/HNSW index |
| `webhook_deliveries` | `endpoint_id` | `WHERE endpoint_id = $1` | Add index |
| `tasks` | `model` | Analytics by model | Add index |
| `tasks` | `provider` | Analytics by provider | Add index |
| `organizations` | `plan` | Plan-based queries | Add index |
| `users` | `is_active` | Active user queries | Add partial index |

---

## 5. Duplicate Indexes

| Table | Duplicate Indexes | Recommendation |
|-------|-------------------|----------------|
| `projects` | `idx_projects_org_id` + `idx_projects_org` | Drop one |
| `agents` | `idx_agents_project_id` + `idx_agents_project` | Drop one |
| `sessions` | `idx_sessions_project_id` + `idx_sessions_project` | Drop one |
| `sessions` | `idx_sessions_user_id` + `idx_sessions_user` | Drop one |
| `tasks` | `idx_tasks_project_id` + `idx_tasks_project` | Drop one |
| `tasks` | `idx_tasks_user_id` + `idx_tasks_user` | Drop one |
| `tasks` | `idx_tasks_created_at` + `idx_tasks_created` | Drop one |

---

## 6. Schema Drift Issues

### skills table
| Column | Old Name | New Name | Status |
|--------|----------|----------|--------|
| Downloads | `download_count` | `downloads` | Both exist |
| Rating | `avg_rating` | `rating` | Both exist |
| Rating Count | — | `rating_count` | New |
| Permissions | — | `permissions` | New |
| Verified | — | `is_verified` | New |
| Category | — | `category` | New |
| Manifest | — | `manifest` | New |

### alerts table
| Column | Old Name | New Name | Status |
|--------|----------|----------|--------|
| Type | `alert_type` | `type` | Both exist |
| Condition | `conditions` | `condition` | Both exist |
| Channel | `channels` | `channel` | Both exist |
| User ID | — | `user_id` | New |

### Recommendation
- Create migration to drop deprecated columns
- Update all queries to use new column names
- Add NOT NULL constraints after data migration

---

## 7. Potential Slow Queries

| Query | Table | Issue | Fix |
|-------|-------|-------|-----|
| `ListByUser` on organizations | `organizations` | JOIN without index on `organization_members.user_id` | Index exists, but query scans both tables |
| `GetCostByOrg` on events | `events` | Aggregation over large table | Add materialized view for analytics |
| `GetTopAgentsByOrg` | `events` | GROUP BY without covering index | Add composite index |
| `HybridSearch` on skills | `skills` + `skill_embeddings` | Vector + full-text join | Ensure HNSW index is populated |
| `SearchMemory` | `memory_episodes` + `memory_patterns` | Vector similarity scan | Add IVFFlat indexes |

---

## 8. Normalization Issues

| Issue | Table | Details | Recommendation |
|-------|-------|---------|----------------|
| Redundant columns | `skills` | Both `download_count` and `downloads` | Drop deprecated columns |
| Redundant columns | `alerts` | Both `alert_type` and `type` | Drop deprecated columns |
| JSONB for structured data | `organizations.settings` | Settings as JSONB | Consider separate table if query patterns emerge |
| JSONB for arrays | `skills.permissions` | Permissions as JSONB array | Consider junction table for complex queries |
| No soft delete | All tables | Hard deletes only | Add `deleted_at` timestamp if needed |

---

## 9. Migration History

| Migration | Date | Purpose | Breaking |
|-----------|------|---------|----------|
| 000001 | Sprint 1 | Initial schema (16 tables) | No |
| 000002 | Sprint 2 | Add tasks, memory tables, fix schema mismatches | No |
| 000003 | Sprint 2 | Add performance indexes | No |
| 000004 | Sprint 3 | Add budget_usage table | No |
| 000005 | Sprint 3 | Add user_id to webhook_endpoints | No |
| 000006 | Sprint 3 | Add email verification, skill embeddings | No |

---

## 10. Future Recommendations

### High Priority
1. **Drop deprecated columns** in `skills` and `alerts` tables
2. **Add missing FK** on `sessions.agent_id`
3. **Add vector indexes** on `memory_episodes` and `memory_patterns`
4. **Add index** on `webhook_deliveries.endpoint_id`
5. **Remove duplicate indexes** (7 pairs identified)

### Medium Priority
6. **Partition `events` table** by `created_at` (monthly)
7. **Partition `webhook_deliveries`** by `created_at` (monthly)
8. **Add materialized views** for analytics queries
9. **Add soft delete** support (optional `deleted_at` column)
10. **Add `updated_at` triggers** to tables missing them

### Low Priority
11. **Add CHECK constraints** for enum-like columns (`status`, `role`)
12. **Add composite indexes** for common query patterns
13. **Consider TimescaleDB** for time-series event data
14. **Add database-level audit logging** (pgAudit extension)
15. **Implement connection pooling** tuning based on load testing
