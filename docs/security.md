# Security

[Русский](security.ru.md) | English

Unison trusts devices, not accounts. Two devices are paired once, they authenticate each other on
every connection, and no service in between is trusted. This page describes how that is done: the
keys, the protocol, how content from another device is handled, what is stored and what is logged.

## Identity

On the first start a device generates two key pairs from 32 random bytes each:

- Ed25519, the classical signature key;
- ML-DSA-65, the post-quantum signature key.

The private seeds are kept in the keystore of the platform and never leave the device. The device ID
is the BLAKE3 hash of both public keys, so a device cannot present one key with the ID of another.

The TLS certificate is self-signed. Its common name is the device ID in canonical form and its
public key is the Ed25519 key. Certificate chains are not verified, because every device signs its
own certificate; the key inside it is the identity, and the trust store decides.

A signature is hybrid: Ed25519 and ML-DSA-65, and both parts have to verify. Each signature is
scoped by a type string, for example `auth`, `introduction`, `revocation` or `peer-policy`. ML-DSA
signs with the context `unison/1 <type>`, and Ed25519 signs `unison/1 <type>`, a zero byte and the
message, so the same bytes cannot be reused under another type.

## Transport

Devices connect over QUIC with TLS 1.3 and mutual certificates. The connection is accepted only when
all of the following hold:

- the TLS version is 1.3;
- the application protocol is `unison/1`;
- the key exchange is X25519MLKEM768. There is no fallback to a classical exchange, and a
  connection that negotiates anything else is closed with code `0x02`;
- the peer presented a certificate and the trust store accepts it.

Session tickets are disabled, so a connection is never resumed. The certificate is checked twice:
during the handshake, and again after it when the identity and the role are collected. The verifier
must fail closed, and the identity it returns has to match the key in the certificate and derive the
same device ID. Unknown, revoked and malformed devices are refused at this point, before any
application data is exchanged.

After the handshake both sides send a signed proof on the first stream:

```
signed:  exporter(32) || role(1) || device_id(32)
frame:   version(1) || role(1) || device_id(32) || signature(3309)
```

The exporter is `ExportKeyingMaterial("unison/1 auth")` of that TLS session, and the signature is
ML-DSA-65. The exporter binds the proof to the session, so a recorded proof cannot be replayed into
another connection. The role in the frame has to equal the role the local trust store returned, and
the device ID has to match the certificate. A failed proof closes the connection with code `0x01`.

The handshake, the certificate check and the proof share a 10 second timeout. An idle connection is
closed after 60 seconds, keepalives go out every 25 seconds, and one peer may hold 32 bidirectional
and 128 unidirectional streams.

## Authorization

What a peer may do is decided by its role and checked for every message and every stream that
arrives, not only when the session opens:

| Action | Personal | Friends |
|--------|----------|---------|
| announce clipboard items | yes | no |
| apply an item to the clipboard | yes, or after confirmation | no |
| exchange the history | yes | no |
| download cached payloads | yes | no |
| offer files | yes, or after confirmation | yes, always asks |
| introduce other devices | yes | no |
| revoke devices | yes | no |
| change the peer policy | yes, if allowed | no |

