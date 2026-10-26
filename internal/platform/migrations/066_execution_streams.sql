-- Redis is rebuildable. These rows are publication hints, never task authority.
CREATE TABLE exec_stream_publications (
 event_id varchar(32) PRIMARY KEY REFERENCES exec_outbox(id),
 stream_epoch varchar(32) NOT NULL,
 stream_id varchar(64) NOT NULL,
 last_published_at timestamptz NOT NULL
);
CREATE INDEX exec_stream_publications_retry ON exec_stream_publications(last_published_at);

-- Poison entries are acknowledged only after a durable, payload-free decision.
CREATE TABLE exec_stream_rejections (
 stream_epoch varchar(32) NOT NULL,
 stream_id varchar(64) NOT NULL,
 reason varchar(32) NOT NULL CHECK(reason IN ('invalid_reference','unknown_event','reference_mismatch')),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(stream_epoch,stream_id)
);
