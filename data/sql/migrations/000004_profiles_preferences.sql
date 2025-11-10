-- 000004_profiles_preferences.sql
-- Adds profile and preference tables with tenant/org scoping plus metadata.

CREATE TABLE IF NOT EXISTS user_profiles (
    user_id UUID PRIMARY KEY,
    display_name TEXT,
    avatar_url TEXT,
    locale TEXT,
    timezone TEXT,
    bio TEXT,
    contact JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by UUID NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by UUID NOT NULL
);

CREATE INDEX IF NOT EXISTS user_profiles_scope_idx
    ON user_profiles (tenant_id, org_id);

CREATE TABLE IF NOT EXISTS user_preferences (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    scope_level TEXT NOT NULL DEFAULT 'user',
    key TEXT NOT NULL,
    value JSONB NOT NULL DEFAULT '{}',
    version INT NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_by UUID NOT NULL,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by UUID NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS user_preferences_scope_key_idx
    ON user_preferences (user_id, tenant_id, org_id, lower(key));

CREATE INDEX IF NOT EXISTS user_preferences_scope_idx
    ON user_preferences (tenant_id, org_id);

CREATE INDEX IF NOT EXISTS user_preferences_user_idx
    ON user_preferences (user_id);
