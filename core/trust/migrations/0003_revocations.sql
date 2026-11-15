-- Revocations are the signed statements of Personal devices that another
-- device is no longer trusted. A record is kept for every subject, so the
-- revocation reaches the rest of the group. The record is sealed and bound to
-- the subject.
CREATE TABLE revocations (
    subject BLOB    NOT NULL PRIMARY KEY CHECK (length(subject) = 32),
    issuer  BLOB    NOT NULL CHECK (length(issuer) = 32),
    ts      INTEGER NOT NULL,
    record  BLOB    NOT NULL
) STRICT;
