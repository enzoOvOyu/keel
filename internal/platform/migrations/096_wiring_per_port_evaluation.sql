-- Independent per-port rule projections retain each original capture. This
-- alone does not authorize partial command scopes or aggregate G2 evidence.
ALTER TABLE validation_wiring_capture_records ADD COLUMN port_evaluations jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE FUNCTION keel_wiring_port_evaluation_scope() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected jsonb; id text; result jsonb;
BEGIN
 SELECT c.command#>'{expectation,ports}' INTO expected FROM validation_wiring_commands c WHERE c.command_id=NEW.command_id;
 IF jsonb_typeof(NEW.port_evaluations) IS DISTINCT FROM 'object' OR (SELECT count(*) FROM jsonb_object_keys(NEW.port_evaluations))<>jsonb_array_length(expected) THEN RAISE EXCEPTION 'capture requires every selected physical port evaluation'; END IF;
 FOR id,result IN SELECT * FROM jsonb_each(NEW.port_evaluations) LOOP
  IF NOT EXISTS(SELECT 1 FROM jsonb_array_elements(expected) p WHERE p->>'physical_port_id'=id) OR NOT (result->>'version'='keel.wiring/v1' AND result->>'rule_version'='wiring-three-source/v1' AND result->>'step2' IN('PASS','FAIL','BLOCKED') AND result->>'step3' IN('PASS','FAIL','BLOCKED') AND (result->>'step2'='PASS' OR result->>'step3'='BLOCKED') AND (NEW.origin='real' OR result->>'step2'<>'PASS')) IS TRUE THEN RAISE EXCEPTION 'per-port evaluation must preserve original scope and source'; END IF;
 END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER validation_wiring_port_evaluation_scope BEFORE INSERT ON validation_wiring_capture_records FOR EACH ROW EXECUTE FUNCTION keel_wiring_port_evaluation_scope();
