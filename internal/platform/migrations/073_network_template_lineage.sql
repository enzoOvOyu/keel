-- A rollback restores content into a new draft; historical release evidence is
-- never copied or rewritten. Existing versions remain valid roots.
ALTER TABLE network_template_versions
 ADD COLUMN parent_version_id varchar(32) REFERENCES network_template_versions(id),
 ADD COLUMN rollback_from_id varchar(32) REFERENCES network_template_versions(id),
 ADD COLUMN change_reason varchar(1000) NOT NULL DEFAULT '';

CREATE FUNCTION keel_network_template_lineage() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent network_template_versions%ROWTYPE; restored network_template_versions%ROWTYPE;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF OLD.parent_version_id IS DISTINCT FROM NEW.parent_version_id OR OLD.rollback_from_id IS DISTINCT FROM NEW.rollback_from_id OR OLD.change_reason IS DISTINCT FROM NEW.change_reason THEN
   RAISE EXCEPTION 'Network template lineage is immutable';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.parent_version_id IS NOT NULL THEN
  SELECT * INTO STRICT parent FROM network_template_versions WHERE id=NEW.parent_version_id;
  IF parent.project_id<>NEW.project_id OR parent.name<>NEW.name OR parent.version>=NEW.version THEN
   RAISE EXCEPTION 'Template parent must be an earlier version in the same project and family';
  END IF;
 END IF;
 IF NEW.rollback_from_id IS NOT NULL THEN
  SELECT * INTO STRICT restored FROM network_template_versions WHERE id=NEW.rollback_from_id;
  IF NEW.parent_version_id IS NULL OR restored.project_id<>NEW.project_id OR restored.name<>NEW.name OR restored.version>=parent.version OR restored.state NOT IN('released','deprecated') OR NEW.state<>'draft' OR NEW.test_report IS NOT NULL OR NEW.released_by IS NOT NULL OR NEW.definition IS DISTINCT FROM restored.definition OR NEW.content_hash<>restored.content_hash OR NEW.source_ref<>restored.source_ref OR length(trim(NEW.change_reason))=0 THEN
   RAISE EXCEPTION 'Template rollback must create a new untested draft from a historical release';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER network_template_lineage BEFORE INSERT OR UPDATE ON network_template_versions FOR EACH ROW EXECUTE FUNCTION keel_network_template_lineage();
