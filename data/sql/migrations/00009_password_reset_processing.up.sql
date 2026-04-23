-- 00009_password_reset_processing.up.sql
-- Allows password_reset records to use a temporary processing claim status.

ALTER TABLE password_reset
	DROP CONSTRAINT IF EXISTS password_reset_status_check;

ALTER TABLE password_reset
	ADD CONSTRAINT password_reset_status_check CHECK (
		status IN ('unknown', 'requested', 'processing', 'expired', 'changed')
	);
