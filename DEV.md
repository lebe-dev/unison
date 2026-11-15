# Development

Everything runs through `just`. Install the toolchains below, then `just` alone lists the
available recipes.

## Toolchains

| Tool | Version | Used for |
|------|---------|----------|
| Go | 1.27 | daemon, CLI, relay, core |
| just | 1.59 | entry point for all tasks |
| golangci-lint | 2.x | linting and formatting |
| buf | 1.x | protobuf generation |
| Node.js | current LTS | Tauri and Svelte frontend |
| Rust | stable | Tauri |
| JDK | 21 | Android builds |
| Android SDK | API 29 min, current target | Android builds |
| Android NDK | 28.2.13676358 | gomobile |
| gomobile | golang.org/x/mobile (latest) | Android core binding |
| Xcode | current | macOS builds and signing |

Target platforms: macOS 13 Ventura and newer, Windows 10 22H2 / 11, Android 10 (API 29) and newer.

## Install

Go, the task runner and the linters:

```sh
brew install go just golangci-lint buf
```

Node.js and Rust for the Tauri frontend:

```sh
brew install node
rustup toolchain install stable
```

The Tauri CLI comes from the desktop package, install the frontend dependencies once:

```sh
cd desktop && yarn install
```

Android builds need a JDK and the Android SDK:

```sh
brew install --cask temurin@21 android-studio
```

macOS builds need the command line tools. The Android app embeds the Go core as an AAR, which
`gomobile` builds, so it needs the NDK as well:

```sh
xcode-select --install
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
sdkmanager "ndk;28.2.13676358"
```

`gomobile` is pinned as a tool in `go.mod` and uses the NDK from `ANDROID_HOME/ndk`, or
`ANDROID_NDK_HOME` when set.

## Recipes

```sh
just build                # host binaries into bin/
just test                 # unit tests
just test-keychain        # Keychain integration tests against the login Keychain
just test-pasteboard      # pasteboard integration tests against the real pasteboard
just test-clipboard       # Win32 clipboard integration tests, run on Windows
just test-blob-large      # large blob round trip, writes about 1.2 GiB
just lint                 # linters and format check
just vet-windows          # vet and build everything for Windows from any host
just fmt                  # format the Go sources
just tidy                 # update go.mod and go.sum
just proto                # generate Go code from proto/ with buf
just proto-android        # generate Java lite classes for the Android app
just proto-lint           # lint the protobuf definitions
just check                # lint, proto lint and tests, run before committing
just aar                  # bind the Go core into android/core/core.aar
just android-build        # build the Android debug APK
just build-unisond-all    # unisond for darwin/arm64, darwin/amd64, windows/amd64
just build-unisond-host   # unisond for the host with the Keychain and pasteboard
just run-daemon           # run the daemon of the default data directory
just run-daemon-2         # run a second daemon, for pairing on one machine
just desktop-dev          # run the desktop app with hot reload
just desktop-build        # build the desktop app bundle for the host platform
just desktop-lint         # svelte-check, rustfmt and clippy for the desktop app
just package-macos        # macOS bundle with the daemon, signed and notarized
just verify-macos         # check the layout and the signature of the macOS bundle
just clean                # remove bin/
```

Binaries are built without cgo so every target can be cross-compiled from any host.

The daemon is the exception on macOS. The Keychain and the pasteboard are cgo packages, so a
macOS daemon that is meant to run needs cgo: `just build-unisond-host` builds it for the host with
`CGO_ENABLED=1`, and `just run-daemon` and `just run-daemon-2` run it from the source tree. A
daemon of the cross-compiled `just build-unisond-all` targets has no Keychain and exits at startup
with a message that asks for a build with cgo.

