-- clip_seq holds the last sequence number allocated for each origin device.
-- Numbers are monotonic per origin and survive restarts.
CREATE TABLE clip_seq (
    origin BLOB    NOT NULL PRIMARY KEY CHECK (length(origin) = 32),
    seq    INTEGER NOT NULL CHECK (seq > 0)
) STRICT;
