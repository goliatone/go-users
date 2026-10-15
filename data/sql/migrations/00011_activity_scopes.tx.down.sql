DO $$ BEGIN IF EXISTS (SELECT 1 FROM user_activity WHERE scope_key <> '') THEN RAISE EXCEPTION 'cannot discard scoped activity'; END IF; END $$;
DROP INDEX user_activity_exact_scope_idx;
ALTER TABLE user_activity DROP COLUMN scope_key;
ALTER TABLE user_activity DROP COLUMN labels;
