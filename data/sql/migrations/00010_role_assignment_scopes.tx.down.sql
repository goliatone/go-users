-- Refuse downgrade while labeled grants exist; never collapse or discard them.
ALTER TABLE user_custom_roles ADD CONSTRAINT user_custom_roles_unlabeled_check CHECK (scope_key = '');
DROP INDEX user_custom_roles_scope_key_idx;
ALTER TABLE user_custom_roles DROP CONSTRAINT user_custom_roles_pkey;
ALTER TABLE user_custom_roles DROP COLUMN scope_key;
ALTER TABLE user_custom_roles DROP COLUMN labels;
ALTER TABLE user_custom_roles ADD PRIMARY KEY (user_id, role_id, tenant_id, org_id);