The macOS Keychain keystore in `platform/darwin/keystore` uses cgo: it links Security.framework
and needs a host build to reach the Keychain. Without cgo, for example in a cross-compiled
binary, its operations return `keystore.ErrUnsupported` and building still works. The Keychain
integration tests use a separate service and are hidden behind the `keychaintest` build tag, so
`just test` never touches the Keychain. Run them with `just test-keychain`; they create items
under a random `app.unison.test.*` service and delete the items they created.

The clipboard in `platform/darwin/clipboard` is also a cgo package: it links AppKit, polls the
general pasteboard change counter every 300 ms and reads plain text, HTML, a PNG image and the
file URLs of every pasteboard item. A screenshot offers both `public.png` and `public.tiff`; the
PNG is taken as is, a TIFF alone is converted to PNG within the image limits of the pipeline. A
copied file arrives as its path, so copying a large file does not read it here: the pipeline
hashes it in the background. Directories are left out. Content marked with
`org.nspasteboard.ConcealedType`, `TransientType` or `AutoGeneratedType` is reported as sensitive
and its payload is never read. Own writes carry the `app.unison.origin` marker so the pipeline
ignores them. Text and a PNG image share one pasteboard item; each written file becomes its own
item with a `public.file-url`, which is the form Finder and Preview read. Without cgo the
operations return `clipboard.ErrUnsupported`. The pasteboard integration tests are hidden behind
the `pasteboardtest` build tag, because they change the user clipboard: run them with
`just test-pasteboard`, they save every item and type first and put the content back afterwards.

The pasteboard permission alert is not active on current macOS. `NSPasteboard.accessBehavior`
reports always allow, and reads from a plain binary, an ad-hoc bundle and a launchd job all
succeed. Apple shipped the alert as a developer preview in macOS 15.4: the
`EnablePasteboardPrivacyDeveloperPreview` default enabled it per application, decisions lived in
Privacy & Security > Paste from Other Apps and `tccutil reset Pasteboard` cleared them. The preview
is off by default and macOS 27.2 ignores the default. The package exposes `AccessBehavior` so a
daemon can detect an ask or deny policy and direct the user to System Settings instead of reading
nothing silently: `unisond` checks the policy at startup and logs a warning that names the setting
when reads may show an alert or be refused.

The Windows keystore in `platform/windows/keystore` keeps every secret in its own file, protected
with DPAPI for the current Windows user. The secret name maps to the file path with a `.bin`
suffix, so `identity/ed25519-seed` becomes `identity/ed25519-seed.bin` below the keys directory,
which the constructor takes. Files are replaced atomically through a temporary file and a rename,
and the optional entropy value passed to the constructor is mixed into the protection. The
package builds on every platform; elsewhere its operations return `keystore.ErrUnsupported`. Its
tests call the real DPAPI, so they run only on Windows: `just vet-windows` checks the build and
the platform independent parts from any host.

The Windows clipboard in `platform/windows/clipboard` uses the Win32 API through
`golang.org/x/sys/windows`, without cgo. A message-only window on a locked OS thread receives
`WM_CLIPBOARDUPDATE` notifications through `AddClipboardFormatListener`, so the watcher does not
poll. Reads cover `CF_UNICODETEXT` and `HTML Format`: the CF_HTML header carries the byte offsets
of the document and of the fragment, and writes wrap the fragment into such a header again.
Content with the `ExcludeClipboardContentFromMonitorProcessing` format, or with
`CanIncludeInClipboardHistory` or `CanUploadToCloudClipboard` set to 0, is reported as sensitive
and its payload is never read. Own writes carry the `Unison.Origin` marker. A clipboard that
another application holds open is retried for up to a second before an operation gives up.
Elsewhere the operations return `clipboard.ErrUnsupported`. The integration tests are hidden
behind the `clipboardtest` build tag, because they change the clipboard: run them on Windows with
`just test-clipboard`, they put back the plain text they found but not other formats.

The protobuf code generators are pinned as tools in `go.mod`, so `just proto` needs only `buf` and
`go` installed. Generated Go code is committed and builds do not require `buf`.

