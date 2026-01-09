-- 00007_custom_roles_order.down.sql (SQLite version)
-- Removes ordering column from custom_roles.

ALTER TABLE custom_roles DROP COLUMN IF EXISTS "order";
