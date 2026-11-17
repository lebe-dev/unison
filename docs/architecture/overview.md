# Architecture overview

[Русский](overview.ru.md) | English

Unison is a set of processes that share one Go core:

| Process | Binary | Talks to |
|---------|--------|----------|
| Daemon | `unisond` | peers over QUIC, the desktop app and the CLI over the local API |
| Desktop app | Tauri bundle | the daemon over the local API |
| Android app | APK with the core as an AAR | the core in the same process |
| Relay | `unison-relay` | devices over QUIC and WebSocket, nothing else |

The daemon is the device. It owns the identity, the database, the clipboard pipeline and the
connections to the peers. Everything that a user sees is a client of the daemon, except on Android,
where the core runs inside the app process and the UI calls it directly.

```
desktop app (Tauri + Svelte)   CLI (unison)        Android app (Kotlin)
            \                     /                        |
             +--- local API -----+                         | in process
                        |                                   |
                  +-----------+                    +-------------------+
                  |  unisond  |                    |  core (Go, AAR)   |
                  +-----------+                    +-------------------+
                        |
             QUIC, TLS, mDNS, relay
                        |
                  other devices
```

## Layers

The repository is split by layer, and the dependencies only point downwards:

| Layer | Paths | Role |
|-------|-------|------|
| Entry points | `cmd/`, `mobile/`, `desktop/`, `android/app` | start a process and wire the core |
| Composition | `core/node` | builds the domains into a running device |
| Domains | `core/*` | trust, pairing, discovery, clipboard, blobs, history, share, relay |
| Protocol | `core/session`, `core/transport`, `proto/` | frames, streams, capabilities on the wire |
| Ports | `core/ports` | keystore, clipboard, discovery, clock |
| Platform | `platform/darwin`, `platform/windows` | the ports for a real OS |
| Shared infrastructure | `core/store`, `core/model`, `core/api`, `internal/` | storage, types, local API, daemon helpers |

`core/node` is the only package that composes domains. A domain knows its own store, its own
protocol messages and its own limits, and it reaches the other domains through interfaces that the
node passes in, not by importing them.

## Device identity

The first start creates an Ed25519 and an ML-DSA-65 key pair. The device ID is derived from both
public keys. The seeds live in the platform keystore, the identity signs the session proof and the
group records, and nothing else in the system is a root of trust: there is no account and no
service that a device has to trust.

A paired device is a record with public keys, a name, a platform, a role and the last time it was
reachable. Two roles exist:

- **Personal** - devices of one person, with a shared discovery key, a shared history and a group
  record that spreads introductions and revocations.
- **Friends** - devices of other people, paired one to one, without the group key.

The role is enforced on every message and every stream by the capability matrix of the session, so
a Friends device cannot read the history even when it asks.

## Connectivity

A device listens on a QUIC socket with mutual TLS, a hybrid post-quantum key exchange and ALPN
`unison/1`. Addresses come from mDNS discovery, from the address cache of the peer, from a manual
address and from the relay. Two paired devices keep exactly one connection: a simultaneous dial is
settled by comparing device IDs.

The relay is optional. It forwards opaque datagrams between the members of a mailbox, so the peers
still run their own QUIC connection end to end; the relay sees no keys and no plaintext. The local
network always wins: the relay path is opened only when a peer stays unreachable directly.

## Data

One data directory per device instance holds everything:

| Content | Where |
|---------|-------|
| SQLite database | `<data dir>/unison.db` |
| Blob store | `<data dir>/blobs` |
| Logs | `<data dir>/logs/unisond.log` |
| Lock | `<data dir>/unisond.lock` |
| Local API endpoint | `<data dir>/unisond.sock` (macOS), derived named pipe (Windows) |
| Secrets | platform keystore, never in the directory |

The sensitive values of the database and every blob chunk are sealed with a data encryption key that
lives in the keystore, so a copy of the directory without the key cannot be read. Every domain keeps
its own migrations and its own tables in the single SQLite file.

## Trust boundaries

- The network: everything from a peer is authenticated by TLS and then filtered by the role matrix
  and by the size and rate limits of the session.
- The local endpoint: a unix socket in a directory only the user may enter, or a named pipe with a
  descriptor for the current user. A client refuses an endpoint that another user owns or can
  reach.
- The platform: the keystore and the clipboard belong to the user session; any application in that
  session can reach them.

See [Security](../security.md) for the model in short and
[Local API](local-api.md) for the endpoint rules.

## Further reading

- [Core](core.md) - the domains and how the node composes them
- [Protocol](protocol.md) - transport, session, discovery, pairing and domain messages
- [Local API](local-api.md) - the daemon, its endpoint and the CLI
- [Relay](relay.md) - the relay client, the server and its deployment
- [Desktop app](desktop.md) - the Tauri shell and the Svelte frontend
- [macOS](macos.md) - the darwin ports and the bundle
- [Windows](windows.md) - the windows ports and the installer
- [Android](android.md) - the mobile binding, the service and the app