The peer protocol lives in `core/session`. A session runs on an authenticated transport connection:
both sides write `Hello` on the control stream, the first stream that already carried the
authentication proof, and read the peer one. A peer that announces another protocol version is asked
to update: the session reports the `peer-needs-update` event and closes the connection with code
`0x10`. A malformed Hello or another protocol violation closes it with code `0x11`.

Every message is a uvarint payload length followed by a protobuf message, and the length is checked
against the limit before the payload is allocated. Each control message is checked against the
capability matrix of the peer before its handler runs. A message the role of the peer forbids is
dropped and counted as a violation, and three violations end the session. Clip announcements are
also rate limited with a token bucket. Request streams are bidirectional and carry one request with
its answer, transfer streams are unidirectional; the payload type of the stream header selects the
handler, which also states the capability its streams need.

Handlers are registered before the session is opened, because the peer may send messages right after
its Hello. The limits are constants of that package: one MiB per control message and per stream
frame, 16 request and 32 transfer streams in flight, a ten second Hello and stream header timeout,
five clip announcements per second with a burst of ten. A stream releases its slot of the connection
only when both of its directions are closed, so the session drops the rest of the read side of a
request stream once its handler is done; a fetch that keeps its streams open stalls as soon as it
reaches the limit of the connection.

`core/node` assembles the core into a running device and is the only package that composes the
domains. `node.New(node.Config{...})` takes the data directory, the device name and platform, the
platform ports (keystore, clipboard, discovery), an optional clock and logger, the connection
limits and a function that binds the packet connections, so a test runs the node on the virtual
network. `Start` opens the database with the migrations of the trust, clipboard, blob and history
domains, loads the identity, binds the main listener on the persisted port and a second socket for
outgoing connections, announces the device, opens the blob store with its transfer service and
fetcher, reads the clipboard policy and the history lifetimes from the settings and starts the
clipboard pipeline. `Stop` ends the sessions, the sockets, discovery and the database.

Every paired device has one connection. A simultaneous dial is settled by comparing the device IDs:
both sides keep the connection that the device with the lower ID initiated, and that device dials
again when it is online on a connection the other side initiated. Addresses come from three
places: the discovery candidates, the address cache of the peer and a manually entered address. The
address a peer connected from is cached last, because it is the ephemeral port of the peer dialer.
After a lost connection the node reconnects with a delay that doubles from one second up to thirty
seconds and falls back to the cached and the discovered addresses. `NetworkChanged` announces the
device again and retries every peer that is offline. The main listener port is stored by the
transport in `transport.main_port`, so the address cached by the other devices survives a restart.

The clipboard pipeline announces every captured item to the online Personal peers and applies what
they announce. Text, images and files are captured: images are normalized to PNG and the original
format travels as a second representation while it stays below twice the PNG size. Payloads above
64 KiB go to the blob store, so the announcement of a large item carries only hashes; files are
hashed on a background queue and announced once the hash is ready. An incoming item follows the
policy: an applied item is written to the clipboard, a metadata item stays in the history for a
later download, and an item that needs confirmation waits for the user. Downloaded files go to
`~/Downloads/Unison` and reach the clipboard as paths.

`Subscribe` returns a `*node.Subscriber` with an event queue of its own: a subscriber
that falls behind misses events instead of holding up the core. The events are `peer-online`,
`peer-offline`, `clipboard`, `clip-download`, `clip-apply`, `peer-incompatible`, `transfer` and the
pairing events `pairing-candidate`, `pairing-request`, `pairing-paired`, `pairing-failed` and
`pairing-closed`. The node also owns the
pairing window (`OpenPairing`, `PairingCandidates`, `StartPairing`, `ConfirmPairing`,
`RejectPairing`, `ExtendPairing`, `ClosePairing`) and translates the window events. `AddSessionHook`
registers handlers on every new session, which is how the clipboard and the blob transfer attach to
the protocol. Peers are always reported on the LAN path.

