# Android

[Русский](android.ru.md) | English

On Android the core runs inside the app process. There is no daemon and no local socket: the Go
core is bound into the app as an AAR, a foreground service keeps it alive, and the UI calls it in
process.

```
app (Kotlin, Compose)
  CoreClient      local API calls and events
  UnisonService   foreground service, owns the core
  Clipboard       reader, writer, tiles, share target
  BridgeService   AIDL interface for keyboards
core.aar          the Go core, built with gomobile
```

## Modules

| Module | Purpose |
|--------|---------|
| `android/app` | the application: UI, service, clipboard, tiles, bridge service |
| `android/bridge-aidl` | the frozen AIDL contract, published as a Maven artifact |
| `android/core/core.aar` | the gomobile binding of `mobile/`, generated and not committed |

`just aar` binds `mobile/` for `arm64-v8a`, `armeabi-v7a` and `x86_64` at API 29 and writes
`android/core/core.aar`, which the app module depends on as a local file. `android-build` runs
`aar` first, because the app cannot be compiled without it.

`just proto-android` generates Java lite classes for `unison.local.v1` and `unison.mobile.v1` into
`android/app/src/main/java`; run it after changing a proto file and commit the result.

## Go facade

`mobile/` is a narrow, byte-oriented API for gomobile:

| Function | Purpose |
|----------|---------|
| `Start(cfg, platform)` | starts the core with a serialized config |
| `Stop()` | stops the core |
| `Version()` | the version injected at build time |
| `Call(req)` | one local API method, answered with a serialized response |
| `SubmitClip(item)` | submits clipboard content the app read |
| `SubmitFile(meta, fd)` | takes an open descriptor, stores and offers the file |
| `AddPeerCandidate(service, addr, txt)` | reports a discovered instance |
| `NetworkChanged(state)` | updates the network conditions |

Requests and responses are protobuf messages of `proto/unison/mobile/v1`, which wraps the local API.
`Call` covers the same methods as the daemon API, except `WatchEvents` (events arrive through a
callback) and a few calls the mobile UI does not use. A request is limited to 1 MiB and a response
to 1 MiB, except `GetHistoryThumbnail`, which answers up to 8 MiB for a 1024 px preview. A call is
answered within 30 seconds, except `FetchHistoryItem`, which allows 30 minutes.

`Platform` is the interface the Kotlin side implements. Every callback runs on a core goroutine:

| Callback | Purpose |
|----------|---------|
| `OnEvent` | one serialized event |
| `WriteClipboard` | write an incoming item to the system clipboard |
| `OpenBlobSink` | return a descriptor for an incoming file |
| `KeystoreWrap`, `KeystoreUnwrap` | protect and read a secret with the Android Keystore |
| `Announce` | publish or withdraw an mDNS announcement |

The facade keeps one core per process: a second `Start` fails, and `Stop` waits up to ten seconds.

## Ports on Android

The facade supplies all four ports, so the core does not know that it runs in an app:

- **Clipboard.** The core cannot watch the clipboard in the background, so `Watch` reports nothing
  and never fails. The app reads the clipboard when it has focus and calls `SubmitClip`; incoming
  items reach the system clipboard through `Platform.WriteClipboard`.
- **Discovery.** The platform callback publishes and browses mDNS through
  `NsdManager`. Only the `main` and `pairing` services exist on Android: a Friends device cannot
  open a session with Android, so the `friends` service is refused. The own announcement is not
  reported back to the core, and a link-local IPv6 address is reported only for a device that has
  no other one: Android refuses apps the interface list, and Go looks the zone up on every packet.
- **Keystore.** The core stores its keys in `<data dir>/keys` as files, and each file is wrapped with
  an AES-256-GCM key that lives in the Android Keystore under the alias `unison.master`. The file
  format is a version byte, a 12 byte IV and the ciphertext with its tag, and the AAD is the version
  byte. The master key never leaves the keystore, so a process restart keeps the same device ID and
  a copy of the app directory cannot be read.
- **Clock.** The system clock.

## Dashboard

The app opens on the dashboard, the first destination of the bottom bar. It shows the newest items
of the history in three tabs: images as a grid of thumbnails, files as a list with names and sizes,
and text as snippets. Each tab asks the local API for at most the configured number of items, and
the pixels arrive through `GetHistoryThumbnail`; the core decodes the image in pure Go, so no image
payload of a peer reaches a platform decoder.

