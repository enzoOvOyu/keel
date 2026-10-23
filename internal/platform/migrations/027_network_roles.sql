-- Independent of the unreleased N-S5/026 draft: extend the existing project
-- role constraint without changing users, memberships or privilege assignments.
ALTER TABLE memberships DROP CONSTRAINT memberships_role_check;
ALTER TABLE memberships ADD CONSTRAINT memberships_role_check
 CHECK (role IN ('Admin','DE','DL','NE','SRE','Viewer','NDE','NH'));
