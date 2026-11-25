# Core

[Русский](core.ru.md) | English

`core/` is a set of Go packages that together are one device. Only `core/node` knows all of them;
a domain package reaches the others through interfaces that the node passes in. `core/ports` holds
the interfaces to the operating system, so everything else builds and runs on any host.

## Packages

| Package | Responsibility |
|---------|----------------|
| `identity` | key pair, device ID, hybrid signatures, the TLS certificate |
| `trust` | peer records, roles, the capability matrix, group secrets |
| `store` | SQLite with encryption at rest, migrations, settings |
| `protocol` | the protocol version shared by the sessions and the relay |
| `session` | the peer protocol over QUIC streams |
| `transport` | QUIC with mutual TLS and the authentication proof |
| `discovery` | mDNS/DNS-SD announcements and browsing |
| `pairing` | the pairing window, the short code and the QR code |
| `mediate` | introductions of two devices through a third one |
| `policy` | role, network and size to an action |
| `clip` | the clipboard pipeline in both directions |
| `tracking` | removal of advertising and campaign parameters from URLs |
| `blob` | content addressed storage with encryption and verification |
| `blob/transfer` | the blob request streams between peers |
| `history` | the append-only log, its version vector and its TTLs |
| `share` | file offers and their transfers |
| `download` | safe names and the mark of a downloaded file |
| `gossip` | spreading trust records and group data over sessions |
| `relay` | the relay client and its virtual packet connection |
| `settingsfile` | the settings document, its versions and its migrations |
| `api` | the local API service, `api/localconn` its endpoint |
| `ports` | the interfaces to the operating system |
| `node` | the composition of all of the above |
| `model` | the domain types, `model/protoconv` maps them to protobuf |

## Node composition

`node.New(node.Config{...})` takes the data directory, the device name and platform, the platform
ports (keystore, clipboard, discovery), an optional clock and logger, the connection limits and a
function that binds packet connections, which is how a test runs a node on the virtual network.

`Start` opens the database with the migrations of the trust, clipboard, blob and history domains,
loads the identity, binds the main listener on the persisted port and a second socket for outgoing
connections, announces the device, opens the blob store with its transfer service and fetcher, reads
the policy and history settings, and starts the clipboard pipeline. `Stop` ends the sessions, the
sockets, discovery and the database.

The node owns:

- the connections: one per paired device, with reconnects, path selection and the address cache;
- the pairing window and the translation of its events;
- the event bus and its subscribers;
- the network state and the decider of `core/policy`;
- the blob store, the fetcher and the list of peers that may serve a blob;
- the history log and its synchronization;
- the clipboard pipeline and the share service.

`AddSessionHook` registers a handler on every new session, which is how the clipboard, the blob
transfer, the share service and gossip attach to the protocol. Handlers must be registered before
the session opens, because the peer may send messages right after its Hello.

`Subscribe` returns a subscriber with an event queue of its own, so a slow reader misses events
instead of holding up the core. The events include `peer-online`, `peer-offline`, `clipboard`,
`clip-download`, `clip-apply`, `peer-incompatible`, `transfer`, `history` and the pairing events.

## Storage

`core/store` opens `<data dir>/unison.db` and keeps its data encryption key in the platform
keystore. Every sensitive value is sealed with that key, the identifiers that are needed for a query
stay readable, and losing the key makes the database unreadable: the store reports `ErrDEKMissing`
instead of starting with fresh data.

| Property | Value |
|----------|-------|
| Driver | `modernc.org/sqlite`, no cgo |
| Write connection | one, transactions serialized |
| Read pool | 8 connections, read-only |
| Busy timeout | 5 s |
| Journal mode | WAL, `synchronous=NORMAL` |
| Data encryption key | 32 random bytes under `store.dek` |
| Migrations | forward-only SQL, one block per domain |

Plaintext columns hold only what is queried: identifiers, origin device, sequence numbers, expiry
times and blob hashes. Every other value is sealed by `Sealer`, which binds the ciphertext to its
table and row key with XChaCha20-Poly1305 and an AAD of `table + row key`, so a value cannot be
moved to another row.

Each domain embeds its own migrations and passes them to `store.Options.Migrations`. The database
records every applied version, so a migration added later still runs on a database that already has
higher versions.

| Domain | Base | Tables |
|--------|------|--------|
| `store` | 0 | `settings` |
| `trust` | 100 | `peers`, `group_secrets`, `introductions`, `revocations` |
| `clip` | 200 | `clip_seq` |
| `blob` | 400 | `blob_meta` |
| `history` | 500 | `history_items`, `history_blobs`, `history_vectors` |

The base keeps the version numbers of the domains apart, so a migration can be added to a lower
block after a database already has higher ones.

Settings are typed values addressed by key. The keys that core reads:

