-- 00002_user_status.down.sql (SQLite version)
-- Removes status and suspended_at columns from users table
-- Note: SQLite doesn't support DROP COLUMN before version 3.35.0
-- For older SQLite versions, you would need to recreate the table

ALTER TABLE users DROP COLUMN IF EXISTS suspended_at;
ALTER TABLE users DROP COLUMN IF EXISTS status;
