# Daemon and local API

[Русский](local-api.ru.md) | English

The daemon is the process that owns a device. It starts the core node, serves the local API on the
endpoint of its data directory, and keeps running while the user works. The desktop app and the
command line client are its clients; on Android the core runs in the app process and there is no
daemon.

## Process model

```
cmd/unisond
  internal/daemon.Run        data directory, lock, log, node lifecycle, endpoint
    core/node                the device
    core/api + localconn     the local API
  signal: SIGINT, SIGTERM    stop the API, stop the node, remove the endpoint
```

`internal/daemon` prepares the data directory, takes the single instance lock, opens the log file,
bootstraps the keystore and the device name, creates the node with the platform ports, serves the
API and shuts everything down in reverse order.

One instance owns a data directory. The daemon takes an exclusive lock on `unisond.lock` and also
checks whether an instance answers on the endpoint; a second one reports that another instance is
running and exits with code 1. The system releases the lock when the process ends, so a crash leaves
no lock behind, and an endpoint file that nobody serves is removed at startup.

## Command

```
unisond [flags]

  -version            print the version and exit
  -data-dir path      data directory of this instance
  -endpoint path      local API endpoint of this instance
  -clipboard mode     system or none
  -name name          device name announced to peers
  -log-level level    debug, info, warn or error
  -log-stderr         copy the log to stderr
```

`--clipboard=none` starts a daemon without the system clipboard, which is what a second daemon on
one machine needs: two daemons would otherwise read and write the same pasteboard. The default data
directory keeps its keys in the `app.unison` Keychain service and any other data directory in a
service named after the directory, so two instances on one machine are two devices and can pair.

`--name` sets the device name announced to peers; the name stored through the local API is used when
the flag is absent, and without either the daemon uses the host name without the `.local` suffix.

## Data directory

| Path | Content |
|------|---------|
| `unison.db` | SQLite database, encrypted |
| `blobs/` | blob store |
| `logs/unisond.log` | JSON log, five MiB per file, three rotated files kept |
| `unisond.lock` | single instance lock |
| `unisond.sock` | local API endpoint on platforms with unix sockets |
| `keys/` | keystore files on Windows |

The default directory is `~/Library/Application Support/Unison` on macOS and
`%LOCALAPPDATA%\Unison` on Windows. `--data-dir` gives an instance a directory of its own, and the
endpoint follows the directory: `unisond.sock` inside it on macOS, a named pipe derived from the path
on Windows.

The log names a clipboard item by its identifier, kind, size and MIME types, never by its content,
and it never carries keys or tokens. A peer that comes online is logged with the negotiated TLS
version and key exchange, so a connection that falls back to a weak key exchange shows up.

## Endpoint

`core/api/localconn` serves the API on the endpoint. The endpoint speaks HTTP/2 in clear text, so no
certificate is involved and `http://unison` is only the base URL of the generated clients.

| Platform | Endpoint |
|----------|----------|
| macOS, Linux | unix socket `<data dir>/unisond.sock` |
| Windows, default directory | named pipe `\\.\pipe\unison-<user-sid>` |
| Windows, own directory | named pipe `\\.\pipe\unisond-<hash>` |

`UNISON_LOCAL_API` overrides the endpoint, so a second daemon can run next to the installed one.

On a unix socket the directory is created with mode 0700 and the socket with 0600. A client refuses
a socket that belongs to another user or that other users can reach, and also refuses a directory
that other users can write to, so the endpoint cannot be replaced. The descriptor of a named pipe
grants access to the current user only.

| Limit | Value |
|-------|-------|
| Connections served at once | 16 |
| Idle timeout | 5 minutes |
| Request head | 64 KiB |
| Request and response body | 1 MiB |

## Service

`core/api` implements `UnisonService` of `proto/unison/local/v1`. The service takes the node, the
daemon version, the device name and an optional `LocalClip`; without it `SetClip` reports that the
call is not implemented, and the daemon passes the clipboard pipeline of the node.

