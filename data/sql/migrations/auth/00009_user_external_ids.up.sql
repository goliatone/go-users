-- 00009_user_external_ids.up.sql
-- Adds external ID columns to users for auth provider mappings.

ALTER TABLE users
    ADD COLUMN external_id TEXT,
    ADD COLUMN external_id_provider TEXT;

CREATE UNIQUE INDEX users_external_id_unique
    ON users (external_id_provider, external_id)
    WHERE external_id IS NOT NULL;
