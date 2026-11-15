-- Introductions are the signed statements of Personal devices about the other
-- devices of the group. The subject, the issuer and the signing time are
-- plaintext for the digest; the record itself is sealed and bound to the
-- subject.
CREATE TABLE introductions (
    subject BLOB    NOT NULL PRIMARY KEY CHECK (length(subject) = 32),
    issuer  BLOB    NOT NULL CHECK (length(issuer) = 32),
    ts      INTEGER NOT NULL,
    record  BLOB    NOT NULL
) STRICT;
