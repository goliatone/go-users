-- 00005_profiles_preferences.up.sql (SQLite version)
-- Adds profile and preference tables with tenant/org scoping plus metadata.
-- Changes from PostgreSQL: UUID -> TEXT, JSONB -> TEXT, INT -> INTEGER

CREATE TABLE IF NOT EXISTS user_profiles (
    user_id TEXT PRIMARY KEY,
    display_name TEXT,
    avatar_url TEXT,
    locale TEXT,
    timezone TEXT,
    bio TEXT,
    contact TEXT NOT NULL DEFAULT '{}',
    metadata TEXT NOT NULL DEFAULT '{}',
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS user_profiles_scope_idx
    ON user_profiles (tenant_id, org_id);

CREATE TABLE IF NOT EXISTS user_preferences (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope_level TEXT NOT NULL DEFAULT 'user',
    key TEXT NOT NULL,
    value TEXT NOT NULL DEFAULT '{}',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by TEXT NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS user_preferences_scope_key_idx
    ON user_preferences (user_id, tenant_id, org_id, lower(key));

CREATE INDEX IF NOT EXISTS user_preferences_scope_idx
    ON user_preferences (tenant_id, org_id);

CREATE INDEX IF NOT EXISTS user_preferences_user_idx
    ON user_preferences (user_id);
