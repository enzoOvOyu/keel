-- Model import drafts (WP-1.12 extension, FR-14). A scan of one live server
-- produces a draft; drafts are reviewed and published through the existing
-- PublishModelVersion path, so the immutable-version invariant keeps a single
-- creation route. content_hash is computed the same way as published versions
-- for idempotent re-imports.
-- Rollback: DROP TABLE device_model_drafts.
CREATE TABLE device_model_drafts (
 id varchar(32) PRIMARY KEY,
 model_id varchar(32) NOT NULL REFERENCES device_models(id),
 source_host varchar(120) NOT NULL,
 hw_spec jsonb NOT NULL DEFAULT '{}',
 expected_topology jsonb NOT NULL DEFAULT '{}',
 panel_layout jsonb NOT NULL DEFAULT '{}',
 bom_expect jsonb NOT NULL DEFAULT '{}',
 default_role_plan jsonb NOT NULL DEFAULT '{}',
 content_hash varchar(64) NOT NULL,
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 UNIQUE(model_id, content_hash)
);
CREATE INDEX device_model_drafts_model ON device_model_drafts(model_id, created_at DESC);
