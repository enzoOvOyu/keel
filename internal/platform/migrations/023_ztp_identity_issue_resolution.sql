ALTER TABLE ztp_identity_issues ADD COLUMN state varchar(12) NOT NULL DEFAULT 'open' CHECK(state IN ('open','resolved'));
ALTER TABLE ztp_identity_issues ADD COLUMN resolved_by varchar(32) REFERENCES users(id);
ALTER TABLE ztp_identity_issues ADD COLUMN resolved_at timestamptz;
ALTER TABLE ztp_identity_issues ADD COLUMN resolution_reason varchar(300);
ALTER TABLE ztp_identity_issues ADD CONSTRAINT ztp_identity_issue_resolution_consistent CHECK (
 (state='open' AND resolved_by IS NULL AND resolved_at IS NULL AND resolution_reason IS NULL)
 OR (state='resolved' AND resolved_by IS NOT NULL AND resolved_at IS NOT NULL AND resolution_reason IS NOT NULL)
);
