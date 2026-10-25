-- A separate consumer of the existing durable execution outbox. No new job queue.
CREATE TABLE notification_webhook_policies (
 sink_id varchar(64) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 event_types jsonb NOT NULL CHECK(jsonb_typeof(event_types)='array' AND jsonb_array_length(event_types) BETWEEN 1 AND 3),
 not_before timestamptz NOT NULL,
 policy_sha256 char(64) NOT NULL CHECK(policy_sha256 ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER immutable_webhook_policy BEFORE UPDATE OR DELETE ON notification_webhook_policies FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
CREATE TABLE notification_webhook_deliveries (
 sink_id varchar(64) NOT NULL REFERENCES notification_webhook_policies(sink_id),
 event_id varchar(32) NOT NULL REFERENCES exec_outbox(id),
 sequence bigint NOT NULL,
 payload jsonb NOT NULL,
 state varchar NOT NULL DEFAULT 'pending' CHECK(state IN('pending','delivering','delivered','dead')),
 attempts integer NOT NULL DEFAULT 0 CHECK(attempts BETWEEN 0 AND 6),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 lease_token varchar(32),
 lease_until timestamptz,
 http_status integer CHECK(http_status BETWEEN 100 AND 599),
 last_error varchar(40) CHECK(last_error IN('transport_unknown','http_rejected','attempts_exhausted')),
 delivered_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(sink_id,event_id),
 CHECK((state='delivering' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR (state<>'delivering' AND lease_token IS NULL AND lease_until IS NULL)),
 CHECK((state='delivered')=(delivered_at IS NOT NULL)),
 CHECK(state<>'delivered' OR (http_status IS NOT NULL AND http_status BETWEEN 200 AND 299))
);
CREATE INDEX webhook_deliveries_due ON notification_webhook_deliveries(sink_id,next_attempt_at,sequence) WHERE state IN('pending','delivering');
CREATE FUNCTION protect_webhook_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' OR ROW(OLD.sink_id,OLD.event_id,OLD.sequence,OLD.payload) IS DISTINCT FROM ROW(NEW.sink_id,NEW.event_id,NEW.sequence,NEW.payload)
 OR OLD.state IN('delivered','dead') THEN RAISE EXCEPTION 'notification identity, payload and terminal evidence are immutable'; END IF;
 IF NEW.attempts<OLD.attempts OR NEW.attempts>OLD.attempts+1
 OR (NEW.state='delivering' AND (NEW.attempts<>OLD.attempts+1 OR (OLD.state='delivering' AND OLD.lease_until>now())))
 OR (NEW.state<>'delivering' AND NEW.attempts<>OLD.attempts)
 OR (OLD.state='pending' AND NEW.state IN('delivered','dead') AND NOT(NEW.state='dead' AND OLD.attempts=6)) THEN
  RAISE EXCEPTION 'notification attempts and claim transitions must be monotonic';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER protect_webhook_delivery BEFORE UPDATE OR DELETE ON notification_webhook_deliveries FOR EACH ROW EXECUTE FUNCTION protect_webhook_delivery();

-- Enforce the same public source projection below the Go consumer, so a queue
-- insert cannot relabel another project's event or add arbitrary metadata.
CREATE FUNCTION validate_webhook_delivery_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected jsonb; event_at timestamptz;
BEGIN
 SELECT jsonb_build_object(
  'specversion','1.0','id',o.id,'source','urn:keel:project:'||p.project_id,
  'type',o.event_type,'subject','execution/'||o.execution_id,'datacontenttype','application/json',
  'data',jsonb_build_object('project_id',p.project_id,'execution_id',o.execution_id,'aggregate_version',o.aggregate_version)
   || CASE WHEN o.payload->>'state' IN('created','scheduled','dispatching','running','pausing','paused','cancelling','reconciling','success','failed','killed','warning')
      THEN jsonb_build_object('state',o.payload->>'state') ELSE '{}'::jsonb END),o.created_at
 INTO expected,event_at
 FROM exec_outbox o JOIN exec_executions e ON e.id=o.execution_id
 JOIN notification_webhook_policies p ON p.sink_id=NEW.sink_id AND p.project_id=e.project_id
 WHERE o.id=NEW.event_id AND o.sequence=NEW.sequence AND o.created_at>=p.not_before
 AND o.event_type IN('execution.state_changed','execution.circuit_breaker','execution.authorization_blocked')
 AND o.event_type IN(SELECT jsonb_array_elements_text(p.event_types));
 IF expected IS NULL OR NEW.payload-'time' IS DISTINCT FROM expected
 OR (NEW.payload->>'time')::timestamptz IS DISTINCT FROM event_at
 OR NEW.state<>'pending' OR NEW.attempts<>0 OR NEW.lease_token IS NOT NULL OR NEW.lease_until IS NOT NULL
 OR NEW.http_status IS NOT NULL OR NEW.last_error IS NOT NULL OR NEW.delivered_at IS NOT NULL THEN
  RAISE EXCEPTION 'notification must be an exact public projection of its bound committed project event';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER validate_webhook_delivery_insert BEFORE INSERT ON notification_webhook_deliveries FOR EACH ROW EXECUTE FUNCTION validate_webhook_delivery_insert();
