-- 00008_user_tokens.up.sql
-- Adds user_tokens table and securelink lifecycle fields to password_reset.

CREATE TABLE IF NOT EXISTS user_tokens (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_type TEXT NOT NULL CHECK (
        token_type IN ('invite', 'register', 'password_reset')
    ),
    jti TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'issued' CHECK (
        status IN ('issued', 'used', 'expired')
    ),
    issued_at TIMESTAMP,
    expires_at TIMESTAMP,
    used_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS user_tokens_user_id_index ON user_tokens (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS user_tokens_jti_unique ON user_tokens (jti);
CREATE INDEX IF NOT EXISTS user_tokens_expiry_status_index ON user_tokens (status, expires_at);

ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS jti TEXT;
ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS issued_at TIMESTAMP;
ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS expires_at TIMESTAMP;
ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS used_at TIMESTAMP;
ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS scope_tenant_id TEXT;
ALTER TABLE password_reset ADD COLUMN IF NOT EXISTS scope_org_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS password_reset_jti_unique ON password_reset (jti);
CREATE INDEX IF NOT EXISTS password_reset_expiry_status_index ON password_reset (status, expires_at);
