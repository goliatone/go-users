CREATE TEMP TABLE activity_scope_guard (n INTEGER CHECK(n=0));
INSERT INTO activity_scope_guard SELECT COUNT(*) FROM user_activity WHERE scope_key <> '';
DROP TABLE activity_scope_guard;
DROP INDEX user_activity_exact_scope_idx;
ALTER TABLE user_activity DROP COLUMN scope_key;
ALTER TABLE user_activity DROP COLUMN labels;
