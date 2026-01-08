-- 00006_custom_roles_metadata.down.sql
-- Removes role_key and metadata from custom_roles.

ALTER TABLE custom_roles
    DROP COLUMN IF EXISTS metadata,
    DROP COLUMN IF EXISTS role_key;
