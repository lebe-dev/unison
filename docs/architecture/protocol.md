# Protocol

[Русский](protocol.ru.md) | English

Two devices speak three layers below the clipboard and the files: QUIC, the transport authentication
and the session. Everything above them is a protobuf message defined in `proto/unison/wire/v1`.

```
clip, history, blob, share        domain handlers
session                            control stream, request and transfer streams
transport                          QUIC, TLS 1.3, certificates, auth proof
QUIC / UDP (+ relay datagrams)
```

## Transport

`core/transport` runs peer connections over QUIC. Both sides present the self-signed certificate of
their device identity; the certificate carries the Ed25519 and ML-DSA-65 public keys and the device
ID derived from them.

| Property | Value |
|----------|-------|
| TLS | 1.3, mutual certificates, no session tickets |
| Key exchange | X25519MLKEM768 only, no classical fallback |
| ALPN, peers | `unison/1` |
| ALPN, pairing | `unison-pair/1` |
| Keepalive | 15 s |
| Idle timeout | 60 s |
| Handshake timeout | 10 s |
| Incoming streams | 32 bidirectional, 128 unidirectional |
| Packet size | 1200 to 1452 bytes |

Both certificates are self-signed and carry the device ID as their common name. The client does not
verify a CA chain: `PeerVerifier` makes the trust decision, twice. It runs during the handshake to
refuse an unknown or revoked device, and after it to collect the identity and the role; both calls
have to accept, and an implementation fails closed. A connection whose handshake did not use the
hybrid key exchange is closed with `CodeInsecure` (`0x02`).

The first stream of a peer connection carries the authentication proof. Both sides sign
`EKM(32) || role(1) || device_id(32)` with the ML-DSA-65 key, where the EKM comes from the TLS
exporter label `unison/1 auth`. The frame is fixed at 3343 bytes, so it binds the identity key to
the exporter secret, the role and the device ID and a recorded session cannot be replayed into
another connection. Each side checks the role of the peer against its own trust record. A proof that
does not verify closes the connection with `CodeAuthFailed` (`0x01`).

The pairing listener is a separate listener with the pairing ALPN. It accepts unknown certificates
and leaves the decision to a `PairingPolicy`: `AcceptPairingPeer` for the short code flow, or
`PinCertificate(fingerprint)` for the QR flow. The listener exists only while a window is open.

## Session

`core/session` runs the peer protocol on an authenticated connection.

A session starts with `Hello` on the control stream, which is the stream that already carried the
authentication proof. Both sides announce:

- the protocol version, currently `1`;
- the device name, at most 128 bytes;
- the platform;
- the local clipboard policy;
- up to 32 capability names of at most 64 bytes each.

A peer with another version gets the `peer-needs-update` event and the connection is closed with
code `0x10`; a malformed Hello or another protocol violation closes it with `0x11`, and a handler
that panics ends the session with `0x12`, never the process.

### Framing

Every message is a uvarint payload length followed by a protobuf payload. The length is checked
against the limit before the payload is allocated.

| Frame | Limit |
|-------|-------|
| Control message | 1 MiB |
| Stream frame | 2 MiB |

### Control messages

The control stream is bidirectional and carries `ControlMessage` envelopes. The payload variant
selects the handler, and the capability of the peer must allow the action before the handler runs.

| Kind | Capability | Purpose |
|------|------------|---------|
| `hello` | - | protocol version, name, platform, policy |
| `clip-announce` | `receive-clip-announce` | a clipboard item |
| `history-sync` | `sync-history` | the version vector of the history |
| `history-batch` | `sync-history` | history entries the peer misses |
| `have-blob` | `fetch-blob` | asks whether a blob is stored |
| `have-blob-resp` | `fetch-blob` | answers the blob query |
| `share-offer` | authorized by the share service | offers files |
| `share-decision` | none | accepts or rejects an offer |
| `share-cancel` | none | stops a transfer |
| `share-result` | none | reports the outcome to the sender |
| `introduction` | `introduce` | introduces another Personal device |
| `revocation` | `revoke` | revokes a device of the group |
| `gossip-digest` | `introduce` | the trust state of a device |
| `gossip-records` | `introduce` | the trust data a peer lacks |
| `set-peer-policy` | `set-remote-policy` | asks a device to change a confirmation flag |
| `peer-policy` | `set-remote-policy` | reports the confirmation flags of the sender |

