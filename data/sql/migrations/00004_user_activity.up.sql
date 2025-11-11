-- 00004_user_activity.up.sql
-- Activity log table powering go-admin dashboards and audit exports.

CREATE TABLE IF NOT EXISTS user_activity (
    id UUID PRIMARY KEY,
    user_id UUID,
    actor_id UUID,
    tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    org_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000',
    verb TEXT NOT NULL,
    object_type TEXT,
    object_id TEXT,
    channel TEXT,
    ip TEXT,
    data JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS user_activity_scope_idx
    ON user_activity (tenant_id, org_id, created_at DESC);

CREATE INDEX IF NOT EXISTS user_activity_user_idx
    ON user_activity (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS user_activity_object_idx
    ON user_activity (object_type, object_id);

CREATE INDEX IF NOT EXISTS user_activity_verb_idx
    ON user_activity (verb);
