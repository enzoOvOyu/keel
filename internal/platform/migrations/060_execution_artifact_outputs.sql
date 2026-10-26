-- Captured bytes are immutable and become downloadable only with the matching
-- successful target receipt. An uploaded object alone never proves retrieval.
CREATE TABLE exec_artifact_outputs (
 attempt_id varchar(32) PRIMARY KEY REFERENCES exec_task_attempts(id),
 execution_id varchar(32) NOT NULL REFERENCES exec_executions(id),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 command_id varchar(32) NOT NULL UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint NOT NULL CHECK(fencing_token>0),
 media_id varchar(32) NOT NULL REFERENCES media_items(id),
 sha256 varchar(64) NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 size bigint NOT NULL CHECK(size>0 AND size<=1073741824),
 object_key text NOT NULL CHECK(length(object_key) BETWEEN 1 AND 256),
 object_version text NOT NULL CHECK(length(object_version) BETWEEN 1 AND 1024),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX exec_artifact_outputs_project ON exec_artifact_outputs(project_id,execution_id);
CREATE TRIGGER exec_artifact_output_immutable BEFORE UPDATE OR DELETE ON exec_artifact_outputs FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