The authentication proof is not a control message: it is a fixed frame that the transport reads
before the session starts, and the `auth-proof` kind exists only to reserve the slot.

The capability matrix lives in `core/trust`. A role decides each action:

| Action | Personal | Personal with flag | Friends |
|--------|----------|--------------------|---------|
| receive-clip-announce | allow | allow | deny |
| apply-clipboard | allow | confirm | deny |
| sync-history | allow | allow | deny |
| fetch-blob | allow | allow | deny |
| receive-share | allow | confirm | confirm |
| receive-files | allow | confirm | confirm |
| introduce, revoke, set-remote-policy | allow | allow | deny |

A message that the matrix denies is dropped and counted as a violation. Three violations end the
session. Clip announcements are also rate limited to 5 per second with a burst of 10.

### Streams

Request streams are bidirectional and carry one request with its answer; transfer streams are
unidirectional and carry one transfer. The first frame is a `StreamHeader`:

| Field | Meaning | Limit |
|-------|---------|-------|
| `kind` | request or transfer | required |
| `payload_type` | selects the registered handler | 64 bytes |
| `transfer_id` | groups the streams of one transfer | 128 bytes |

| Payload type | Kind | Handler |
|--------------|------|---------|
| `blob.request` | request | blob transfer service |
| `share.request` | request | share service |

At most 16 request and 32 transfer streams are served at once. A stream header has to arrive within
10 s. A stream releases its slot of the connection only when both directions are closed, so the
session drops the read side of a request once its handler is done.

## Connections

A node keeps exactly one connection per paired device. A simultaneous dial is settled by comparing
device IDs: both sides keep the connection that the device with the lower ID initiated, and that
device dials again when it is online on a connection the other side initiated.

Addresses come from four places, in this order of preference:

1. a manual address stored with the device;
2. the candidates of mDNS discovery;
3. the address cache of the peer;
4. the relay.

The address a peer connected from is the ephemeral port of its dialer, which takes no connections,
so it is cached last and only for a host the cache does not know yet. A dialed address replaces the
other ports of its host, which belong to an earlier run. The main listener port is persisted in
`transport.main_port`, so the cached address of a device survives a restart. A connection attempt
stops as soon as the peer connects on its own. After a lost connection the node reconnects with a
delay that doubles from one second up to thirty seconds. `NetworkChanged` announces the device again
and retries every offline peer.

## Discovery

`core/discovery` speaks DNS-SD on the local network, without a library, over its own mDNS sockets.

| Service | Type | When |
|---------|------|------|
| main | `_unison._udp.local.` | while the node runs |
| pairing | `_unison-pair._udp.local.` | while a pairing window is open |
| friends | `_unison-friends._udp.local.` | while the Friends search runs |

The announcement carries no name, device ID or role: the TXT record holds `v=1` and, for a service
that has keys, one or more tags. The tags are what a device matches against the keys it knows:

- the main service carries the tag of the Personal group discovery key;
- the friends service carries one tag per Friends pair, derived from the rendezvous key of the pair;
- the pairing service carries no tag, because a device that is open for pairing is not yet trusted.

A tag is an HMAC-SHA-256 of the ten minute epoch, truncated to 8 bytes and written as 13 lowercase
base32 characters. A device accepts the epochs of the previous, the current and the next ten
minutes, so clock skew does not matter and a rotation never breaks discovery for long. A rotation
of the group key accepts the previous key for a transition period.

The service instance name, host name and the query are ordinary DNS-SD; PTR and TXT records live
4500 s, SRV and address records 120 s. Announcements are sent twice, one second apart, and the
epoch and the interface set are re-checked every 30 s. A browser repeats its query one second after
the first one and doubles the pause up to one minute, as RFC 6762 asks, because every query wakes
the phones of the network; a change of the network or of the epoch starts the schedule again.

`LocalNetworkBlocked` reports a process that the system refuses to send on the local network while
loopback still works, which is what macOS does without the local network permission.

## Pairing

`core/pairing` implements the window, the short code and the QR code.

