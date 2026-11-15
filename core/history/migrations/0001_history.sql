-- History log: one row per origin sequence number. A row with deleted set is a
-- tombstone and carries the id of the deleted item. The payload descriptions
-- are sealed; the columns hold what is queried and ordered.
CREATE TABLE history_items (
    origin     BLOB    NOT NULL CHECK (length(origin) = 32),
    seq        INTEGER NOT NULL CHECK (seq > 0),
    id         BLOB    NOT NULL CHECK (length(id) = 16),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    kind       INTEGER NOT NULL,
    source     INTEGER NOT NULL,
    deleted    INTEGER NOT NULL CHECK (deleted IN (0, 1)),
    payload    BLOB    NOT NULL,
    PRIMARY KEY (origin, seq)
) STRICT;

-- Items are read newest first, by device and by kind.
CREATE INDEX history_items_created ON history_items (created_at DESC, seq DESC);
CREATE INDEX history_items_origin ON history_items (origin, created_at DESC);
CREATE INDEX history_items_kind ON history_items (kind, created_at DESC);

-- Expiry walks the entries that are due.
CREATE INDEX history_items_expires ON history_items (expires_at);

-- Tombstones are looked up by the id of the item they hide.
CREATE INDEX history_items_tombstones ON history_items (id) WHERE deleted = 1;

-- Payload references: one row per blob hash with the number of entries that
-- point to it and the cached local state of the payload.
CREATE TABLE history_blobs (
    hash    BLOB    NOT NULL PRIMARY KEY CHECK (length(hash) = 32),
    size    INTEGER NOT NULL CHECK (size >= 0),
    refs    INTEGER NOT NULL CHECK (refs >= 0),
    present INTEGER NOT NULL CHECK (present IN (0, 1))
) STRICT;

-- Version vector: the highest sequence number seen per origin device. It only
-- grows, so entries removed after their expiry stay accounted for.
CREATE TABLE history_vectors (
    origin BLOB    NOT NULL PRIMARY KEY CHECK (length(origin) = 32),
    seq    INTEGER NOT NULL CHECK (seq > 0)
) STRICT;
