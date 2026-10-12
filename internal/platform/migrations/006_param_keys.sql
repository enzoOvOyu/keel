-- Parameter catalog (WP-1.11, FR-1.9). Registered parameter keys with typed
-- option lists; version-type options sync from approved, non-retired media.
-- Rollback: DROP TABLE param_keys;
CREATE TABLE param_keys (
 id varchar(32) PRIMARY KEY,
 scope_key varchar NOT NULL CHECK(scope_key = 'global' OR scope_key LIKE 'project:%'),
 key varchar(120) NOT NULL,
 value_type varchar NOT NULL CHECK(value_type IN('string','enum','version')),
 options jsonb NOT NULL DEFAULT '[]',
 default_value varchar NOT NULL DEFAULT '',
 source_media_type varchar NOT NULL DEFAULT '',
 deps jsonb NOT NULL DEFAULT '{}',
 state varchar NOT NULL DEFAULT 'active' CHECK(state IN('active','retired')),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 UNIQUE(scope_key, key)
);
