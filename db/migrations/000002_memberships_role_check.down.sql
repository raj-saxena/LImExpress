-- 000002_memberships_role_check.down.sql

ALTER TABLE memberships
    DROP CONSTRAINT IF EXISTS memberships_role_check;
