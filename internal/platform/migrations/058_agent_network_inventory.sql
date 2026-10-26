-- Optional observational data; never a gate, credential or execution queue.
CREATE TABLE agent_network_inventory (
 credential_id char(32) PRIMARY KEY REFERENCES agent_credentials(id),
 project_id char(32) NOT NULL REFERENCES projects(id),
 machine_id char(32) NOT NULL REFERENCES machines(id),
 connection_id char(32) NOT NULL REFERENCES agent_connections(id),
 baseline_connection_id char(32) REFERENCES agent_connections(id),
 frame_sequence bigint NOT NULL CHECK(frame_sequence>0),
 certificate_hash char(64) NOT NULL CHECK(certificate_hash ~ '^[0-9a-f]{64}$'),
 state text NOT NULL CHECK(state IN('ready','unavailable','disabled')),
 interfaces jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(interfaces)='array' AND jsonb_array_length(interfaces)<=64),
 failure_code text NOT NULL DEFAULT '' CHECK(failure_code IN('','interface_read_failed','interface_limit_exceeded','interface_state_unavailable','inventory_frame_limit')),
 collected_at timestamptz,
 last_success_at timestamptz,
 received_at timestamptz NOT NULL DEFAULT now(),
 CHECK((collected_at IS NULL)=(last_success_at IS NULL)),
 CHECK((baseline_connection_id IS NULL)=(last_success_at IS NULL)),
 CHECK((state='unavailable')=(failure_code<>'')),
 CHECK(state<>'ready' OR (baseline_connection_id IS NOT NULL AND baseline_connection_id=connection_id AND collected_at IS NOT NULL))
);
CREATE INDEX agent_network_inventory_project ON agent_network_inventory(project_id,machine_id);
