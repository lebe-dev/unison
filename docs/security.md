# Security

## Identity and transport

Every device creates a pair of keys on the first start: Ed25519 and ML-DSA-65, a post-quantum
signature scheme. The device ID is derived from both public keys, and the same keys sign the
messages of the protocol.

The devices talk over QUIC with TLS 1.3, mutual certificates and the hybrid X25519MLKEM768 key
exchange. There is no fallback to a classical key exchange, and a connection that negotiates
anything else is closed. After the handshake both sides send a signed proof that also covers the
keys of that connection, so a recorded session cannot be replayed into another one.

## Trust

Only paired devices are accepted. The certificate of an unknown or revoked device is refused
during the handshake, before anything is exchanged, and the pairing listener exists only while a
pairing window is open.

The role of a device decides what it may do, and it is checked for every message and every stream
that arrives: a Friends device cannot read the history, request cached payloads or send clipboard
items, whatever it asks for. A message that the role forbids is dropped, and repeated violations end
the session.

The size of every message that comes from the network is checked before memory is allocated for it,
the number of streams and requests of a peer is bounded, and clipboard announcements are rate
limited.

## Pairing

The short code is computed from the keys of the connection and from the identities behind its
certificates, so a device between the two produces a different code on each side and the users
notice. A pairing message that names another identity than the certificate of its connection is
refused before a code is shown, and a window accepts only a few connections per ten seconds, so a
device cannot test codes in bulk. The QR code pins the fingerprint of the certificate it belongs to
and carries a token that is accepted once, in one window, for one pairing.

## Data at rest

The database and the cached payloads are encrypted with a key that the daemon keeps in the keystore
of the platform: the login Keychain on macOS, the Android Keystore on Android. The key never leaves
the device, and a copy of the data directory without it cannot be read.

The local endpoint of the daemon is a unix socket in a directory that only the user may enter, with
the socket itself readable by the user alone. The CLI and the desktop app refuse an endpoint that
another user owns or can reach.

## Relay

A relay forwards opaque datagrams. The devices keep their end-to-end connection inside it, so the
relay sees neither keys nor plaintext. The mailbox of a pair is derived from the shared rendezvous
key and the day, so the relay cannot link a device across days, and it keeps only hashes of the
access tokens.

## Logs

The daemon logs identifiers, kinds, sizes, hashes and connection events. It never writes the
content of the clipboard, the name or the content of a file, a key, a token or a pairing code.

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
