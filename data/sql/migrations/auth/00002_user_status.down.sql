-- 00002_user_status.down.sql
-- Removes status and suspended_at columns from users table

ALTER TABLE users
    DROP COLUMN IF EXISTS suspended_at,
    DROP COLUMN IF EXISTS status;
