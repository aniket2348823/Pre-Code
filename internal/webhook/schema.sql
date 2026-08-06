-- Reference schema for the webhook tables. This file mirrors migration
-- 000001_init_schema.up.sql — migration 000001 is the canonical source of
-- truth and this copy exists only for documentation/reference. Keep the two
-- in sync: user_id is UUID so the RLS policies in 000001 can compare it to
-- app_auth.current_user_id(), and webhook_deliveries carries the columns the
-- delivery recorder (internal/webhook/webhook.go) actually writes.

CREATE TABLE IF NOT EXISTS webhook_endpoints (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    secret VARCHAR(255) NOT NULL DEFAULT '',
    events JSONB DEFAULT '["*"]'::jsonb,
    is_active BOOLEAN DEFAULT true NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    last_triggered_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id UUID PRIMARY KEY DEFAULT extensions.uuid_generate_v4(),
    endpoint_id UUID NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_type VARCHAR(100) NOT NULL,
    payload JSONB DEFAULT '{}'::jsonb,
    status_code INTEGER,
    response_body TEXT,
    delivered_at TIMESTAMPTZ DEFAULT NOW() NOT NULL,
    success BOOLEAN DEFAULT false NOT NULL,
    error TEXT DEFAULT '',
    duration_ms BIGINT DEFAULT 0
);
