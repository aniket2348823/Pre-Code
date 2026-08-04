-- Add API key rotation support
-- rotation_token_hash: stores HMAC of a rotation token so old key stays valid during 24h transition

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotation_token_hash VARCHAR(255);
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS rotated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON api_keys (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_rotation_token ON api_keys (rotation_token_hash) WHERE rotation_token_hash IS NOT NULL;