A Personal device can be moved to the "asks first" column for the clipboard and for files with its
confirmation flags, set per pair and per direction. The flag belongs to the device that asks, and a
change travels there as a signed request. See [Devices and pairing](devices.md#confirmation-flags).
A device accepts such a request only while the permission for the sending device, and the one that
covers every device, is on. The permission of a device that is revoked is not used: a revoked device
is refused before its request is read.

An action that the role forbids is a protocol violation. The message is dropped, the violation is
counted, and the third violation ends the session with code `0x11`. Share decisions and results carry
no capability of their own: the share service authorizes them against the offer it recorded, and a
blob is served only to the peer and the transfer that were offered.

Revocation, introduction and policy changes arrive as signed records and are applied only when the
issuer is a stored, non-revoked Personal device, the signature verifies and the record targets the
local device. A policy is refused when it is not newer than the last one applied from the same
issuer, and a record dated more than 24 hours in the future is refused, so a bad clock cannot block
later records. A revocation is irreversible.

## Pairing

Pairing is the only time a device accepts a connection from an unknown certificate, and only while a
window is open. The window lasts 3 minutes, accepts at most 4 connections per 10 seconds and 8
exchanges at once, and one exchange is bounded by 2 minutes. The limit matters because one
connection is one guess at the short code.

**Short code.** Both devices derive the code from the TLS exporter of the pairing session and from
the two public identities, ordered by device ID:

```
BLAKE3("unison/1 sas" || exporter(32) || identity_1 || identity_2)
```

The result is shown as six digits and four emoji. A device in the middle terminates two separate
sessions, so it produces a different exporter on each side and the users see two different codes. A
message that names another identity than the certificate of its connection is refused before a code
is shown. Nothing is stored until both users confirm the code.

**QR code.** The code carries the device ID, the SHA-256 fingerprint of the pairing certificate, a
one time token, the addresses of the listener and the role to propose. The scanning device accepts
only the pinned fingerprint, so a device in the middle cannot take part. The token is accepted once,
in one window, for one pairing: it is replaced after a completed pairing and given back when the
exchange fails. The code contains the token, so it is never logged or stored.

## Introduction through a trusted device

A paired device can introduce two devices it knows, and the mediator sees every message of the
exchange. It is trusted as far as its own role goes: a Personal mediator is a device of the user, a
Friends mediator is a device of somebody else. Both are treated the same way here, and the protocol
makes a compromised mediator unable to do much more than cancel the exchange.

**Role.** The new pair gets the lower of the two roles the mediator has with the endpoints, and each
endpoint checks that rule against its own record of the mediator. A Friends mediator that offers a
Personal pair is refused by the endpoints even when it lies to both, and a Friends relationship
cannot become Personal through an introduction. A revoked mediator or endpoint is refused before a
prompt is shown.

**Consent.** Both users have to confirm, and no endpoint is stored before the other endpoint signed
the transcript. The mediator cannot answer for an endpoint or make one endpoint believe the other
agreed. Neither endpoint can ask the mediator for its peer list, so an introduction does not become
a way to enumerate the contacts of somebody else.

**Equivocation.** Both offers carry one transcript that names the mediator, both device identities,
the role and the deadline. Each endpoint signs that transcript, extended with its own ephemeral
keys, name and addresses, before it is stored. A mediator that shows one endpoint a different role,
deadline or set of keys than the other one produces a signature that does not verify on the second
endpoint, and the second endpoint writes nothing.

**The rendezvous key.** The endpoints run an X25519 and ML-KEM-768 exchange through the mediator and
derive the rendezvous key from the transcript and both shared secrets. The mediator forwards the
public keys and the ciphertext but sees neither shared secret, so it cannot derive the key, cannot
compute the discovery tags of the pair and cannot join their relay mailbox, now or after the
mediator is revoked. The private ephemeral keys and the shared secrets are wiped when the key is
derived or the exchange ends.

**No group secrets.** The messages have no field for the discovery key, the relay address or the
relay token. A Personal pair receives the group data through the existing sessions with the group,
and a Friends pair never receives it. The relay token is not compared between Friends devices
either, since a hash of a weak token would allow an offline search for it.

**Address hints.** A device signs its own local addresses, so the mediator cannot replace them with
its own. Up to four hints are accepted, each an IP address with a non-zero port; names, IPv6 zones,
unspecified, multicast and loopback addresses are dropped, and loopback is kept only when the
session with the mediator itself runs over loopback, which a development setup uses. A hint is used
only after the user confirmed the exchange, and only through the authenticated transport, never
through the pairing listener.

**Replay.** An exchange identifier is 128 random bits and the deadline is inside the signature. A
device remembers the identifiers it has seen for ten minutes, and an offer whose deadline is more
than thirty seconds beyond the three minute lifetime is refused, so an old offer cannot be brought
back into a new exchange.

**Load.** One device runs at most four mediator exchanges at once, one per pair, keeps at most four
incoming offers, at most two from one mediator, and accepts one offer per mediator in fifteen
seconds with a burst of two. The limits bound the prompts a compromised Friends device can cause and
the memory an exchange may take.

**Revocation.** A revoked device cannot mediate or take part, and revoking a device after an
introduction removes the pair like any other. The mediator does not learn the rendezvous key at any
point, so a later compromise or revocation of the mediator does not open the new connection.

What a compromised mediator can still do: it can refuse to start or forward an exchange, cancel it,
see that two of its peers are being introduced, and read the names and local addresses they send it.
The verification code proves that both endpoints agree on the mediator, the two identities and the
role, while the signed transcripts cover the deadline and the keys. The code does not prove that the
other device is the one the mediator named. Check the name and the short device ID as well, since the
mediator chooses which of its peers to offer.

## Content from another device

Everything that comes from a peer is untrusted input.

### Text

Only `text/plain` and `text/html` are taken. A text representation that is not valid UTF-8 is
dropped. An inline payload is accepted only when its length and its BLAKE3 hash match the
announcement, and a stored payload is verified against the same hash when it is read.

### Images

Only three formats are accepted from a peer, and the format is picked by the file signature, not by
the announced MIME type: PNG, JPEG and GIF. The bytes are then handled in a fixed order:

1. the encoded size is limited to 64 MiB;
2. the header is decoded first with `DecodeConfig`, and the image is rejected before any pixel
   buffer is allocated when a side exceeds 8192 pixels or the total exceeds 56 megapixels;
3. the full image is decoded, with a panic of the decoder turned into an ordinary error;
4. the decoded size must equal the size the header announced;
5. the image is encoded again as PNG by this code, and only those bytes reach the platform.

The last step is the important one: the clipboard and the image decoders of the operating system
only ever see PNG produced locally, never the bytes of the peer. The same path produces thumbnails,
limited to 1024 pixels on the longer side.

Images that this device captures itself are normalized to PNG on the way out. A PNG source is
forwarded as is, because it came from the local system rather than from a peer; other formats are
decoded and re-encoded. An image above 56 megapixels, 8192 pixels on a side or 64 MiB is skipped
instead of being decoded, and the original format is sent as a second representation only when it is
smaller than twice the PNG.

### Files

A received file name is reduced to a base name and stripped of control characters and of invisible
formatting characters that could disguise the extension. Characters that Windows refuses are
replaced, reserved names are prefixed, and the name is cut at 200 bytes. A file is created with mode
`0600` and `O_EXCL`, so it never replaces an existing file; a taken name gets a counter.

Received files are marked as downloaded, so the system warns before it runs one: the
`com.apple.quarantine` attribute on macOS. On Android the app writes only to the folder the user
picked and asks for no storage permission.

Files move through the blob protocol in 1 MiB chunks. A source sends the hash tree of the content
first, and every chunk is verified against the root hash before it is stored, so a source cannot make
the receiver store content it did not ask for. A source that sends a corrupt or unexpected frame is
dropped and its ranges are requested from another source. An interrupted transfer keeps the chunks it
already verified and continues where it stopped.

### Sensitive content

A clipboard that the system marks as sensitive is not read and not sent: the macOS pasteboard types
`org.nspasteboard.ConcealedType`, `org.nspasteboard.TransientType` and
`org.nspasteboard.AutoGeneratedType`, and `EXTRA_IS_SENSITIVE` on Android. This is what most password
managers set.

## Data at rest

The SQLite database and the settings store are sealed column by column with XChaCha20-Poly1305. The
nonce is random and stored in front of the ciphertext, and the additional data binds a value to its
table and row key, so a sealed value cannot be moved to another row.

The 32 byte data encryption key is kept in the keystore of the platform: the login Keychain on macOS,
the Secret Service of the session on Linux, the Android Keystore on Android. It is created on first
use and never leaves the device. A database without its key is refused instead of being opened, and
a key is never created over an existing database.

On Linux the keystore is the Secret Service over D-Bus (GNOME Keyring, KWallet, KeePassXC), with a
key exchange that keeps the secrets off the bus in clear text. The backend is chosen once, when the
first secret is stored, and does not change by itself: a data directory that recorded the Secret
Service refuses to move its keys to files. When the session has no usable Secret Service, for
example an autologin session whose keyring is still locked, the keys are stored as files with `0600`
permissions in `<data directory>/keys`, the directory has mode `0700`, and `unison status` prints
`keystore file` and a warning. That warning means the identity seeds and the data encryption key lie
in the file system of the user session, readable by anything that runs as that user. Unlocking the
keyring of the session and starting from a fresh data directory gives the Secret Service backend.

Blob chunks are sealed the same way and bound to the blob hash and the chunk index, so a chunk of one
blob cannot be opened as a chunk of another. An item up to 64 KiB travels inside its announcement;
larger payloads sit in the local cache and are fetched on demand.

The local API of the daemon is not a network service. On macOS it is a unix socket in a directory
with mode `0700` and the socket has mode `0600`; the CLI and the desktop app refuse an endpoint owned
by another user or reachable by other users. The Android app hosts the node in its own process and
has no socket at all. Access to the daemon therefore equals access to the user session, and anything
in that session can call it.

## Discovery

A Personal device announces itself with a tag derived from the discovery key of the group. The tag
rotates every 10 minutes and the announcement carries no device name, device ID or role, so another
device on the network sees an anonymous service. Friends devices find each other only while the user
turns the search on, and then with one tag per pair, derived from the rendezvous key that only those
two devices know.

## Relay

The relay forwards opaque datagrams. The devices run their usual QUIC connection with mutual TLS and
the post-quantum key exchange inside it, so the relay sees neither keys nor plaintext and writes
nothing to disk.

The mailbox of a pair is `BLAKE3(rendezvous_key || day)[:16]`, with the day counted from the Unix
epoch in UTC. The id changes every day and is unrelated to the previous one, so the relay cannot
follow a pair across days. The server keeps only SHA-256 hashes of the access tokens in memory and
compares them in constant time. Mailboxes are scoped to the token that a connection presented, so one
group cannot join or receive the mailbox of another even when its id is known. A token has a datagram
rate and a daily traffic quota; above them the relay drops datagrams or closes the connection.

## Retention

The history keeps text for 24 hours, images for 7 days and files for 7 days by default. The lifetime
belongs to the device that captured the item and is written into it, so every device expires it at the
same moment and no network is needed for that. Expired entries are removed every ten minutes, and a
deletion travels to the other Personal devices as a tombstone.

## Logs and diagnostics

The daemon logs identifiers, kinds, sizes, hashes and connection events. It never writes the content
of the clipboard, the name or the content of a file, a key, a token or a pairing code. The relay
token is never returned by the API, and the pairing code with its token is never logged or stored.

Crash reports are off unless the user turns them on. A report carries the stack, the release, the
environment and a component tag; user data, request data and breadcrumbs are dropped, and absolute
paths in messages are replaced with `[path]` before a report leaves the device.

## Resource limits

A peer is bounded in every direction:

| Limit | Value |
|-------|-------|
| control message | 1 MiB |
| frame on a request or transfer stream | 2 MiB |
| concurrent request streams | 16 |
| concurrent transfer streams | 32 |
| QUIC streams of a peer | 32 bidirectional, 128 unidirectional |
| protocol violations before the session ends | 3 |
| clipboard announcements | 5 per second, burst of 10 |
| pairing connections per window | 4 per 10 seconds, 8 at once |
| mediated exchanges per mediator | 4, one per pair |
| pending mediated offers | 4, 2 per mediator |
| mediated offers per mediator | 1 per 15 seconds, burst of 2 |
| mediated exchange lifetime | 3 minutes |
| remembered mediated exchange ids | 256 for 10 minutes |
| address hints in a mediated message | 4, 64 bytes each |
| blob range per request | 64 chunks |
| sources of one fetch | 16 |
| hash tree of a blob | 2 MiB |
| item carried inline in an announcement | 64 KiB |
| payload read into memory | 64 MiB |
| blob store | 2 GiB on desktop, 500 MiB on mobile |

A frame length is checked against the limit before memory is allocated for it, a device name and a
capability must be valid UTF-8 within their bounds, and a stream whose header is malformed or whose
payload type is unknown is refused. A panic in a handler ends the session and not the process.

## What this does not protect

- A paired device is trusted. A compromised Personal device can read the history and the clipboard
  items that reach it.
- The clipboard belongs to the user session. Any application of the session can read it, and the
  system may allow that without asking.
- Malware in the session of the user can talk to the daemon through its local endpoint.
- An unlocked device in the hands of somebody else is not protected by any of this.

## The limitation of revocation

A device that is offline when it is revoked keeps its records until it meets another Personal
device: that meeting brings the revocation, and only then the device knows that it is cut off.

Until then it still cannot connect. The devices of the group refuse it during the handshake, so the
only thing the revoked device can do is try. See [Devices and pairing](devices.md#revocation).
