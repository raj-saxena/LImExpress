-- 000002_memberships_role_check.up.sql

ALTER TABLE memberships
    ADD CONSTRAINT memberships_role_check CHECK (role IN ('member', 'org_admin'));
