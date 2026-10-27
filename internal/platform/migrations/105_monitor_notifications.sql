-- Reuse the existing notification dispatcher and delivery ledger, without
-- putting observational alerts into the execution scheduler's outbox.
ALTER TABLE notification_webhook_policies DROP CONSTRAINT notification_webhook_policies_event_types_check;
ALTER TABLE notification_webhook_policies ADD CONSTRAINT notification_webhook_policies_event_types_check CHECK(jsonb_typeof(event_types)='array' AND jsonb_array_length(event_types) BETWEEN 1 AND 5);
ALTER TABLE notification_webhook_deliveries DROP CONSTRAINT notification_webhook_deliveries_event_id_fkey;
ALTER TABLE notification_webhook_deliveries ADD COLUMN execution_event_id varchar(32) GENERATED ALWAYS AS(CASE WHEN payload->>'type'<>'monitor.alert' THEN event_id END)STORED REFERENCES exec_outbox(id);
ALTER TABLE notification_webhook_deliveries ADD COLUMN monitor_event_id varchar(32) GENERATED ALWAYS AS(CASE WHEN payload->>'type'='monitor.alert' THEN event_id END)STORED REFERENCES monitor_notification_outbox(id);
ALTER TABLE notification_webhook_deliveries ADD CHECK((execution_event_id IS NULL)<>(monitor_event_id IS NULL));
CREATE OR REPLACE FUNCTION validate_webhook_delivery_insert() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected jsonb; event_at timestamptz;
BEGIN
 IF NEW.payload->>'type'='monitor.alert' THEN
  SELECT jsonb_build_object('specversion','1.0','id',o.id,'source','urn:keel:project:'||p.project_id,'type',o.event_type,'subject','alert/'||o.alert_id,'datacontenttype','application/json','data',jsonb_build_object('project_id',p.project_id,'execution_id','','aggregate_version',o.aggregate_version,'alert_id',o.alert_id,'severity',o.severity,'observation_source',o.source,'issue_id',coalesce(o.issue_id,''))),o.created_at INTO expected,event_at
  FROM monitor_notification_outbox o JOIN notification_webhook_policies p ON p.sink_id=NEW.sink_id AND p.project_id=o.project_id
  WHERE o.id=NEW.event_id AND o.sequence=NEW.sequence AND o.created_at>=p.not_before AND o.event_type IN(SELECT jsonb_array_elements_text(p.event_types));
 ELSE
  SELECT jsonb_build_object('specversion','1.0','id',o.id,'source','urn:keel:project:'||p.project_id,'type',o.event_type,'subject',CASE WHEN o.event_type='issue.escalated' THEN 'issue/'||(o.payload->>'issue_id') ELSE 'execution/'||o.execution_id END,'datacontenttype','application/json','data',jsonb_build_object('project_id',p.project_id,'execution_id',o.execution_id,'aggregate_version',o.aggregate_version)
  || CASE WHEN o.payload->>'state' IN('created','scheduled','dispatching','running','pausing','paused','cancelling','reconciling','success','failed','killed','warning') THEN jsonb_build_object('state',o.payload->>'state') ELSE '{}'::jsonb END
  || CASE WHEN o.event_type='issue.escalated' THEN jsonb_build_object('issue_id',o.payload->>'issue_id','failed_verifications',2) ELSE '{}'::jsonb END),o.created_at INTO expected,event_at
  FROM exec_outbox o JOIN exec_executions e ON e.id=o.execution_id JOIN notification_webhook_policies p ON p.sink_id=NEW.sink_id AND p.project_id=e.project_id
  WHERE o.id=NEW.event_id AND o.sequence=NEW.sequence AND o.created_at>=p.not_before AND o.event_type IN('execution.state_changed','execution.circuit_breaker','execution.authorization_blocked','issue.escalated') AND o.event_type IN(SELECT jsonb_array_elements_text(p.event_types));
 END IF;
 IF expected IS NULL OR NEW.payload-'time' IS DISTINCT FROM expected OR (NEW.payload->>'time')::timestamptz IS DISTINCT FROM event_at OR NEW.state<>'pending' OR NEW.attempts<>0 OR NEW.lease_token IS NOT NULL OR NEW.lease_until IS NOT NULL OR NEW.http_status IS NOT NULL OR NEW.last_error IS NOT NULL OR NEW.delivered_at IS NOT NULL THEN RAISE EXCEPTION 'notification must retain the exact public source projection'; END IF;
 RETURN NEW;
END $$;
