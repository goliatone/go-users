-- 00009_user_external_ids.down.sql
-- Removes external ID columns from users.

DROP INDEX IF EXISTS users_external_id_unique;

ALTER TABLE users
    DROP COLUMN IF EXISTS external_id_provider,
    DROP COLUMN IF EXISTS external_id;
