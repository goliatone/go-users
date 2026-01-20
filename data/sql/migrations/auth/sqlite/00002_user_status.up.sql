-- 00002_user_status.up.sql (SQLite version)
-- Adds status and suspended_at columns to users table
-- Note: SQLite doesn't support multiple ADD COLUMN in single ALTER TABLE

ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (
    status IN ('pending', 'active', 'suspended', 'disabled', 'archived')
);

---bun:split

ALTER TABLE users ADD COLUMN suspended_at TIMESTAMP NULL;

---bun:split

UPDATE users
SET status = 'archived'
WHERE deleted_at IS NOT NULL;
