-- 00009_password_reset_processing.down.sql
-- Removes password_reset processing claim status support.

PRAGMA foreign_keys = OFF;

UPDATE password_reset
SET status = 'requested'
WHERE status = 'processing';

CREATE TABLE password_reset_new (
	id TEXT NOT NULL PRIMARY KEY,
	user_id TEXT NOT NULL,
	email TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'unknown' CHECK (
		status IN ('unknown', 'requested', 'expired', 'changed')
	),
	reseted_at TIMESTAMP,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	deleted_at TIMESTAMP,
	updated_at TIMESTAMP,
	jti TEXT,
	issued_at TIMESTAMP,
	expires_at TIMESTAMP,
	used_at TIMESTAMP,
	scope_tenant_id TEXT,
	scope_org_id TEXT,
	FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

INSERT INTO password_reset_new (
	id,
	user_id,
	email,
	status,
	reseted_at,
	created_at,
	deleted_at,
	updated_at,
	jti,
	issued_at,
	expires_at,
	used_at,
	scope_tenant_id,
	scope_org_id
)
SELECT
	id,
	user_id,
	email,
	status,
	reseted_at,
	created_at,
	deleted_at,
	updated_at,
	jti,
	issued_at,
	expires_at,
	used_at,
	scope_tenant_id,
	scope_org_id
FROM password_reset;

DROP TABLE password_reset;
ALTER TABLE password_reset_new RENAME TO password_reset;

CREATE UNIQUE INDEX IF NOT EXISTS password_reset_jti_unique ON password_reset (jti);
CREATE INDEX IF NOT EXISTS password_reset_expiry_status_index ON password_reset (status, expires_at);

PRAGMA foreign_keys = ON;