Incoming items pass the clipboard policy of `core/policy`, which the node builds over the trust
store, the network state and the peer path, and loads from the `policy.*` settings. A node that has
not been told its network state treats itself as online. Blobs live in the `blobs` directory of the
data directory; `Blobs`, `Fetcher` and `BlobPeers` give the local API and the share flow the store,
the fetcher and the peers that may serve a blob.

The history in `core/history` is the log of the device: one row per origin device and origin
sequence number, and the version vector that outlives the cleanup of expired entries. Captured and
received items are recorded there together with the payload description of every blob, so the local
API can tell a stored payload from one that still has to be downloaded. A manual deletion adds a
tombstone with the id of the deleted item to the log of the device that deletes, so it travels to
the other devices like any other entry. Expiry is decided by the origin device and carried in the
item: the queries skip entries past their expiry and a cleanup every ten minutes removes them, so
an expired item disappears on every device without a network exchange. The lifetimes are the
`history.ttl.text`, `history.ttl.image` and `history.ttl.files` settings, seven days, three days and
one day by default.

When a session with a Personal peer opens, both sides send the version vector of their history in
`HistorySync`, and `HistoryBatch` answers it with the entries the other side misses, oldest first,
up to 64 entries or 512 KiB per batch and 4096 entries per exchange, so an exchange that was cut
short continues with the next one. A Friends peer takes no part in the exchange. Payloads are not
part of it: `Fetch` downloads the missing blobs from the online peers through the blob transfer and
writes them to a path, and `Copy` puts an item back on the local clipboard. The local API exposes
the log as `ListHistory`, `GetHistoryItem`, `DeleteHistoryItem`, `FetchHistoryItem` and
`CopyHistoryItem`; the CLI prints it with `unison history` and `unison clip get --from`.

`core/share` sends files the user picked. The sender stores the files in the blob store and offers
them with `ShareOffer{transfer_id, files}`, and the receiver answers with `ShareDecision`. The
trust matrix decides what happens to an offer: a Personal peer is accepted at once, a Friends peer
is kept as a pending request until the user accepts or rejects it, and a peer the matrix refuses
gets a rejection. Nothing of an undecided offer is stored: the payloads are downloaded only after
the decision.

A transfer downloads its files by pulling them over the blob protocol with the payload type
`share.request` and the transfer ID of the offer, so an interrupted transfer continues from the
chunks that are already stored and every chunk is verified against the hash tree before it is
written. The sender serves those requests only while it tracks the transfer as offered, accepted
or transferring, which is what the `transfer.ShareAuthorizer` of the share service checks. The
received file is written to the downloads directory, which is the `share.downloads_dir` setting or
`~/Downloads/Unison` by default, under a safe name with a counter when the name is taken; a
platform that keeps files elsewhere passes a `Sink`, which is how Android hands them to
`OpenBlobSink`. `ShareCancel` stops a transfer from either side, `ShareResult` reports the outcome
of the receiver back to the sender, and the service resumes a transfer when the session with its
peer comes back. The limits are 32 files per transfer, the size limit of the blob store per file,
16 offers that wait for a decision, ten minutes for an offer to wait and 64 transfers kept in
memory.

`core/api` serves the local API over a node: the `UnisonService` of `proto/unison/local/v1` with
the status, device, pairing, clipboard, transfer, pause and settings calls. The service takes the
node, the daemon version and the device name the node runs with, and an optional `LocalClip`, which
is how `SetClip` reaches the clipboard pipeline: without it the call reports that it is not
implemented, and the daemon passes the pipeline of the node. `Handler` returns the connect handler
with the request and response size limits. The errors of the core are mapped to connect codes: a
missing peer is `not_found`, a closed pairing window or a stopped node `failed_precondition`,
invalid input `invalid_argument` and a refused device `permission_denied`. The calls that manage
paired devices are part of the contract but report `unimplemented`.

