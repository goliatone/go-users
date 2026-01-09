-- 00007_custom_roles_order.down.sql
-- Removes ordering column from custom_roles.

ALTER TABLE custom_roles
    DROP COLUMN IF EXISTS "order";