A tap on an image opens a fullscreen viewer with pinch zoom, drag to pan and a double tap that
toggles between 1x and 2.5x; the close button and the system back return to the grid. A tap on a file
or a text entry opens the preview dialog, which copies, downloads or applies the item when it waits
for a confirmation. A long press on any entry opens the menu with `Send`, `Copy` and `Delete`.
`Send` lists the paired devices that are online and offers the payloads as files: the app stages
them in its cache under `outbox` with `FetchHistoryItem`, hands the absolute paths to `SendFiles`,
and keeps the files while the transfer runs; entries older than a day are removed when the app
starts. `Copy` writes the entry to the local clipboard with the echo marker of the core, so nothing
is announced again. `Delete` asks once more and removes the entry on every Personal device.

The number of items of each tab is a local preference of this device: 6, 12, 24 or 48, by default
12. It lives in the `unison.settings` shared preferences under `dashboard.limit` and is not part of
the settings file. A thumbnail is requested at 256 px for a tile and 1024 px for the fullscreen
viewer; `GetHistoryThumbnail` answers up to 8 MiB, while every other response stays at 1 MiB.

Android has no drag and drop out of an application, so the dashboard does not offer it.

## Updates

The app looks for a release of `lebe-dev/unison` on GitHub, verifies the APK against
`SHA256SUMS` and `SHA256SUMS.sig` with the Ed25519 key of `packaging/release-public-key.txt`, and
installs it through the session of the Android package installer.

- Settings > Updates holds the automatic checks (off by default), the beta channel (off by default)
  and the interval of 6 hours, 24 hours (default) or 7 days. `Check now` always works, also while the
  automatic checks are off.
- `UpdateCheckWorker` runs the unique periodic work `unison.update_check` of WorkManager. `MainActivity`
  also checks on start when the interval has passed, so an OEM battery saver cannot stall the checks
  for long. The last answer lives in the `unison.settings` preferences under `updates.*`, so the
  dashboard shows the banner without a request after a restart.
- The dashboard shows the banner above the tabs, and the dialog renders the changelog with a small
  Markdown reader, never a web view. It shows the download, verification and installation progress,
  the permission it needs and the errors with a retry.
- Verification fails closed: the signature of the manifest is checked before the APK is read, the
  SHA-256 of the APK must match the manifest, and the archive must carry the package name, a version
  code no older than the installed one and the signing certificates of the installed app. A debug
  build therefore reports a clear signature mismatch instead of failing inside the system installer.
- `mobile.VerifyReleaseSignature` checks the signature, because the JCA provider of Android exists
  only from API 33 while the app runs from API 29. An APK is never downloaded in the background.
- The key comes from `UNISON_UPDATE_PUBLIC_KEY` or from `packaging/release-public-key.txt` at build
  time. The `versionCode` grows with the pre-releases of a version:
  `major * 10000000 + minor * 100000 + patch * 1000 + slot`, where a stable release uses 999 and
  alpha, beta and rc use their own ranges, so `0.3.0-beta.2` upgrades to `0.3.0-rc.1` and then to
  `0.3.0`.

## Service

`UnisonService` is a foreground service of the type `connectedDevice`. It owns the core for the
lifetime of the app:

- it starts the core with the files directory and the device name of the settings, and stops a
  leftover core before a new one starts;
- it stops the core when the service is destroyed;
- it shows the device name and the number of online devices in an ongoing notification on the
  `service` channel;
- it handles the actions of that notification (`Send clipboard`, download, apply, cancel) and of the
  request notifications.

The service is `START_STICKY`, survives a task removal, and restarts after `BOOT_COMPLETED` for the
modes that allow it.

| Operating mode | Behavior |
|----------------|----------|
| `always` | the core runs from boot and from the app |
| `wifi_only` | the core runs on Wi-Fi or Ethernet and waits otherwise |
| `app_open` | the service runs while the activity is in front |

The mode is stored in the `unison.settings` shared preferences under `service.operating_mode`, and
the device name under `device.name`. The core reads the network conditions from `NetworkMonitor`.

| Channel | Importance | Content |
|---------|------------|---------|
| `service` | low | the ongoing status, with the send action |
| `transfers` | low | the progress of outgoing transfers |
| `requests` | high | an offer that waits for a decision |
| `confirmations` | high | a held clipboard item |
| `clipboard` | low, silent | an item that only arrived as metadata |

## Clipboard

The system allows an app to write the clipboard in the background but not to read it, so the two
directions are separate.

**Reading.** `SendClipboardActivity` is a transparent window that the `Send clipboard` quick
settings tile, the notification action and the main screen open. It reads the clipboard when the
window gains focus, submits the content with `SubmitClip` and finishes. A clip with
`EXTRA_IS_SENSITIVE` is never read; a clip without text is reported as unsupported. The main screen
also offers the clipboard on focus when its hash differs from `clipboard.last_hash`, so content that
just arrived is not sent back to its origin.