| Group | Methods |
|-------|---------|
| Status | `GetStatus`, `WatchEvents` |
| Devices | `ListDevices`, `RenameDevice`, `RevokeDevice`, `SetDeviceConfirmation`, `SetPeerAddress` |
| Pairing | `OpenPairingWindow`, `ExtendPairingWindow`, `ClosePairingWindow`, `ListPairingCandidates`, `StartSas`, `ConfirmSas`, `RejectSas` |
| Pairing by QR | `GetPairingQr`, `ScanPairingQr` |
| Clipboard | `GetClip`, `SetClip` |
| History | `ListHistory`, `GetHistoryItem`, `GetHistoryThumbnail`, `DeleteHistoryItem`, `FetchHistoryItem`, `CopyHistoryItem`, `ApplyHistoryItem` |
| Transfers | `SendFiles`, `ListTransfers`, `CancelTransfer`, `AcceptTransfer`, `RejectTransfer` |
| Requests | `SetFriendsDiscovery`, `ListRequests`, `DecideRequest` |
| Pause | `GetPause`, `SetPause` |
| Settings | `GetSettings`, `UpdateSettings` |
| Relay | `GetRelay`, `SetRelay`, `RelayOff` |

`WatchEvents` carries the events of the node bus: a peer that comes online or goes offline, a
clipboard item, a peer that has to be updated and the pairing events with the address of a candidate
and the short code. The response of a server stream arrives with the first event, so a client opens
the stream before it needs the events.

Device IDs are lowercase hex and times are unix milliseconds. `GetClip` returns the latest clipboard
item. The settings hold the device name, the confirmation flags of the local device, the blob store
limit, the downloads directory, the lifetimes and the network policy; a change applies at once. The
relay settings are reported as a URL, a change time and whether a token is stored, never as the
token itself.

The desktop app does not use a socket directly: its Rust shell proxies the same API from a webview
over Tauri IPC to the endpoint. See [Desktop app](desktop.md#local-api-proxy).

## Errors

The errors of the core are mapped to connect codes:

| Condition | Code |
|-----------|------|
| missing peer, item or transfer | `not_found` |
| closed window, stopped node, offer that is not pending | `failed_precondition` |
| already paired device | `already_exists` |
| refused device, revoked peer, bad token | `permission_denied` |
| invalid input, malformed request | `invalid_argument` |
| too many pairing exchanges | `resource_exhausted` |
| offline peer, no source for a payload | `unavailable` |
| unsupported operation of the platform | `unimplemented` |
| anything unexpected | `internal` |

An internal failure is reported to the error reporter, a client error is not. The message of the
cause is kept; it never carries clipboard content or secrets.

## Command line client

`cmd/unison` speaks the same API, so the core can be driven without the desktop app. It lands in
`bin/unison` and reaches the daemon of the app without any option, because both use the same
endpoint.

```
usage: unison [--endpoint path] [--json] <command> [arguments]
```

| Command | Purpose |
|---------|---------|
| `status` | daemon, network, address and peers |
| `devices` | list, address, rename, confirm and revoke |
| `pair` | open a window, list open devices, confirm the code, or join with `--qr` |
| `clip` | `get` prints the last item, `set` copies standard input to the clipboard |
| `history` | list, fetch and copy recorded items |
| `send` | offer a file or standard input to the devices of `--to` |
| `pause` | pause or resume the clipboard synchronization |
| `settings` | print and change the settings, including the lifetimes |
| `policy` | print the network policy |
| `relay` | print, set and turn off the relay access |
| `friends` | start and stop the search for Friends devices |
| `requests` | list the offers that wait for a decision |
| `transfers` | list transfers and cancel one |

A device is addressed by a unique prefix of its fingerprint, a prefix of its device ID or its name.
Output is plain text by default and the JSON form of the API messages with `--json`. The command
exits with 0 on success, 1 on a failure and 2 when it cannot reach the daemon.

## Error reporting

`internal/observability` wraps Sentry. The DSN and the environment are set at build time; reporting
stays off until the user grants consent, which is stored in the `sentry.enabled` setting. Events
carry only the stack, the release, the environment and the component tag: extras, breadcrumbs and
request data are dropped, and absolute paths are replaced with `[path]` before an event leaves the
process.
