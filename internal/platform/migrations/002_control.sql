-- Identity and project control: optimistic member versions and bounded query access paths.
ALTER TABLE memberships ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
CREATE INDEX users_created_id ON users(created_at DESC,id DESC);
CREATE INDEX projects_created_id ON projects(created_at,id);
CREATE INDEX memberships_project_created_id ON memberships(project_id,created_at,id);
CREATE INDEX audits_created_id ON audits(created_at DESC,id DESC);
CREATE INDEX audits_action_created_id ON audits(action,created_at DESC,id DESC);
