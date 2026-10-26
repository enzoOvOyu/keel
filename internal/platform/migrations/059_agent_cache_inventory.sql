-- Observational ownership index, never executable approval or content proof.
CREATE TABLE agent_cache_inventory (
 credential_id char(32) PRIMARY KEY REFERENCES agent_credentials(id),
 project_id char(32) NOT NULL REFERENCES projects(id),
 machine_id char(32) NOT NULL REFERENCES machines(id),
 connection_id char(32) NOT NULL REFERENCES agent_connections(id),
 frame_sequence bigint NOT NULL CHECK(frame_sequence>0),
 certificate_hash char(64) NOT NULL CHECK(certificate_hash ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN('ready','unavailable','disabled')),
 manifest_sha256 text NOT NULL DEFAULT '' CHECK(manifest_sha256='' OR manifest_sha256 ~ '^[0-9a-f]{64}$'),
 entries jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(entries)='array' AND jsonb_array_length(entries)<=64),
 failure_code text NOT NULL DEFAULT '' CHECK(failure_code IN('','cache_directory_unavailable','cache_index_unavailable','cache_index_busy','cache_observation_interrupted')),
 collected_at timestamptz,
 last_success_at timestamptz,
 received_at timestamptz NOT NULL DEFAULT now(),
 CHECK((collected_at IS NULL)=(last_success_at IS NULL)),
 CHECK((state='unavailable')=(failure_code<>'')),
 CHECK(state<>'ready' OR (manifest_sha256<>'' AND collected_at IS NOT NULL))
);
CREATE INDEX agent_cache_inventory_project ON agent_cache_inventory(project_id,machine_id);