| Limit | Value |
|-------|-------|
| Window | 3 minutes, extendable |
| Exchange timeout | 2 minutes |
| Pending exchanges | 8 |
| Connections per window | 4 per 10 seconds |

The short authentication string is computed from the TLS exporter (`unison/1 sas`, 32 bytes) and
the identities of both certificates: six decimal digits and four emoji from a fixed table of 64.
A device between the two gets different codes on the two screens, and a pairing message that names
another identity than the certificate of its connection is refused before a code is shown.

The QR code carries the device ID, the certificate fingerprint, a random one time token, the
addresses of the pairing listener and the role to propose. The scanning device pins the fingerprint,
so it refuses any other certificate, and sends the token with a request signed by its identity keys.
The token belongs to one window, is accepted once and is replaced after a completed pairing.

A pairing message is a uvarint length and a protobuf payload of at most 64 KiB. The exchange is
`PairRequest`, `PairResponse`, `PairDecision` and `PairAccept`; the last one carries the rendezvous
key of the pair and, for a Personal peer, the discovery key and up to 64 introductions. No group
data is sent before both sides accept.

The URI of a code is `unison://pair` with the parameters `v`, `id`, `fp`, `t`, `a` and `role`. It is
at most 512 bytes and carries at most four addresses, each an IP address with a port, so the
scanning device connects to the address instead of searching the network.

The result of both flows is a trust record: public keys, name, platform, role and the time the
device was last reachable. A pairing window is also the only time the pairing listener exists.

## Domain exchanges

### Clipboard

`clip-announce` carries the item metadata and its representations when the payload is small enough
to travel inline. An item above 64 KiB carries hashes only and the receiver fetches the payload from
the blob protocol. An announcement from a device that Unison wrote itself is ignored on the origin.

### History

When a session with a Personal peer opens, both sides send `history-sync` with the version vector of
their history. `history-batch` answers with the entries the peer misses, oldest first, up to 64
entries or 512 KiB per batch and 4096 entries and 16 MiB per exchange, so a large gap is filled over
several exchanges. Payloads are not part of the exchange: they are fetched as blobs.

### Blobs

`have-blob` asks whether a peer stores a blob, `have-blob-resp` answers it with the chunk count. The
payload itself is pulled with `blob.request` streams on the request payload type:

1. the receiver asks for a range of at most 64 chunks, optionally for the tree alone;
2. the answer starts with the Bao tree of the blob, at most 2 MiB;
3. the chunks follow in ascending index order;
4. every chunk is verified against the tree before it is written.

A fetch uses at most 16 sources and 2 concurrent requests per peer, and drops a source that sends a
corrupt, out of order or unexpected frame. The same mechanism serves an accepted share with the
`share.request` payload type and the transfer ID of the offer.

### Share

The sender offers files with `share-offer`. The trust matrix decides the answer: a Personal peer
without the files flag is accepted at once, a Personal peer with the flag and a Friends peer wait
for the user, and a peer the matrix refuses gets a rejection. `share-decision` carries the answer,
`share-cancel` stops a transfer from either side and `share-result` reports the outcome back to the
sender.

### Trust

`introduction` introduces another Personal device, so pairing with one device of the group is
enough. `revocation` carries a signed revocation, which any Personal device may issue for any
device of the group. `gossip-digest` and `gossip-records` bring a device that was away up to date
with the trust state of the group: a digest carries only the heads of the records, a reply carries
at most 16 records per message and 512 per digest, and a session answers at most 8 digests.
`set-peer-policy` changes the confirmation flags of the peer for the local device, and
`peer-policy` reports the flags of the sender after a change and when a session opens.

Only Personal sessions take part in gossip and history synchronization. A record is applied only
when its issuer is a stored, non-revoked Personal device and its signature and timestamp verify, so
an introduction or a revocation cannot be forged by a Friends device.

## Limits

| Limit | Value |
|-------|-------|
| Control message | 1 MiB |
| Stream frame | 2 MiB |
| Request streams in flight | 16 |
| Transfer streams in flight | 32 |
| Protocol violations | 3 |
| Clip announcements | 5 per second, burst 10 |
| Stream header timeout | 10 s |
| Hello timeout | 10 s |
| Session events buffered | 16 |