**Writing.** `UnisonService` writes incoming items through `Platform.WriteClipboard`. Plain text
becomes `ClipData.newPlainText`, text with HTML becomes `ClipData.newHtmlText`, both labelled
`Unison` and tagged with the item ID. An image is written as a `FileProvider` URI under
`cache/clipboard` of the `app.unison.files` authority; older files are swept.

**Pause.** `PauseClipboardTile` opens a dialog with 5 minutes, 1 hour or until turned off, and the
pause lives in the core, so it covers both directions.

## Files

An incoming file is written through the platform sink: `BlobSink` creates an entry in
`Download/Unison` through `MediaStore`, writes the bytes over a pipe and marks the entry complete at
the end. A folder the user picked with the system picker is preferred; Unison gets access to that
folder and to nothing else, so the app asks for no storage permission. A size mismatch discards the
entry.

While a file arrives, `UnisonService` shows its progress on the `transfers` channel, at most once
a second. When the transfer ends, the same notification turns into the result: a received file
moves to the `received` channel, which makes a sound, and a tap opens the system list of downloads
(the app when the user picked a folder). Both follow the `Transfers` switch of the notification
settings; offers that wait for an answer keep their own request notification.

A download from the history stages the file in the cache, then copies it into the same place and
cancels its notification.

## Share target

`ShareActivity` is the share target of other apps. It accepts streams from `content://` URIs and
text, lists the online Personal devices for both and the Friends devices for files, and hands every
file to the core with `SubmitFile`. It reads its content again when another share arrives while it
is open, and the devices again when it comes back to the front. The limit is 32 files.

## Pairing

The `Add device` screen opens a pairing window and shows the QR code. The code can be read by the
`Scan a code` button or by the system camera, which opens the `unison://pair` link in Unison. A link
never pairs on its own: the app asks once more and names the role the other device will get.

## Keyboard bridge

The app serves an AIDL interface to keyboard apps that are signed with the same key. The contract
lives in `android/bridge-aidl` and is frozen: new methods are only appended, and a caller reads
`protocolVersion()` before it uses methods it does not know.

| Property | Value |
|----------|-------|
| Package | `app.unison` |
| Bind action | `app.unison.bridge.BIND` |
| Service | `app.unison.service.BridgeService` |
| Permission | `app.unison.permission.BRIDGE` (signature) |
| Protocol version | 2 |

The service checks on every call that the caller is this app or a package signed with one of its
certificates. Text and HTML travel as parcels; a binary is passed as a pipe, up to 16 MiB. A publish
reports its outcome through `IPublishCallback`, and a registered listener receives incoming items and
the pause state.

`just bridge-publish` publishes the module as `app.unison:bridge-aidl:<version>` to the local Maven
repository, which the keyboard repository adds as a repository.

## Build

| Recipe | Result |
|--------|--------|
| `just aar` | `android/core/core.aar` |
| `just android-build` | the debug APK |
| `just deploy-android` | installs the APK over USB |
| `just android-lint` | Android lint, ktlint and detekt |
| `just android-check` | ktlint and detekt |
| `just bridge-publish` | the AIDL artifact |

The app targets API 37, runs from API 29, and declares `ACCESS_LOCAL_NETWORK`, which apps targeting
API 37 need to reach the local network. The `versionName` comes from `VERSION`.

ktlint checks the format and detekt the code of the Kotlin sources. The detekt rules live in
`android/config/detekt/detekt.yml`, and `android/config/detekt/baseline.xml` keeps the findings of
the existing code.

## Error reporting

`diagnostics/Diagnostics` wraps Sentry for the app with the component tag `android`. The DSN comes
from the build, reporting is off until the user grants consent, and only uncaught exceptions and ANRs
are sent: breadcrumbs, tracing and personal data are disabled, and paths are redacted.

## Application log

The user turns the application log on in Settings > Application log. While it is on, the shell
appends its own records to `logs/app.log` of the data directory and the core writes `logs/journal.log`;
both files rotate and keep backups, and `logs/journal.on` exists exactly while the log is on, so the
switch applies without restarting the service. Records use the same line format as on the other
platforms, and absolute paths become `[path]`.

Copy and Share build one plain-text file from a header and the merged records of both files and their
backups; the live page reads the current files only. The header carries the app and core versions,
the OS, the device, the network condition, the listener addresses and the settings, with
`downloads_dir` shown as `[path]`; relay tokens and keys are never included. The display level of the
page filters the live view only: an export always carries the debug records too.

## Settings file

Settings > Settings file keeps the settings of the device in a JSON document and restores them. The
app asks the core for the document with `ExportSettings`, adds the service mode and the notification
categories under `app.android`, and writes the file through the system document picker. An import
reads the picked file, applies the document with `ImportSettings`, then applies the app section and
reloads the screens. The format is described under [Settings file](settings-file.md).
