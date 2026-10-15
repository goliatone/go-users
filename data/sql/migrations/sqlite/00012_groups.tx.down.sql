CREATE TEMP TABLE groups_down_guard (n INTEGER CHECK(n=0));
INSERT INTO groups_down_guard SELECT COUNT(*) FROM user_groups;
DROP TABLE groups_down_guard;
DROP TABLE user_group_memberships;
DROP TABLE user_groups;
