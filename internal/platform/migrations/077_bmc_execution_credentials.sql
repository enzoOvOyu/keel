-- BMC is a domain-owned adapter on the existing execution scheduler and leases.
ALTER TABLE exec_executions DROP CONSTRAINT exec_executions_mode_check;
ALTER TABLE exec_executions ADD CONSTRAINT exec_executions_mode_check CHECK(mode IN('sim','kea','fabric','ssh','agent','provision','control','bmc'));
ALTER TABLE exec_task_attempts DROP CONSTRAINT exec_task_attempts_runner_check;
ALTER TABLE exec_task_attempts ADD CONSTRAINT exec_task_attempts_runner_check CHECK(runner IN('sim','kea','fabric','ssh','agent','provision','control','bmc'));

-- The BOM owns batch/scope/vault references; ciphertext is server-only custody.
CREATE TABLE device_credential_versions (
 project_id varchar(32) NOT NULL,
 credential_batch_ref varchar(120) NOT NULL,
 scope varchar(8) NOT NULL CHECK(scope IN('bmc','switch')),
 credential_version varchar(120) NOT NULL,
 expires_at timestamptz NOT NULL,
 vault_path varchar(300) NOT NULL,
 wrapping_key_id varchar(64) NOT NULL,
 ciphertext bytea NOT NULL CHECK(octet_length(ciphertext) BETWEEN 29 AND 8256),
 reason text NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,credential_batch_ref,scope,credential_version),
 FOREIGN KEY(project_id,credential_batch_ref) REFERENCES bom_credential_batches(project_id,credential_batch_ref)
);
CREATE TABLE device_credential_heads (
 project_id varchar(32) NOT NULL,
 credential_batch_ref varchar(120) NOT NULL,
 scope varchar(8) NOT NULL,
 credential_version varchar(120) NOT NULL,
 state_version bigint NOT NULL CHECK(state_version>0),
 verification_state varchar(16) NOT NULL CHECK(verification_state IN('pending','verified')),
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,credential_batch_ref,scope),
 FOREIGN KEY(project_id,credential_batch_ref,scope,credential_version) REFERENCES device_credential_versions(project_id,credential_batch_ref,scope,credential_version)
);
CREATE TABLE device_credential_revocations (
 project_id varchar(32) NOT NULL,
 credential_batch_ref varchar(120) NOT NULL,
 scope varchar(8) NOT NULL,
 credential_version varchar(120) NOT NULL,
 reason text NOT NULL,
 revoked_at timestamptz NOT NULL,
 PRIMARY KEY(project_id,credential_batch_ref,scope,credential_version),
 FOREIGN KEY(project_id,credential_batch_ref,scope,credential_version) REFERENCES device_credential_versions(project_id,credential_batch_ref,scope,credential_version)
);
CREATE TRIGGER device_credential_version_immutable BEFORE UPDATE OR DELETE ON device_credential_versions FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
CREATE TRIGGER device_credential_revocation_immutable BEFORE UPDATE OR DELETE ON device_credential_revocations FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
-- An import cannot silently reinterpret an existing batch in a different scope.
CREATE FUNCTION keel_device_credential_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM bom_credential_batches b WHERE b.project_id=NEW.project_id AND b.credential_batch_ref=NEW.credential_batch_ref AND b.scope=NEW.scope AND b.vault_path=NEW.vault_path) THEN RAISE EXCEPTION 'Credential version must bind the current BOM batch and vault reference'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER device_credential_scope BEFORE INSERT ON device_credential_versions FOR EACH ROW EXECUTE FUNCTION keel_device_credential_scope();

CREATE TABLE bmc_collection_dispatches (
 request_id varchar(32) PRIMARY KEY REFERENCES bmc_collection_requests(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 attempt_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 dispatched_at timestamptz NOT NULL,
 deadline timestamptz NOT NULL,
 CHECK(deadline>dispatched_at AND deadline<=dispatched_at+interval '15 minutes')
);
CREATE TRIGGER bmc_dispatch_immutable BEFORE UPDATE OR DELETE ON bmc_collection_dispatches FOR EACH ROW EXECUTE FUNCTION keel_fabric_evidence_immutable();
