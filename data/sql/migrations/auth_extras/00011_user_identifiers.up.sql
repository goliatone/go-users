-- 00011_user_identifiers.up.sql
-- Adds user_identifiers for external auth identifiers.

CREATE TABLE IF NOT EXISTS user_identifiers (
    id UUID NOT NULL PRIMARY KEY,
    user_id UUID NOT NULL,
    provider TEXT NOT NULL,
    identifier TEXT NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT uq_user_identifiers_provider_id UNIQUE (provider, identifier)
);

CREATE INDEX IF NOT EXISTS idx_user_identifiers_user_provider ON user_identifiers(user_id, provider);