`core/api/localconn` serves that API on the endpoint of the daemon: the unix socket
`~/Library/Application Support/Unison/unisond.sock` on macOS and the named pipe
`\\.\pipe\unison-<user-sid>` on Windows. `UNISON_LOCAL_API` overrides the endpoint, so a second
daemon can run next to the installed one. The directory is created with mode 0700 and the socket
with 0600, and a client refuses a socket that belongs to another user or that other users can
reach; the descriptor of the pipe grants access to the current user only. The endpoint speaks
HTTP/2 in clear text, so no certificate is involved: `Serve` runs the connect handler on the
endpoint and `NewClient` returns a generated client for it. `NewHTTPClient` serves the clients that
run across a proxy.

`WatchEvents` carries the events of the node bus: a peer that comes online or goes offline, a
clipboard item, a peer that has to be updated and the pairing events with the address of a
candidate and the SAS. Device IDs are lowercase hex and times are unix milliseconds. The response
of a server stream arrives with the first event, so a client opens the stream before it needs the
events. `GetClip` returns the latest clipboard item. The settings hold the device name, which the
daemon reads at the next start, the confirmation flag of the local device, which the next handshake
advertises, and the blob store limit, which applies at once.

`cmd/unisond` runs the daemon of the current user. Its data directory is
`~/Library/Application Support/Unison` on macOS and `%LOCALAPPDATA%\Unison` on Windows, and
`--data-dir` gives an instance a directory of its own. The database, the blob store, the logs and
the lock file live there, and the local API endpoint follows: `unisond.sock` inside the directory
on macOS and a named pipe derived from the path on Windows. `--endpoint` and `UNISON_LOCAL_API`
override it.

One instance owns a data directory. The daemon takes an exclusive lock on `unisond.lock` and also
checks whether an instance answers on the endpoint; a second one reports that another instance is
running and exits with code 1. The system releases the lock when the process ends, so a crash
leaves no lock behind, and an endpoint file that nobody serves is removed at startup.

The log is JSON on `<data dir>/logs/unisond.log`, five MiB per file with three rotated files kept.
`--log-stderr` copies the records to the terminal and `--log-level` takes `debug`, `info`, `warn`
or `error`. The log names a clipboard item by its identifier, kind, size and MIME types, never by
its content, and it never carries keys or tokens.

`--clipboard=none` starts a daemon without the system clipboard, which is what a second daemon on
one machine needs: two daemons would otherwise read and write the same pasteboard. The default
data directory keeps its keys in the `app.unison` Keychain service and any other data directory
gets a service of its own; on Windows the keys live below the data directory. Two instances on one
machine are therefore two devices and can pair. `--name` sets the device name announced to peers;
the name stored through the local API is used when the flag is absent. `SIGINT` and `SIGTERM` stop
the daemon: the local API closes, the node stops, and the endpoint disappears. `--version` prints
the version from the `VERSION` file.

`cmd/unison` is the command line client of the daemon and speaks that API, so the core can be
driven without the desktop app. `status` shows the daemon, the network and the peers, `devices`
lists the paired devices and `devices rename` changes a name, `pair` opens a pairing window, offers
the devices that are open for pairing and shows the short authentication code the user has to
compare, `clip get` prints the last clipboard item, `clip set` copies standard input to the
clipboard, `send` offers a file or standard input to the devices of `--to`, and `pause` pauses or
resumes the clipboard synchronization for a duration or until it is turned off.

A device is addressed by a unique prefix of its fingerprint, a prefix of its device ID or its name.
Output is plain text by default and JSON with `--json`, which prints the answer in the JSON form of
the local API messages; `pair` prints one event per line in that mode and keeps the questions on
stderr, and the answer of `clip set` leaves out the payload it just read. The command exits with 0
on success, 1 on a failure and 2 when it cannot reach the daemon. `--endpoint` selects the socket or
the named pipe, and the common flags are read before the command name or right after it, but before
the arguments of the command. The daemon does not serve the device management calls yet, so
`devices rename` reports that the call is not implemented.

