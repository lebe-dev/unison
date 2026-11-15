-- Blob metadata: one row per complete or partial blob. Metadata of an evicted
-- blob is removed with it, so a missing row means the content is not stored.
CREATE TABLE blob_meta (
    hash         BLOB    NOT NULL PRIMARY KEY CHECK (length(hash) = 32),
    size         INTEGER NOT NULL CHECK (size >= 0),
    chunks       INTEGER NOT NULL CHECK (chunks >= 0),
    partial      INTEGER NOT NULL CHECK (partial IN (0, 1)),
    received     BLOB,
    created_at   INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL
) STRICT;

-- Eviction walks the complete blobs from the least recently used one.
CREATE INDEX blob_meta_last_used ON blob_meta (last_used_at) WHERE partial = 0;