| Key | Meaning |
|-----|---------|
| `device.name` | the name this device announces |
| `transport.main_port` | the persisted listener port |
| `blob.max_size` | the blob store limit in bytes |
| `history.ttl.text` / `.image` / `.files` | the lifetimes of captured items |
| `share.downloads_dir` | the directory that receives shared files |
| `policy.network.<condition>.<kind>.mode` / `.max_size` | one cell of the network policy |
| `policy.network.<condition>.relay` | the relay is allowed on the condition |
| `policy.relay.max_size`, `policy.relay.files_on_request` | relay limits |
| `clip.pause.active`, `clip.pause.until` | the clipboard pause |
| `clipboard.strip_tracking` | remove advertising parameters from the URLs in text |
| `sentry.enabled` | consent to report errors |

## Identity and trust

`core/identity` creates an Ed25519 and an ML-DSA-65 key pair on the first start. The seeds live in
the keystore under `identity/ed25519-seed` and `identity/mldsa65-seed`, and the device ID is the
first 32 bytes of BLAKE3 over both public keys. Hybrid signatures carry both parts and a peer accepts
them only when both verify; signature types separate the domains of different messages.

`core/trust` records which devices are trusted and what each may do. A peer record holds the
identity, the role assigned at pairing, the local confirmation flags, the address cache and the time
the device was last seen. A revoked device stays in the table as a deny-list entry and is never
trusted again. Public keys and the filtering columns are plaintext, the profile and the address
cache are sealed.

The Personal group shares its secrets through `group_secrets`: the discovery key of the group and
the rendezvous key of each pair. Introductions spread a new Personal device to the rest of the
group, so pairing with one device is enough. A completed pairing records the introductions of both
sides, so the device that started the exchange spreads the new device as well, and the node
announces itself again under the discovery key of the group. A revocation is signed by the device
that issues it and spreads the same way. Revoking a Personal device rotates the discovery key, and
the previous key keeps matching the discovery tags for one hour.

A peer keeps at most 8 cached addresses, the group keeps at most 4096 introductions and 4096
revocations, and a signed record whose time is more than 24 hours in the future is refused.

