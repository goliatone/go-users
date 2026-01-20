-- 00002_user_status.up.sql
-- Adds status and suspended_at columns to users table

ALTER TABLE users
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK (
        status IN ('pending', 'active', 'suspended', 'disabled', 'archived')
    ),
    ADD COLUMN suspended_at TIMESTAMP NULL;

---bun:split

UPDATE users
SET status = 'archived'
WHERE deleted_at IS NOT NULL;
