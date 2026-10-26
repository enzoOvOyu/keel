-- Separate actual installation from immutable source-preparation evidence.
-- No listener, credential issuance or device operation is enabled by migration.
CREATE TABLE provision_installations (
 id varchar(32) PRIMARY KEY CHECK(id ~ '^[a-f0-9]{32}$'),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 request_id varchar(32) NOT NULL UNIQUE REFERENCES provision_requests(id),
 machine_id varchar(32) NOT NULL REFERENCES machines(id),
 execution_id varchar(32) NOT NULL UNIQUE REFERENCES exec_executions(id),
 scope_id varchar(32) NOT NULL REFERENCES provision_boot_scopes(id),
 scope_sha256 char(64) NOT NULL CHECK(scope_sha256 ~ '^[a-f0-9]{64}$'),
 snapshot_sha256 char(64) NOT NULL CHECK(snapshot_sha256 ~ '^[a-f0-9]{64}$'),
 grant_id varchar(32) NOT NULL UNIQUE CHECK(grant_id ~ '^[a-f0-9]{32}$'),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at AND expires_at<=created_at+interval '30 minutes'),
 state varchar NOT NULL DEFAULT 'queued' CHECK(state IN('queued','booting','preflight_ready','awaiting_storage_review','storage_approved','installing','awaiting_first_boot','registered','failed','canceled','unknown')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 attempt_id varchar(32) UNIQUE REFERENCES exec_task_attempts(id),
 command_id varchar(32) UNIQUE REFERENCES exec_task_attempts(command_id),
 fencing_token bigint CHECK(fencing_token>0),
 spec jsonb,
 boot_id varchar(32) CHECK(boot_id ~ '^[a-f0-9]{32}$'),
 boot_public_key bytea CHECK(octet_length(boot_public_key)=32),
 callback_sequence bigint NOT NULL DEFAULT 0 CHECK(callback_sequence BETWEEN 0 AND 4096),
 latest_report bytea CHECK(octet_length(latest_report) BETWEEN 1 AND 2097152),
 latest_report_sha256 char(64) CHECK(latest_report_sha256 ~ '^[a-f0-9]{64}$'),
 storage_plan jsonb,
 storage_plan_sha256 char(64) CHECK(storage_plan_sha256 ~ '^[a-f0-9]{64}$'),
 storage_requested_by varchar(32) REFERENCES users(id),
 storage_requested_at timestamptz,
 storage_approved_by varchar(32) REFERENCES users(id),
 storage_approved_at timestamptz,
 permit_issued_at timestamptz,
 failure_reason varchar(120),
 updated_at timestamptz NOT NULL,
 CHECK((attempt_id IS NULL AND command_id IS NULL AND fencing_token IS NULL AND spec IS NULL) OR (attempt_id IS NOT NULL AND command_id IS NOT NULL AND fencing_token IS NOT NULL AND spec IS NOT NULL)),
 CHECK(state NOT IN('booting','preflight_ready','awaiting_storage_review','storage_approved','installing','awaiting_first_boot','registered','unknown') OR attempt_id IS NOT NULL),
 CHECK((boot_id IS NULL AND boot_public_key IS NULL AND callback_sequence=0) OR (boot_id IS NOT NULL AND boot_public_key IS NOT NULL AND callback_sequence>0)),
 CHECK((latest_report IS NULL)=(latest_report_sha256 IS NULL)),
 CHECK((storage_plan IS NULL AND storage_plan_sha256 IS NULL AND storage_requested_by IS NULL AND storage_requested_at IS NULL) OR (storage_plan IS NOT NULL AND storage_plan_sha256 IS NOT NULL AND storage_requested_by IS NOT NULL AND storage_requested_at IS NOT NULL)),
 CHECK((storage_approved_by IS NULL)=(storage_approved_at IS NULL)),
 CHECK(storage_approved_by IS NULL OR (storage_approved_by<>created_by AND storage_approved_by<>storage_requested_by AND storage_plan IS NOT NULL)),
 CHECK(state NOT IN('storage_approved','installing','awaiting_first_boot','registered') OR storage_approved_by IS NOT NULL),
 CHECK(state NOT IN('installing','awaiting_first_boot','registered') OR permit_issued_at IS NOT NULL)
);
CREATE UNIQUE INDEX provision_installation_active_machine ON provision_installations(machine_id) WHERE state NOT IN('registered','failed','canceled');
CREATE INDEX provision_installations_project ON provision_installations(project_id,created_at DESC,id);
CREATE TABLE provision_boot_tokens (
 id varchar(32) PRIMARY KEY CHECK(id ~ '^[a-f0-9]{32}$'),
 installation_id varchar(32) NOT NULL REFERENCES provision_installations(id),
 token_hash char(64) NOT NULL UNIQUE CHECK(token_hash ~ '^[a-f0-9]{64}$'),
 reason varchar(500) NOT NULL CHECK(length(reason)>0),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
 consumed_at timestamptz,
 revoked_at timestamptz,
 CHECK(consumed_at IS NULL OR (revoked_at IS NULL AND consumed_at>=created_at AND consumed_at<expires_at)),
 CHECK(revoked_at IS NULL OR (consumed_at IS NULL AND revoked_at>=created_at))
);
CREATE UNIQUE INDEX provision_one_active_boot_token ON provision_boot_tokens(installation_id) WHERE consumed_at IS NULL AND revoked_at IS NULL;
CREATE TABLE provision_installation_events (
 id bigserial PRIMARY KEY,
 installation_id varchar(32) NOT NULL REFERENCES provision_installations(id),
 event_type varchar(64) NOT NULL,
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object' AND octet_length(payload::text)<=16384),
 created_at timestamptz NOT NULL
);
CREATE INDEX provision_installation_events_order ON provision_installation_events(installation_id,id);
CREATE FUNCTION protect_provision_installation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'installation evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.request_id,NEW.machine_id,NEW.execution_id,NEW.scope_id,NEW.scope_sha256,NEW.snapshot_sha256,NEW.grant_id,NEW.created_by,NEW.reason,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.request_id,OLD.machine_id,OLD.execution_id,OLD.scope_id,OLD.scope_sha256,OLD.snapshot_sha256,OLD.grant_id,OLD.created_by,OLD.reason,OLD.created_at,OLD.expires_at)
 OR NEW.state_version<>OLD.state_version+1 OR NEW.callback_sequence<OLD.callback_sequence OR NEW.callback_sequence>OLD.callback_sequence+1
 OR OLD.state IN('registered','failed','canceled') THEN RAISE EXCEPTION 'installation identity, terminal evidence and versions are immutable'; END IF;
 IF OLD.attempt_id IS NOT NULL AND ROW(NEW.attempt_id,NEW.command_id,NEW.fencing_token,NEW.spec) IS DISTINCT FROM ROW(OLD.attempt_id,OLD.command_id,OLD.fencing_token,OLD.spec) THEN RAISE EXCEPTION 'installation attempt is immutable'; END IF;
 IF OLD.attempt_id IS NULL AND NEW.attempt_id IS NOT NULL AND NOT(OLD.state='queued' AND NEW.state='booting') THEN RAISE EXCEPTION 'attempt assigned only at boot activation'; END IF;
 IF OLD.boot_id IS NOT NULL AND ROW(NEW.boot_id,NEW.boot_public_key) IS DISTINCT FROM ROW(OLD.boot_id,OLD.boot_public_key) THEN RAISE EXCEPTION 'enrolled boot identity is immutable'; END IF;
 IF OLD.boot_id IS NULL AND NEW.boot_id IS NOT NULL AND NOT(OLD.state='booting' AND NEW.state='booting' AND NEW.callback_sequence=1) THEN RAISE EXCEPTION 'boot identity assigned only by enrollment'; END IF;
 IF OLD.storage_plan IS NOT NULL AND ROW(NEW.storage_plan,NEW.storage_plan_sha256,NEW.storage_requested_by,NEW.storage_requested_at) IS DISTINCT FROM ROW(OLD.storage_plan,OLD.storage_plan_sha256,OLD.storage_requested_by,OLD.storage_requested_at) THEN RAISE EXCEPTION 'selected storage plan is immutable'; END IF;
 IF OLD.storage_plan IS NULL AND NEW.storage_plan IS NOT NULL AND NOT(OLD.state='preflight_ready' AND NEW.state='awaiting_storage_review') THEN RAISE EXCEPTION 'select storage only after verified preflight'; END IF;
 IF OLD.storage_approved_by IS NOT NULL AND ROW(NEW.storage_approved_by,NEW.storage_approved_at) IS DISTINCT FROM ROW(OLD.storage_approved_by,OLD.storage_approved_at) THEN RAISE EXCEPTION 'storage approval is immutable'; END IF;
 IF OLD.storage_approved_by IS NULL AND NEW.storage_approved_by IS NOT NULL AND NOT(OLD.state='awaiting_storage_review' AND NEW.state='storage_approved') THEN RAISE EXCEPTION 'approve only a selected storage plan'; END IF;
 IF OLD.permit_issued_at IS NOT NULL AND NEW.permit_issued_at IS DISTINCT FROM OLD.permit_issued_at THEN RAISE EXCEPTION 'first write permit evidence is immutable'; END IF;
 IF OLD.permit_issued_at IS NULL AND NEW.permit_issued_at IS NOT NULL AND NOT(OLD.state='storage_approved' AND NEW.state='installing') THEN RAISE EXCEPTION 'permit assigned only after storage approval'; END IF;
 IF NOT((OLD.state=NEW.state AND OLD.state IN('booting','preflight_ready','installing'))
 OR (OLD.state='queued' AND NEW.state IN('booting','failed','canceled'))
 OR (OLD.state='booting' AND NEW.state IN('preflight_ready','failed','canceled','unknown'))
 OR (OLD.state='preflight_ready' AND NEW.state IN('awaiting_storage_review','failed','canceled','unknown'))
 OR (OLD.state='awaiting_storage_review' AND NEW.state IN('storage_approved','failed','canceled','unknown'))
 OR (OLD.state='storage_approved' AND NEW.state IN('installing','failed','canceled','unknown'))
 OR (OLD.state='installing' AND NEW.state IN('awaiting_first_boot','failed','unknown'))
 OR (OLD.state='awaiting_first_boot' AND NEW.state IN('registered','failed','unknown'))
 OR (OLD.state='unknown' AND NEW.state IN('registered','failed','canceled'))) THEN RAISE EXCEPTION 'invalid installation transition'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_provision_installation BEFORE UPDATE OR DELETE ON provision_installations FOR EACH ROW EXECUTE FUNCTION protect_provision_installation();
CREATE FUNCTION protect_provision_boot_token() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'boot token evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.installation_id,NEW.token_hash,NEW.reason,NEW.created_at,NEW.expires_at) IS DISTINCT FROM ROW(OLD.id,OLD.installation_id,OLD.token_hash,OLD.reason,OLD.created_at,OLD.expires_at)
 OR OLD.consumed_at IS NOT NULL OR OLD.revoked_at IS NOT NULL OR ((NEW.consumed_at IS NULL)=(NEW.revoked_at IS NULL)) THEN RAISE EXCEPTION 'boot token is immutable and single-use'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_provision_boot_token BEFORE UPDATE OR DELETE ON provision_boot_tokens FOR EACH ROW EXECUTE FUNCTION protect_provision_boot_token();
CREATE FUNCTION protect_provision_installation_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'installation events are append-only'; END $$;
CREATE TRIGGER protect_provision_installation_event BEFORE UPDATE OR DELETE ON provision_installation_events FOR EACH ROW EXECUTE FUNCTION protect_provision_installation_event();
