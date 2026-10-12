-- Initial ent schemas. IDs are opaque random hex; all timestamps are UTC instants.
CREATE TABLE users (
 id varchar(32) PRIMARY KEY, username varchar(128) NOT NULL UNIQUE, display_name varchar(120) NOT NULL,
 password_hash varchar NOT NULL, is_admin boolean NOT NULL DEFAULT false, enabled boolean NOT NULL DEFAULT true,
 security_version bigint NOT NULL DEFAULT 1, created_at timestamptz NOT NULL,
 CHECK (security_version > 0)
);
CREATE TABLE sessions (
 id varchar(64) PRIMARY KEY, user_id varchar(32) REFERENCES users(id),
 csrf_hash varchar(64) NOT NULL, security_version bigint NOT NULL DEFAULT 0,
 issued_at timestamptz NOT NULL, last_seen_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 revoked_at timestamptz
);
CREATE INDEX sessions_user_id ON sessions(user_id);
CREATE INDEX sessions_expires_at ON sessions(expires_at);
CREATE TABLE login_throttles (
 id varchar(64) PRIMARY KEY, failures bigint NOT NULL DEFAULT 0,
 window_started_at timestamptz NOT NULL, locked_until timestamptz
);
CREATE TABLE projects (
 id varchar(32) PRIMARY KEY, name varchar(120) NOT NULL, slug varchar(64) NOT NULL UNIQUE,
 description varchar NOT NULL DEFAULT '', state varchar NOT NULL DEFAULT 'planning',
 created_by varchar(32) NOT NULL REFERENCES users(id), version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 CHECK (state IN ('planning','deploying','verifying','delivered','archived')), CHECK (version > 0)
);
CREATE TABLE memberships (
 id varchar(32) PRIMARY KEY, user_id varchar(32) NOT NULL REFERENCES users(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id), role varchar NOT NULL,
 created_at timestamptz NOT NULL,
 CONSTRAINT membership_user_id_project_id UNIQUE(user_id,project_id),
 CHECK (role IN ('Admin','DE','DL','NE','SRE','Viewer'))
);
CREATE INDEX memberships_project_id ON memberships(project_id);
CREATE TABLE audits (
 id varchar(32) PRIMARY KEY, actor_id varchar(32), project_id varchar(32),
 action varchar(64) NOT NULL, target_id varchar, trace_id varchar(64), metadata jsonb, created_at timestamptz NOT NULL
);
CREATE INDEX audits_project_created ON audits(project_id,created_at);
CREATE TABLE idempotencies (
 id varchar(64) PRIMARY KEY, actor_id varchar(32) NOT NULL REFERENCES users(id),
 request_hash varchar(64) NOT NULL, response jsonb NOT NULL, created_at timestamptz NOT NULL
);
-- Append-only at the database boundary. Owners/superusers can disable triggers;
-- production additionally requires separate migration/application roles and archival copies.
CREATE FUNCTION keel_audit_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'audit records are append-only'; END $$;
CREATE TRIGGER audits_append_only BEFORE UPDATE OR DELETE ON audits
 FOR EACH ROW EXECUTE FUNCTION keel_audit_append_only();
