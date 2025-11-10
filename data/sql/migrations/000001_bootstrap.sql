-- 000001_bootstrap.sql
-- Placeholder migration that creates a bookkeeping table so smoke tests can
-- execute against SQLite. Future migrations will replace this with real schema.

CREATE TABLE IF NOT EXISTS user_migration_bootstrap (
    id INTEGER PRIMARY KEY,
    applied_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
