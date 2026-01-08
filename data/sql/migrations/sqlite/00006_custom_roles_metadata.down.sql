-- 00006_custom_roles_metadata.down.sql (SQLite version)
-- Removes role_key and metadata from custom_roles.
-- Note: SQLite doesn't support DROP COLUMN before version 3.35.0
-- For older SQLite versions, you would need to recreate the table.

ALTER TABLE custom_roles DROP COLUMN IF EXISTS metadata;
ALTER TABLE custom_roles DROP COLUMN IF EXISTS role_key;
