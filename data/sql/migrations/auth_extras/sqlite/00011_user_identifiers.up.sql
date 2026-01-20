-- 00011_user_identifiers.up.sql (SQLite version)
-- Adds user_identifiers for external auth identifiers.

CREATE TABLE IF NOT EXISTS user_identifiers (
    id TEXT NOT NULL PRIMARY KEY,
    user_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    identifier TEXT NOT NULL,
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
    CONSTRAINT uq_user_identifiers_provider_id UNIQUE (provider, identifier)
);

CREATE INDEX IF NOT EXISTS idx_user_identifiers_user_provider ON user_identifiers(user_id, provider);
