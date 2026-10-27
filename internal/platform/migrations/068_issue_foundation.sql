-- WP-2.7: stable identity based issue foundation. Revalidation is attached by
-- its owning domain later; no client-supplied PASS can close an issue.
CREATE TABLE issues (
 id varchar(32) PRIMARY KEY,
 project_id varchar(32) NOT NULL REFERENCES projects(id),
 title varchar(240) NOT NULL CHECK(length(btrim(title))>0),
 category varchar(2) NOT NULL CHECK(category IN('T1','T2','T3','T4','T5','T6','T7')),
 source varchar(16) NOT NULL CHECK(source IN('topology','g2','fabric','g5','g6','portmon','bmc')),
 phase varchar(16) NOT NULL CHECK(phase IN('delivery','post-delivery')),
 responsible_domain varchar(8) NOT NULL DEFAULT 'unknown' CHECK(responsible_domain IN('host','fabric','unknown')),
 state varchar(16) NOT NULL DEFAULT 'open' CHECK(state IN('open','assigned','in_repair','verify','closed','reopened')),
 anchors jsonb NOT NULL CHECK(jsonb_typeof(anchors)='array' AND jsonb_array_length(anchors) BETWEEN 1 AND 32),
 evidence jsonb NOT NULL CHECK(jsonb_typeof(evidence)='object'),
 assignee_id varchar(32) REFERENCES users(id),
 created_by varchar(32) NOT NULL REFERENCES users(id),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(phase<>'post-delivery' OR source='bmc'),
 CHECK(state='open' OR assignee_id IS NOT NULL)
);
CREATE INDEX issues_project_state ON issues(project_id,state,updated_at DESC,id DESC);
CREATE INDEX issues_recent ON issues(updated_at DESC,id DESC);
CREATE TABLE issue_events (
 id varchar(32) PRIMARY KEY,
 issue_id varchar(32) NOT NULL REFERENCES issues(id),
 version bigint NOT NULL CHECK(version>0),
 actor_id varchar(32) NOT NULL REFERENCES users(id),
 action varchar(24) NOT NULL,
 data jsonb NOT NULL,
 created_at timestamptz NOT NULL,
 UNIQUE(issue_id,version)
);
CREATE FUNCTION keel_issue_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Issue history cannot be deleted'; END IF;
 IF ROW(NEW.id,NEW.project_id,NEW.title,NEW.category,NEW.source,NEW.phase,NEW.anchors,NEW.evidence,NEW.created_by,NEW.created_at)
 IS DISTINCT FROM ROW(OLD.id,OLD.project_id,OLD.title,OLD.category,OLD.source,OLD.phase,OLD.anchors,OLD.evidence,OLD.created_by,OLD.created_at)
 OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'Issue identity and discovery evidence are immutable; version must advance'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_issue_identity BEFORE UPDATE OR DELETE ON issues FOR EACH ROW EXECUTE FUNCTION keel_issue_identity_immutable();
CREATE TRIGGER immutable_issue_events BEFORE UPDATE OR DELETE ON issue_events FOR EACH ROW EXECUTE FUNCTION keel_exec_immutable();
