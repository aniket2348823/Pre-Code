-- Remove user_id column from webhook_endpoints table.

DROP INDEX IF EXISTS idx_webhook_endpoints_user;
ALTER TABLE webhook_endpoints DROP COLUMN IF EXISTS user_id;
