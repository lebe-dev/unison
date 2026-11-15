-- Peers are the devices the local device trusts. A revoked peer stays in the
-- table as a deny-list entry. Public keys and the fields used for filtering are
-- plaintext; the profile and the address cache are sealed and bound to the
-- device ID and column.
CREATE TABLE peers (
    device_id    BLOB    NOT NULL PRIMARY KEY CHECK (length(device_id) = 32),
    ed25519_pub  BLOB    NOT NULL UNIQUE CHECK (length(ed25519_pub) = 32),
    mldsa_pub    BLOB    NOT NULL CHECK (length(mldsa_pub) = 1952),
    role         INTEGER NOT NULL,
    revoked      INTEGER NOT NULL DEFAULT 0 CHECK (revoked IN (0, 1)),
    added_at     INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    profile      BLOB    NOT NULL,
    addrs        BLOB    NOT NULL
) STRICT;

CREATE INDEX peers_role ON peers (role) WHERE revoked = 0;

-- Group secrets are sealed values addressed by name: the discovery key shared
-- by the Personal group and the rendezvous key of each peer.
CREATE TABLE group_secrets (
    name  TEXT NOT NULL PRIMARY KEY,
    value BLOB NOT NULL
) STRICT;
