-- Pinned artifact binding; bytes cannot be replaced under an approved version.
-- Rollback: DROP TABLE media_artifacts;
CREATE TABLE media_artifacts (
 media_id varchar(32) PRIMARY KEY REFERENCES media_items(id),
 object_key varchar NOT NULL UNIQUE,
 object_version varchar NOT NULL CHECK(object_version <> '' AND object_version <> 'null'),
 sha256 varchar(64) NOT NULL,
 size bigint NOT NULL CHECK(size >= 0),
 uploaded_by varchar(32) NOT NULL REFERENCES users(id),
 created_at timestamptz NOT NULL
);
CREATE FUNCTION protect_media_artifact() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'media artifact bindings are immutable'; END $$;
CREATE TRIGGER immutable_media_artifact BEFORE UPDATE OR DELETE ON media_artifacts FOR EACH ROW EXECUTE FUNCTION protect_media_artifact();