The capability matrix is a pure function of a peer record and lives in `core/trust/action.go`; it is
described in [Protocol](protocol.md#control-messages).

## Introduction through a trusted device

`core/mediate` lets a paired device introduce two devices it knows. The service is both the mediator
and a participant of an exchange, and it registers its message handlers on every session like the
other domains.

The service owns:

- the canonical transcripts of the offer, the commit and the finish, and the hybrid signatures over
  them;
- the ephemeral X25519 and ML-KEM-768 keys, the rendezvous key both endpoints derive and the
  verification code they show;
- the state machine of the mediator, which forwards a commit and a finish unchanged, tracks the two
  endpoints and ends the exchange at its deadline or when a session closes;
- the state machine of a participant, which validates an offer against the local trust store,
  applies the limits and the replay cache, shows the offer to the user and stores the new peer with
  its rendezvous key and address hints in one step;
- the validation of names and address hints, so a peer cannot inject an invisible name, a DNS name,
  a zone, a multicast or an unsuitable loopback address.

The service reads and writes through two narrow interfaces: `Store`, implemented by `*trust.Store`,
and `Sessions`, which hands out the open session of a device. Ephemeral keys and exchanges live in
memory only, and `Stop` wipes them.

`core/node` creates the service in `openMediate`, adds `mediated-pair/1` to the announced
capabilities, and registers `installMediate` as a session hook. The node methods `StartMediatedPair`,
`ConfirmMediatedPair`, `RejectMediatedPair`, `CancelMediatedPair` and `MediatedPairs` are the API of
the service, and `supports_mediated_pair` of a device is true while its session announced the
capability. The events `mediated-pair-offer`, `mediated-pair-updated`, `mediated-pair-completed` and
`mediated-pair-canceled` carry the snapshot of an exchange. When an exchange completes, the node
turns the search for Friends devices on if the user asked for it, syncs the links and, for a
Personal pair, notifies gossip.

## Clipboard pipeline

`core/clip` turns a clipboard change into a clip item and applies the items that arrive.

Capture:

- a change is merged with changes closer than 250 ms and created as one item;
- own writes are ignored for 2 s after they are written;
- content the platform marks as sensitive is dropped before its payload is read;
- text keeps plain text and HTML, and the advertising parameters of its URLs are removed when the
  setting asks for it;
- an image is normalized to PNG, and the original format travels as a second representation while
  it stays below twice the size of the PNG;
- images above 56 megapixels, 8192 pixels on a side or 64 MiB are skipped;
- a payload up to 64 KiB travels inside the announcement, a bigger one goes to the blob store;
- a file is hashed on a background queue and announced once its hash is ready.

Incoming items are decided by `core/policy`:

| Action | Effect |
|--------|--------|
| reject | the item is dropped and not recorded |
| metadata | the item is recorded, the payload is fetched when the user asks |
| confirm | the item is recorded and waits for the user |
| auto | the item is applied at once, up to the size limit of the cell |

A pause silences both directions: nothing is captured or announced, and an incoming item is dropped
without being recorded.

## Policy

`core/policy` maps the role of the peer, the network condition and the size of the item to an
action. `Decide` is a pure function over a configuration; `Decider` binds it to the live trust
store, the network state and the path of the connection, and implements `clip.PolicyDecider`, so
`core/clip` never imports `core/policy`.

| Condition | Text | Image | Files | Relay |
|-----------|------|-------|-------|-------|
| unmetered | auto up to 10 MiB | auto up to 10 MiB | metadata | yes |
| metered | auto up to 1 MiB | auto up to 1 MiB | metadata | yes |
| roaming | auto up to 1 MiB | metadata | metadata | no |
| data saver | auto up to 1 MiB | metadata | metadata | no |

The decision fails closed: an unknown role, kind, source or path is rejected. A Friends device can
never inject a clipboard item. An item that arrives over the relay when the row disallows the relay
is rejected, and files over the relay stay metadata until the user asks for them. The limits are
stored in the `policy.*` settings and a change applies to the next item.

## Blob store

`core/blob` keeps content addressed files. A blob is addressed by the BLAKE3 hash of its whole
content and stored as one MiB chunks.

- the Bao tree of the content is stored next to the chunks, so every chunk is verified against the
  hash tree before it is written;
- every chunk is sealed with the data encryption key of the node and bound to the hash and the chunk
  index;
- a file starts with a fixed header, followed by the tree and the sealed chunks;
- a partial transfer keeps a chunk bitmap in `blob_meta`, so `WriteChunk` with `Complete` resumes
  where it stopped;
- the store is capped by `blob.max_size` (2 GiB on desktop, 500 MiB on mobile by default) and evicts
  the least recently used complete blobs; pinned blobs and partial transfers are never evicted.

`core/blob/transfer` serves the `blob.request` streams: a receiver asks for chunks by index, writes
and verifies them, and keeps a chunk bitmap per blob so a connection that drops does not lose the
work. A fetch carries the size the item or the offer announced as `MaxSize`; a source whose hash
tree describes a larger blob is dropped with `ErrSize` before anything is stored, so a peer cannot
pass the size limits of the policy by announcing a small item.

## History

`core/history` is the log of the device: one row per origin device and origin sequence number, plus
the version vector that outlives the cleanup of expired entries.

- captured and received items are recorded together with the payload description of every blob, so
  the local API can tell a stored payload from one that still has to be downloaded;
- a manual deletion adds a tombstone with the id of the deleted item, and the tombstone travels like
  any other entry;
- expiry is decided by the origin device and carried in the item, so an expired item disappears on
  every device without a network exchange;
- the default lifetimes are seven days for text, three days for images and one day for files, up to
  365 days;
- a cleanup every ten minutes removes expired entries;
- every write reports itself through `history.Options.OnChange`, which the node publishes as the
  `history` event.

When a session with a Personal peer opens, both sides send their version vector and answer with the
entries the peer misses, oldest first, up to 64 entries or 512 KiB per batch and 4096 entries or
16 MiB per exchange. Payloads are not part of the exchange.

## Share

`core/share` sends files the user picked. The sender stores the files in the blob store and offers
them with `ShareOffer`; the receiver answers with `ShareDecision`. The trust matrix decides: a
Personal peer without the files flag is accepted at once, a Personal peer with the flag and a
Friends peer wait for the user, and a refused peer gets a rejection. An offer the matrix accepts at
once then goes through the network policy with the total size of its files and the `share` source:
an automatic apply downloads it, a reject declines it and anything else holds it for the user.
Nothing of an undecided offer is written to the disk.

A transfer pulls its files over the blob protocol with the `share.request` payload type and the
transfer ID of the offer, so an interrupted transfer continues from the chunks that are already
stored. The sender serves those requests only while it tracks the transfer as offered, accepted or
transferring, which the `transfer.ShareAuthorizer` of the share service checks.

| Limit | Value |
|-------|-------|
| Files per transfer | 32 |
| Transfers kept in memory | 64 |
| Pending offers | 16, 8 per peer |
| Pending offer lifetime | 10 minutes |
| File name length | 200 bytes |

Received files are written to `share.downloads_dir` or `~/Downloads/Unison` by default, under a safe
name with a counter when the name is taken. A platform that keeps files elsewhere passes a `Sink`,
which is how Android hands them to the system storage.

## Download

`core/download` prepares an incoming file: a name that is safe on every platform, a new file that
never replaces an existing one, and the mark the platform puts on downloaded files, so the system
warns before it runs one.

## Model

`core/model` holds the domain types shared by the packages: device IDs, roles, platforms, item
kinds, network conditions and the clip item. `core/model/protoconv` maps them to and from the
protobuf messages of `proto/unison/wire/v1`, so a domain never imports generated code for a type it
owns.

## Ports

`core/ports` defines the interfaces through which the core reaches the operating system:

| Interface | Responsibility |
|-----------|----------------|
| `Keystore` | load, store and delete a small secret by name |
| `Clipboard` | watch the local clipboard and replace its content |
| `Discovery` | advertise and browse a service on the local network |
| `Clock` | the time source, so tests control time |

A platform package reports `ErrUnsupported` when its build cannot provide the port, for example a
macOS keystore built without cgo, and the daemon does not start with a port it needs.
`ports.SystemClock` is the real clock and `core/ports/fake` implements the ports for tests.
