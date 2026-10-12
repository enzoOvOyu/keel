-- Media library (WP-1.10, FR-1.8). Registered media with an auditable
-- lifecycle: uploaded -> verified -> approved -> retired (one-way).
-- A media item is registry metadata; artifact bytes live in object storage.
-- Rollback: DROP TABLE media_items;
CREATE TABLE media_items (
 id varchar(32) PRIMARY KEY,
 scope_key varchar NOT NULL CHECK(scope_key = 'global' OR scope_key LIKE 'project:%'),
 type varchar NOT NULL CHECK(type IN('driver','firmware','os','nccl','tool','config')),
 name varchar(120) NOT NULL,
 version varchar(120) NOT NULL,
 sha256 varchar(64) NOT NULL,
 size bigint NOT NULL CHECK(size >= 0),
 arch varchar NOT NULL CHECK(arch IN('amd64','arm64','noarch')),
 os_scope varchar NOT NULL DEFAULT '',
 hw_scope varchar NOT NULL DEFAULT '',
 deps jsonb NOT NULL DEFAULT '{}',
 manifest_ref varchar NOT NULL DEFAULT '',
 signature_ref varchar NOT NULL DEFAULT '',
 state varchar NOT NULL DEFAULT 'uploaded' CHECK(state IN('uploaded','verified','approved','retired')),
 uploaded_by varchar(32) NOT NULL REFERENCES users(id),
 approved_by varchar(32) REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(scope_key, type, name, version)
);
CREATE INDEX media_items_scope ON media_items(scope_key, type, name);
CREATE INDEX media_items_state ON media_items(state);
