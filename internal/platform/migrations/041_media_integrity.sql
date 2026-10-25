-- Integrity is independent of the immutable binding and approval lifecycle.
-- Rollback requires API rollback first, then DROP TABLE media_artifact_integrity; DROP FUNCTION protect_media_integrity().
CREATE TABLE media_artifact_integrity (
 media_id varchar(32) PRIMARY KEY REFERENCES media_artifacts(media_id),
 quarantined_at timestamptz,
 reason varchar(64),
 observed_sha256 varchar(64),
 observed_size bigint,
 mismatch_count integer NOT NULL DEFAULT 0 CHECK(mismatch_count BETWEEN 0 AND 3),
 CHECK ((quarantined_at IS NULL AND mismatch_count=0 AND reason IS NULL AND observed_sha256 IS NULL AND observed_size IS NULL)
     OR (quarantined_at IS NOT NULL AND mismatch_count=3 AND reason IS NOT NULL AND reason='digest_mismatch' AND observed_sha256 IS NOT NULL AND observed_size IS NOT NULL AND observed_sha256 ~ '^[a-f0-9]{64}$' AND observed_size>=0))
);
INSERT INTO media_artifact_integrity(media_id) SELECT media_id FROM media_artifacts;

CREATE FUNCTION protect_media_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'media integrity records cannot be deleted'; END IF;
 IF NEW.media_id<>OLD.media_id OR OLD.quarantined_at IS NOT NULL THEN
  RAISE EXCEPTION 'quarantined media integrity evidence is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER immutable_media_integrity BEFORE UPDATE OR DELETE ON media_artifact_integrity FOR EACH ROW EXECUTE FUNCTION protect_media_integrity();