The desktop app serves the frontend on `127.0.0.1:1420` during development and puts the
bundle in `desktop/src-tauri/target/release/bundle`.

## macOS packaging

`just package-macos` builds the app with the daemon inside it, signs both, and writes
`packaging/macos/build/Unison.app` and `packaging/macos/build/Unison-<version>.dmg`. The version
comes from the `VERSION` file for the bundle, the disk image and the daemon.

The daemon is `Contents/MacOS/unisond`, a universal arm64 and x86_64 binary: it uses cgo for the
Keychain and the pasteboard, so both slices are built on macOS and merged with `lipo`. The app
itself is built for the host architecture; `just package-macos --universal` builds it universal
as well and needs `rustup target add x86_64-apple-darwin`.

`Contents/Library/LaunchAgents/app.unison.unisond.plist` makes the daemon a login item of the
bundle: `BundleProgram` points at `Contents/MacOS/unisond`, the job starts at login and launchd
restarts it after a crash. The app registers the login item with `SMAppService` on the first
launch of a packaged build, and the Startup toggle of the settings screen registers and
unregisters it afterwards. A system that asks for approval reports `requires-approval` until the
user allows the item in System Settings > General > Login Items. When the login item is turned
off, the app starts the daemon of the bundle for the rest of the session. `--login-item=status`,
`--login-item=register` and `--login-item=unregister` change the login item from the command
line, which is how the packaged app is checked without the window.

The Info.plist of the bundle declares `NSLocalNetworkUsageDescription` and `NSBonjourServices`
with `_unison._udp` and `_unison-pair._udp`, because the daemon of the bundle does the discovery.
The bundle is signed with the hardened runtime and the entitlements in
`desktop/src-tauri/entitlements.plist`: network client for the paired devices and the local API,
network server for the QUIC listener of the daemon.

Signing and notarization take the Apple Developer credentials from the environment and never
store them:

```sh
export UNISON_MACOS_SIGN_IDENTITY="Developer ID Application: Name (TEAMID)"
xcrun notarytool store-credentials unison-notary --apple-id you@example.com --team-id TEAMID
export UNISON_MACOS_NOTARY_PROFILE=unison-notary
just package-macos
```

Instead of the keychain profile, `UNISON_MACOS_APPLE_ID`, `UNISON_MACOS_APPLE_PASSWORD` (an
app-specific password) and `UNISON_MACOS_TEAM_ID` notarize with the account directly.
`UNISON_MACOS_SKIP_NOTARIZE=1` or `--skip-notarize` stops after signing, and `--no-dmg` builds
the app only. Without `UNISON_MACOS_SIGN_IDENTITY` the app is signed ad-hoc, which is enough to
build, install and run the bundle on the building machine but not for distribution: Gatekeeper
rejects it and notarization is skipped.

The login item needs an Apple-issued signature as well. An ad-hoc build can register it, but the
system then stores a code requirement without a hash and launchd refuses to start the job with
exit code 78, so the daemon neither runs at login nor after the registration. Sign with a
Developer ID certificate to check the login item. `SMAppService` reports `not-found` for a bundle
the system has never seen, `not-registered` after a registration was removed and `enabled` for a
registered item.

The daemon is signed under `app.unison.unisond`, and the Keychain items it stores belong to that
identity. A change of the signing identity makes macOS ask for access to the existing items, and
an ad-hoc rebuild changes it on every build. Keep the identifier and the certificate stable across
releases; a first run with an unreadable item set can wait on a Keychain dialog, so the daemon
writes no log line until the dialog is answered.

