DO $$ BEGIN IF EXISTS (SELECT 1 FROM user_groups) THEN RAISE EXCEPTION 'cannot discard native Groups'; END IF; END $$;
DROP TABLE user_group_memberships;
DROP TABLE user_groups;
