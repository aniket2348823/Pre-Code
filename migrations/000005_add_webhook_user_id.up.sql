-- Add user_id to webhook_endpoints for user-scoped webhook management.
-- This ensures each user can only see and manage their own webhooks.

-- First, add user_id as nullable to allow backfilling existing rows
ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE CASCADE;

-- Backfill existing webhook_endpoints with the first admin user (if any exist)
-- In production, this should be run manually with the correct user_id
UPDATE webhook_endpoints SET user_id = COALESCE(
    (SELECT id FROM users WHERE role = 'admin' LIMIT 1),
    (SELECT id FROM users LIMIT 1)
) WHERE user_id IS NULL;

-- Only add NOT NULL constraint if no NULL rows remain
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM webhook_endpoints WHERE user_id IS NULL LIMIT 1) THEN
        ALTER TABLE webhook_endpoints ALTER COLUMN user_id SET NOT NULL;
    ELSE
        RAISE WARNING 'Some webhook_endpoints still have NULL user_id. Run backfill manually.';
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_webhook_endpoints_user ON webhook_endpoints(user_id);
