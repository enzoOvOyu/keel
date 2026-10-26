-- Server boot domains are independently approved, separate from network-team
-- Kea pools. This registry does not activate any listener or allocate a lease.
CREATE TABLE provision_boot_scopes (
 id varchar(32) PRIMARY KEY CHECK(id ~ '^[a-f0-9]{32}$'),
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 scope jsonb NOT NULL CHECK(jsonb_typeof(scope)='object'),
 scope_sha256 char(64) NOT NULL CHECK(scope_sha256 ~ '^[a-f0-9]{64}$'),
 source_ref varchar(300) NOT NULL CHECK(length(source_ref)>0),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL,
 state varchar NOT NULL DEFAULT 'requested' CHECK(state IN('requested','approved','revoked')),
 state_version bigint NOT NULL DEFAULT 1 CHECK(state_version>0),
 approved_by varchar(32) REFERENCES users(id),
 approved_at timestamptz,
 customer_approval_ref varchar(300),
 network_approval_ref varchar(300),
 revoked_by varchar(32) REFERENCES users(id),
 revoked_at timestamptz,
 revocation_reason varchar(500),
 CHECK((approved_by IS NULL)=(approved_at IS NULL)),
 CHECK(approved_by IS NULL OR approved_by<>created_by),
 CHECK((approved_by IS NULL AND approved_at IS NULL AND customer_approval_ref IS NULL AND network_approval_ref IS NULL)
    OR (approved_by IS NOT NULL AND approved_at IS NOT NULL AND COALESCE(length(customer_approval_ref),0)>0 AND COALESCE(length(network_approval_ref),0)>0)),
 CHECK(state<>'approved' OR approved_by IS NOT NULL),
 CHECK((state='revoked' AND revoked_by IS NOT NULL AND revoked_at IS NOT NULL AND COALESCE(length(revocation_reason),0)>0)
    OR (state<>'revoked' AND revoked_by IS NULL AND revoked_at IS NULL AND revocation_reason IS NULL))
);
CREATE INDEX provision_boot_scopes_project ON provision_boot_scopes(project_id,created_at DESC,id);
CREATE FUNCTION protect_provision_boot_scope() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'boot scope evidence cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.scope,NEW.scope_sha256,NEW.source_ref,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.scope,OLD.scope_sha256,OLD.source_ref,OLD.created_by,OLD.created_at)
 OR NEW.state_version<>OLD.state_version+1
 OR NOT((OLD.state='requested' AND NEW.state IN('approved','revoked')) OR (OLD.state='approved' AND NEW.state='revoked')) THEN
  RAISE EXCEPTION 'boot scope identity and state transitions are immutable';
 END IF;
 IF OLD.approved_by IS NOT NULL AND ROW(NEW.approved_by,NEW.approved_at,NEW.customer_approval_ref,NEW.network_approval_ref)
 IS DISTINCT FROM ROW(OLD.approved_by,OLD.approved_at,OLD.customer_approval_ref,OLD.network_approval_ref) THEN
  RAISE EXCEPTION 'boot scope approval is immutable';
 END IF;
 IF OLD.approved_by IS NULL AND NEW.approved_by IS NOT NULL AND NEW.state<>'approved' THEN
  RAISE EXCEPTION 'approval may only be assigned during approval';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_provision_boot_scope BEFORE UPDATE OR DELETE ON provision_boot_scopes FOR EACH ROW EXECUTE FUNCTION protect_provision_boot_scope();
