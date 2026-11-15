-- Settings are typed values addressed by key. The value column is sealed and
-- bound to the key.
CREATE TABLE settings (
    key        TEXT    NOT NULL PRIMARY KEY,
    value      BLOB    NOT NULL,
    updated_at INTEGER NOT NULL
) STRICT;