`just verify-macos [path]` checks the layout, the login item property list, the Info.plist keys,
the entitlements, the hardened runtime and the signature of the bundle, and prints the checks
that need a second machine: the local network prompt, the daemon after a re-login, and the
clipboard between two devices. Its login item check prints the state `SMAppService` reports for
the bundle, which is `not-registered` before the first registration and `enabled` afterwards.

`just aar` binds the `mobile` package with gomobile for API 29 and the `arm64-v8a`,
`armeabi-v7a` and `x86_64` ABIs. The library is written to `android/core/core.aar`, which the
app module depends on as a local file, so the AAR is not committed. The generated Java API
lives in the `app.unison.core.mobile` package; `android-build` runs `aar` first because the
app cannot be compiled without it.

The version of all binaries comes from the `VERSION` file and is injected at build time, so
`bin/unisond --version`, `just build` and the core version shown by the app always agree.

The Android app keeps the core in `UnisonService`, a foreground service of the type
`connectedDevice`. The service starts the core with the app files directory, passes the device
name from the app settings and stops the core when the service is destroyed. It shows the device
name and the number of online devices in a persistent notification on the `service` channel and
keeps the `transfers`, `requests` and `clipboard` channels ready for the other components;
`clipboard` is silent. The service runs without `POST_NOTIFICATIONS`; the notification is then
hidden by the system. The UI talks to the core through `CoreClient`, which runs local API methods
in process and delivers the answers as suspend functions, and collects events from
`coreEvents.events`.

`OperatingMode` decides how long the core runs: `always` also starts the service from
`BOOT_COMPLETED` when the user unlocks the device, `app_open` stops the service when the
activity goes to the background, and `wifi_only` starts with the app while the network policy
decides what it may transfer. The mode is stored in the `unison.settings` shared preferences
under `service.operating_mode`; the startup device name is stored under `device.name`. The
manifest also declares `ACCESS_LOCAL_NETWORK`, which apps targeting API 37 need to reach the
local network.

Incoming clipboard items reach the system clipboard from `UnisonService`: the core hands a
`ClipPayload` to the platform callback, which writes plain text with `ClipData.newPlainText` and
text with HTML with `ClipData.newHtmlText`. Writing is allowed in the background, reading is not,
so a local clipboard is read only by a focused app. `SendClipboardActivity` is such an app: the
`SendClipboardTile` quick settings tile and the send action of the persistent notification open it
as a transparent window, it reads the clipboard when the window gains focus, submits the content
with `Mobile.submitClip` and finishes. A clip that carries
`ClipDescription.EXTRA_IS_SENSITIVE` is never read or sent, and a clip without text is reported as
unsupported. The app screen offers the clipboard on focus when its hash differs from
`clipboard.last_hash` of the `unison.settings` preferences; the hash of the last content this
device sent or received is kept there, so content that just arrived is not offered back to its
origin.

`PauseClipboardTile` opens a dialog with a pause for 5 minutes, for 1 hour or until turned off,
and turns the pause off again. The pause lives in the core and covers both directions of the
synchronization. The manifest declares a `FileProvider` under the `app.unison.files` authority
with a cache directory for clipboard files, which image and file payloads will use.

Identity seeds and the store key are wrapped by an AES-GCM master key that lives in the Android
Keystore under the `unison.master` alias, so a process restart keeps the same device ID. The
wrapped blobs are files in the app files directory and the master key never leaves the keystore.

`just proto-android` generates Java lite classes for `unison.local.v1` and `unison.mobile.v1`
from the protobuf definitions into `android/app/src/main/java`, where the app module compiles
them; run it after changing a proto file and commit the result.

