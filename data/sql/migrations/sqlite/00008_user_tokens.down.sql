-- 00008_user_tokens.down.sql (SQLite version)
-- Removes user_tokens table and securelink fields from password_reset.

DROP INDEX IF EXISTS password_reset_expiry_status_index;
DROP INDEX IF EXISTS password_reset_jti_unique;

ALTER TABLE password_reset DROP COLUMN scope_org_id;
ALTER TABLE password_reset DROP COLUMN scope_tenant_id;
ALTER TABLE password_reset DROP COLUMN used_at;
ALTER TABLE password_reset DROP COLUMN expires_at;
ALTER TABLE password_reset DROP COLUMN issued_at;
ALTER TABLE password_reset DROP COLUMN jti;

DROP INDEX IF EXISTS user_tokens_expiry_status_index;
DROP INDEX IF EXISTS user_tokens_jti_unique;
DROP INDEX IF EXISTS user_tokens_user_id_index;
DROP TABLE IF EXISTS user_tokens;
