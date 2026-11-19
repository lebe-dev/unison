# FAQ

[Русский](faq.ru.md) | English

Short answers to the questions people ask most often, mostly about security. The full detail is on
the [Security](security.md) page, and the roles are explained on the
[Devices and pairing](devices.md) page.

## Trust and connections

### Is there an account or a central server I have to trust?

No. A device is trusted only because it was paired with another device, and the keys of a pair stay
on those two devices. There is no account and no cloud. In one local network the devices talk
directly. For devices in different networks you can run your own relay, and even then it only
forwards encrypted datagrams. The app works without internet access while the devices are in one
network.

### Who can connect to my device?

Only a device you have paired with, or the one you are pairing right now in an open window. Every
other connection is refused during the TLS handshake, before a single application message is
exchanged. The trust store decides, not a certificate authority.

### Can someone on the same Wi-Fi connect without pairing?

No. Discovery is anonymous: an announcement carries a rotating tag and no device name, device ID or
role, so another device can see that some Unison service exists but not whose it is. To pair,
someone has to confirm a code on your screen or scan a QR code with a one-time token. After the
window closes, no pairing listener exists at all. Without pairing, the handshake fails.

## Data on the wire

### Is traffic encrypted in the local network too?

Yes. Every connection, local or through the relay, is QUIC with TLS 1.3 and certificates on both
sides, so a cafe Wi-Fi gets the same protection as any other path. Someone who captures the traffic
sees encrypted packets, their timing and their size, but not the clipboard, the files or the device
identities.

### Does the relay see my data in plain text?

No. The relay forwards opaque datagrams, and the devices run their usual QUIC connection with mutual
TLS and the post-quantum key exchange inside them. The relay terminates only its own outer TLS, and
what it forwards stays the ciphertext of the pair. It writes no payload to disk either. A
compromised relay can drop or delay datagrams, but it cannot decrypt them or take part in a session
without a paired identity.

### What does the relay operator see?

The client addresses, the TLS server name and timing, the access token a connection presents, the
mailbox ids it joins, and the size and rate of the datagrams. The operator can tell that two
connections share a mailbox on one day and that devices share a token. It cannot see the payload,
the keys or the device identities, and the mailbox id changes every day, so a pair cannot be
followed across days.

### Can someone impersonate a paired device or replay a recorded session?

A device ID is the BLAKE3 hash of both public keys of the device, so one key cannot be presented
under the ID of another. Both sides check the certificate during the handshake and again after it,
and a connection is accepted only with TLS 1.3, the `unison/1` protocol and the hybrid key exchange.
After the handshake each side signs the TLS exporter together with its role and device ID, and that
signature is checked against the stored device. The exporter belongs to one session, so a recorded
proof cannot be replayed into another connection. Session resumption is disabled.

### Is the protocol protected against quantum computers?

The key exchange is X25519MLKEM768 and there is no fallback to a classical one: a connection that
negotiates anything else is closed. Device identities are signed with Ed25519 and ML-DSA-65, and the
post-quantum part has to verify as well. The protocol assumes that traffic recorded today can be
stored and decrypted later, so the post-quantum part is mandatory rather than optional.

## Pairing

### How do I know I am pairing with the right device?

Both screens show the same six digits and four emoji, computed from the TLS exporter of the pairing
session and from the identities of both certificates. A device in the middle terminates two separate
sessions and therefore produces a different code on each side, so the screens do not match. With a
QR code the scanning device accepts only the certificate fingerprint from the code. Nothing is
stored until both users confirm.

### Can the pairing code be guessed?

One connection is one guess at the code. The window lasts three minutes, accepts at most four
connections per ten seconds and eight exchanges at once, so a brute-force attempt gets a few dozen
tries and then the window is gone. Besides the six digits the code carries four emoji from a table
of 64, so the space is far larger than a million. A failed guess stores nothing.

### What can a device of another person do?

A Friends device is paired one to one and never learns the group key. It cannot read the clipboard,
the history or the cached payloads; its only action is to offer files, and every offer waits for
your decision. Its role is fixed: to change it, revoke the device and pair it again.

## Content from other devices

### Can a malicious device exploit the images or files it sends?

Everything that comes from a peer is treated as untrusted input. Images are limited in size and
pixels, decoded with bounds and re-encoded as PNG locally, so the clipboard and the image decoders
of the system never see the bytes of the peer. A file name is reduced to a base name, stripped of
control and invisible characters and cut to a length, and a file is created with mode `0600` and
`O_EXCL`, so it never replaces an existing one. On macOS the received file is marked as downloaded,
so the system warns before it runs it. Files arrive in chunks, and every chunk is checked against
the hash tree of the content before it is stored.

### Are passwords copied from a password manager?

Content that the system marks as sensitive is not read and not sent. This covers the macOS
pasteboard types that most password managers set and the sensitive flag on Android. If an
application copies a secret without such a mark, Unison cannot tell it from ordinary text and sends
it as usual. Pause the clipboard when you work with secrets that are not marked.

### Is the history stored in the cloud?

No. The history is a local SQLite database on each device, shared only with the paired Personal
devices and kept for a fixed time: 7 days for text, 3 days for images and 24 hours for files by
default. The relay stores no content and writes nothing to disk. Payloads are cached locally and
sealed.

## The device and its data

### What if a device is lost or stolen?

Revoke it from another Personal device. A revocation is irreversible: every connection to the device
is closed, the transport refuses it during the handshake, its address cache and relay key are
dropped, and it cannot be paired again. The group also rotates its discovery key, so the revoked
device no longer matches its announcement. A device that was offline learns about the revocation
when it meets another Personal device, but it cannot connect in the meantime.

This does not help when the device is unlocked and in somebody else's hands: a paired device is
trusted, and none of this protects an unlocked device.

### Is the data on disk encrypted?

Yes. The database, the settings and the cached payloads are sealed with XChaCha20-Poly1305, and the
32 byte key lives in the system keystore: the macOS Keychain or the Android Keystore. It never
leaves the device, and a database without its key is refused instead of being opened.

### What goes into the logs and what is sent to the developers?

The daemon logs identifiers, kinds, sizes, hashes and connection events. It never writes the content
of the clipboard, the name or content of a file, a key, a token or a pairing code. Crash reports are
off unless you turn them on; a report carries the stack, the release, the environment and a
component tag, while user data, request data and breadcrumbs are dropped and absolute paths are
replaced.

### Does Unison protect me from malware on my own device?

No, and no clipboard tool can. The clipboard belongs to the user session, so any application of the
session can read it, and on macOS anything in the session can call the daemon through its local
socket. On Android the node runs inside the app and has no socket, but the clipboard is still
readable by the session. Unison protects the path between devices, not a device that is already
compromised.
