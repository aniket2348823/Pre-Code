DROP INDEX IF EXISTS idx_api_keys_rotation_token;
DROP INDEX IF EXISTS idx_api_keys_expires_at;
ALTER TABLE api_keys DROP COLUMN IF EXISTS rotated_at;
ALTER TABLE api_keys DROP COLUMN IF EXISTS rotation_token_hash;
