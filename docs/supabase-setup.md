# Supabase Setup Guide for VigilAgent

## Prerequisites
- A Supabase account (https://supabase.com)
- Your Supabase project created

## Step 1: Get Your Credentials

1. Go to **Supabase Dashboard** → **Settings** → **Database**
2. Find your **Connection string** (URI format)
3. Copy the values you need:
   - **Host**: `db.XXXX.supabase.co`
   - **Password**: Your database password (set during project creation)

## Step 2: Enable pgvector Extension

### Option A: Via Dashboard (Recommended)
1. Go to **Database** → **Extensions**
2. Search for **"vector"**
3. Toggle it **ON**

### Option B: Via SQL Editor
```sql
-- Run this in Supabase SQL Editor
CREATE EXTENSION IF NOT EXISTS vector;
```

## Step 3: Run Migrations

Copy the content of each migration file and run it in **Supabase SQL Editor** in order:

### Migration 1: Core Schema
```sql
-- Run: migrations/000001_init_schema.up.sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

-- Users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255),
    avatar_url TEXT,
    role VARCHAR(50) DEFAULT 'user' NOT NULL,
    is_active BOOLEAN DEFAULT true NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NOW() NOT NULL
);
-- ... (copy full migration content from file)
```

### Migration 2-4: Run in order
- `000002_add_tasks_and_fix_schemas.up.sql`
- `000003_add_performance_indexes.up.sql`
- `000004_add_budget_usage.up.sql`

## Step 4: Update Your Config

### configs/config.yaml
```yaml
database:
  host: db.YOUR-PROJECT-REF.supabase.co
  port: 5432
  user: postgres
  password: YOUR-DATABASE-PASSWORD
  name: postgres
  sslmode: require  # Supabase requires SSL
  max_open_conns: 25
  max_idle_conns: 10
  max_lifetime: 5m
```

### Or via Environment Variables
```bash
export VIGILAGENT_DATABASE_HOST=db.YOUR-PROJECT-REF.supabase.co
export VIGILAGENT_DATABASE_PASSWORD=your-password
export VIGILAGENT_DATABASE_SSLMODE=require
```

## Step 5: Verify Connection

```bash
# Test connection
psql "postgresql://postgres:YOUR-PASSWORD@db.YOUR-PROJECT-REF.supabase.co:5432/postgres?sslmode=require"

# Or run your app
go run ./cmd/api
```

## Troubleshooting

### "connection refused"
- Check your project is not paused (free tier pauses after inactivity)
- Verify host and port are correct

### "SSL required"
- Ensure `sslmode=require` is set in your connection string

### "pgvector not found"
- Make sure you enabled the vector extension in Dashboard → Database → Extensions

### "prepared statement" errors
- If using transaction mode pooler (port 6543), disable prepared statements in your driver config