The blob store in `core/blob` keeps files and images addressed by the BLAKE3 hash of the whole
content. Content is split into one MiB chunks and the Bao tree of the content is stored next to
them, so a chunk that arrives from a peer is verified against the hash before it is written. Every
chunk is sealed with the data encryption key of the node and bound to the hash and the chunk index.
A file starts with a fixed header, followed by the tree and the sealed chunks. A transfer that
stops early keeps a chunk bitmap in the database, and `WriteChunk` with `Complete` resumes it where
it stopped. The store is capped by the `blob.max_size` setting, 2 GiB on desktop and 500 MiB on
mobile unless configured otherwise, and evicts the least recently used complete blobs; blobs a
caller pinned and partial transfers are never evicted. The round trip of a 1.2 GiB blob is checked
by `just test-blob-large`, which also asserts that the heap stays bounded.

The relay in `cmd/unison-relay` and `internal/relay` moves opaque datagrams between the members of
a mailbox. A client registers with an access token and up to eight mailbox ids of 16 bytes, the
relay forwards every datagram to the other members of its mailbox and forgets the mailbox when its
last member leaves. Nothing is written to disk, so the mailbox id is all the relay learns about a
pair of devices, and the payload is the ciphertext of the inner QUIC connection.

The main transport is QUIC over UDP with RFC 9221 datagrams and ALPN `unison-relay/1`. The fallback
is WebSocket over TLS on TCP for networks that block UDP: one frame per binary message, path
`/relay`, subprotocol `unison-relay/1`. Both speak the messages from `proto/unison/relay/v1`. A
connection starts with a `Register` frame that carries the token and the mailbox ids and gets a
`RegisterResult` with the limits that apply; on QUIC this exchange runs on the first bidirectional
stream with a uvarint length prefix, on WebSocket on the first two binary messages. After that every
datagram is a `Frame` with a `Datagram` body and no length prefix.

Tokens come from `UNISON_RELAY_TOKENS` or from `UNISON_RELAY_TOKENS_FILE`, one per line, and are kept
as SHA-256 hashes, so a running server never holds them in clear form. Every token has a datagram
rate and a traffic quota per window, 200 datagrams per second with a burst of 400 and 1 GiB per
24 hours by default. A client above the rate loses datagrams, a client that runs out of quota is
closed with a status in the answer and in the application error code.

A datagram stays within a single QUIC packet, so its payload is capped at 1024 bytes by default. The
inner QUIC connection has to keep its own packets within that, which means a packet size below the
1200 bytes of a usual QUIC initial packet.

The server needs a TLS certificate and key, listens on `-quic-addr` and `-ws-addr` and serves health
and metrics on `-http-addr` over plain HTTP for a sidecar scrape. `GET /healthz` and `GET /readyz`
report the process, `GET /metrics` renders the Prometheus text format with transport and drop reason
labels only: no mailbox ids, tokens or addresses. Logs are JSON on stderr and never carry payloads,
tokens or mailbox ids. Run it locally with:

```sh
UNISON_RELAY_TOKENS=secret go run ./cmd/unison-relay \
    -tls-cert cert.pem -tls-key key.pem \
    -quic-addr :8443 -ws-addr :8443 -http-addr :8080
```

Every flag reads an environment variable as well: `UNISON_RELAY_QUIC_ADDR`, `UNISON_RELAY_WS_ADDR`,
`UNISON_RELAY_HTTP_ADDR`, `UNISON_RELAY_TLS_CERT`, `UNISON_RELAY_TLS_KEY`, `UNISON_RELAY_TOKENS`,
`UNISON_RELAY_TOKENS_FILE`, `UNISON_RELAY_LOG_LEVEL`, `UNISON_RELAY_MAX_MAILBOXES`,
`UNISON_RELAY_MAX_DATAGRAM_BYTES`, `UNISON_RELAY_RATE`, `UNISON_RELAY_RATE_BURST`,
`UNISON_RELAY_QUOTA_BYTES`, `UNISON_RELAY_QUOTA_WINDOW`, `UNISON_RELAY_MAX_CONNECTIONS`,
`UNISON_RELAY_MAX_MAILBOXES_TOTAL`, `UNISON_RELAY_PING_INTERVAL` and
`UNISON_RELAY_REGISTER_TIMEOUT`.
