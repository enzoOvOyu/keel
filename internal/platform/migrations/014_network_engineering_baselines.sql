-- Project-scoped, immutable engineering limits supplied by the customer and
-- network engineer. This is the engineering subset of the eventual full
-- network configuration baseline; template/config approval remains separate.
CREATE TABLE network_baseline_versions (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 version_ref varchar(120) NOT NULL,
 state varchar(20) NOT NULL CHECK(state IN('draft','approved')),
 source_ref varchar(300) NOT NULL,
 engineering_limits jsonb NOT NULL,
 customer_approval_ref varchar(300),
 network_approval_ref varchar(300),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 approved_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 approved_at timestamptz,
 UNIQUE(project_id,version_ref),
 CHECK(state='draft' OR (customer_approval_ref IS NOT NULL AND network_approval_ref IS NOT NULL AND approved_by IS NOT NULL AND approved_at IS NOT NULL))
);
CREATE INDEX network_baseline_versions_project ON network_baseline_versions(project_id,state,created_at DESC);
