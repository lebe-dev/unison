# Desktop app

[Русский](desktop.ru.md) | English

The desktop app is the desktop window of the daemon on macOS, Windows and Linux. It is a Tauri 2
shell with a Svelte 5 frontend and never speaks to the daemon directly: every call goes through
Rust, which opens the local endpoint and proxies the request.

```
Svelte 5 frontend        stores, screens, i18n
  Connect client         protobuf over Tauri IPC
Rust shell (src-tauri)   daemon supervision, local API proxy, tray, login item
  HTTP/2 h2c             unix socket or named pipe
unisond                  the core
```

## Frontend

`desktop/src` is Svelte 5 with TypeScript, built by Vite. Two entry points exist, because the app
opens a second small window for questions:

| Entry | Mounts | Purpose |
|-------|--------|---------|
| `src/main.ts` | `App.svelte` | the main window |
| `src/prompt.ts` | `PromptWindow.svelte` | the prompt above the other windows |

| Area | Path |
|------|------|
| Stores | `src/lib/stores/*.svelte.ts` |
| API client and generated protobuf | `src/lib/api` |
| Screens | `src/lib/screens` |
| Shell glue | `src/lib/*.ts` (tray, notify, files, login, events, network, policy) |
| Translations | `src/lib/i18n` (English and Russian) |

The stores are Svelte 5 rune classes: `daemon`, `dashboard`, `history`, `requests`, `pairing`, `prompts`,
`transfers`, `notifications`, `confirmation`, `theme`, `updater`. A store owns its data and calls the API; a
screen renders it and dispatches actions.

Settings > General > Settings file exports the settings document of the daemon with the theme, the
login item and the updater settings under `app.desktop`, and an import applies the document and then the local settings.
The format is described under [Settings file](settings-file.md).

`daemon.svelte.ts` polls `GetStatus` every 5 seconds, follows `WatchEvents` and reopens the stream
after 3 seconds when it drops, keeps the recent events and pushes the tray state on every refresh.
While the daemon is not answering yet, the store polls it every 300 ms for up to 10 seconds and the
screens show a spinner, so a window that opens before the daemon is up does not look broken. A start
that failed is reported at once, without waiting for the timeout.
Notifications subscribe before the daemon starts, because their action buttons have to be registered
before the first one can arrive.

The i18n layer picks `en` or `ru` from the browser locale. Inline text is not translated: messages
that come from the daemon are shown as they are.

The Settings screen groups its sections into five tabs: General holds the device name, appearance,
dashboard, startup, the updates, the application log and the settings file, Clipboard holds the pause, the
confirmation of new devices, tracking parameters and the history lifetimes, Files holds the received
folder and the network policy, Network holds the friends search, the remote settings switch and the
relay, and Privacy holds diagnostics and the keystore warning. The state of the daemon and the
notices stay above the tabs, so a lost connection is visible from every tab. The screen opens on
General.

### Dashboard

`Dashboard.svelte` is the first section of the sidebar and the screen the window opens on. It shows
the newest items of the history in three tabs: images as a grid of thumbnails, files as a list with
names and sizes, and text as snippets. The thumbnails come from the daemon, so the page decodes no
image itself. The number of items per tab is a local preference (`unison.dashboard.limit` in
`localStorage`); Settings > General offers 6, 12, 24 and 48, and 12 is the default. A click opens a
preview with the full image, the text content, or the files. On macOS, Windows and Linux an image or
a file item can be dragged out of the window; text items cannot.

The Dashboard keeps the update banner at its top, so an update is offered even when the daemon is
connecting or the version of the app is too old for it. The banner opens the dialog with the
changelog, the progress and the retry, or puts the release aside.

The `dashboard` store follows the events of the daemon and reloads the tabs, capped by the configured
number. The History screen keeps its own store and is not affected by the Dashboard.

## Local API proxy

The daemon listens on a unix socket or a named pipe, and a webview cannot dial either. The frontend
uses the Connect client with a custom `fetch` that sends the serialized request over Tauri IPC:

1. `invoke("api_call", {service, method, body})` for a unary call;
2. `invoke("api_stream", {id, service, method, body, onChunk})` with a Tauri channel for a server
   stream, and `api_stream_cancel` to stop it.

`desktop/src-tauri/src/proxy.rs` opens the endpoint and performs a hyper HTTP/2 handshake in clear
text, then sends the request with the Connect headers:

| Property | Value |
|----------|-------|
| URI | `http://unison/<service>/<method>` |
| Unary content type | `application/proto` |
| Stream content type | `application/connect+proto` |
| Request timeout | 30 s |
| Response limit | 16 MiB |

The service and method names are checked against a character whitelist, and the endpoint is checked
before use: the directory must not be writable by other users, the socket must belong to the current
user and only that user may reach it, and a symlink is refused. This repeats the check of the Go
client, so a replaced endpoint cannot be reached through the app either.

`streams.rs` keeps a stream registry per webview. A reload cancels every stream of that webview, so
a page that is gone does not keep a `WatchEvents` stream open.

## Rust shell

| Module | Responsibility |
|--------|----------------|
| `lib.rs` | the Tauri builder, the window, the command list |
| `daemon.rs` | endpoint resolution and daemon supervision |
| `agent.rs` | the login item: `SMAppService` on macOS, the `Run` key on Windows, the user unit on Linux |
| `proxy.rs`, `streams.rs` | the local API bridge |
| `tray.rs` | the tray icon, its menu and the file pickers |
| `prompt.rs` | the frameless prompt window |
| `files.rs` | show a file in the folder, open files |
| `dragout.rs` | stage the content of an item and start a native drag out of the window |
| `journal.rs` | the application log of the shell and its export |
| `updater/` | the checks, the download, the verification and the installation of an update |
| `settings.rs` | the settings document of the shell, read and written in the file the user picks |
| `observability.rs` | error reporting of the shell |
| `redact.rs` | the paths removed from reports and logs |
| `i18n.rs` | the locale of the shell messages |

### Daemon supervision

`daemon.rs` resolves the endpoint the same way as the Go client: `UNISON_LOCAL_API` first, then the
default path of the platform, which is `$XDG_DATA_HOME/unison` on Linux.

1. probe the endpoint for up to 500 ms;
2. on Linux, ask the service manager to start the user unit with `systemctl --user start
   unisond.service`, and wait up to 10 seconds for it to answer. A session without a service manager
   falls back to the binary of the app;
3. when the login item of macOS is enabled, wait up to 3 seconds for launchd to start the daemon;
4. otherwise start the bundled `unisond` next to the app, and wait up to 10 seconds for it to
   answer, polling every 200 ms.

The state is reported as `running`, `offline` or `failed` with a reason, and the frontend shows it.
On Linux the reason of a failed start names `journalctl --user -u unisond -n 50`, where the journal
of the unit holds the details. A daemon that the app started itself is stopped with `SIGTERM` when
the login item takes over, so only one instance runs per data directory. In a debug build the binary
is taken from `bin/unisond`, `UNISON_DAEMON_BIN` overrides it, and the unit is never used.

### Window and tray

Closing the window hides it: the app keeps running in the tray until `Quit Unison` is chosen. The
daemon outlives the app either way.

The tray menu carries the status, the pause choices (5 minutes, 1 hour, until turned off, off), the
recent clipboard items, a submenu that sends files to a device, `Open` and `Quit`. The frontend
pushes the state with `set_tray_state`, so the tray and the window never disagree. A menu action is
delivered to the frontend as a `tray-action` event.

macOS and Windows open the menu on a click of the tray icon. Linux shows the icon through the
status notifier host of the desktop, which a GNOME session provides with the AppIndicators
extension; a click there opens the menu and never reaches the app, so every action is a menu item.
The icon is the coloured icon of the app, because a monochrome glyph is invisible on a dark panel.
When the session has no status notifier host, the app starts without the icon and closing the window
quits it, so the window is never left running out of reach.

Notifications come from the system notification server. Their buttons are registered before the
first notification and report `accept`, `decline`, `apply`, `show-in-folder` and `open`. A desktop
that shows no buttons reports a click on the notification itself as `tap`, and the app brings its
window up, where the request waits.

The prompt window is a second frameless webview of 360 by 150 pixels in the top right corner, above
the other windows, without a taskbar entry. It asks whether to apply a clipboard item or to accept
files, and can be put away with `Later`.

### Dragging items out

On macOS, Windows and Linux an image or a file item of the Dashboard can be dragged out of the
window. When the press becomes a drag, the frontend asks `drag_staging_dir` for a new directory
under the app cache (`drag/`), then fetches the payloads into it with `FetchHistoryItem`. Once the
copies are written, `start_file_drag` receives their paths and the backend in `dragout/` starts a
native drag with `DragItem::Files` and `DragMode::Copy`. A plain click stages nothing and opens the
preview instead. Several files of one item are dragged together. The drag preview is the first
staged image, or the icon of the app.

The backends differ in what starts the session: macOS begins an `NSDraggingSession` and returns
while the user still drags, Windows runs an OLE `DoDragDrop`, and Linux sets the drag source of the
GTK `ApplicationWindow` that Tauri exposes with `gtk_window()`. The Linux backend needs the primary
button to be pressed when the command reaches the main thread, so the copies must be ready while the
user holds the pointer.

`start_file_drag` canonicalizes every path and accepts only files below the cache directory, so the
window cannot hand an arbitrary file of the user to another app. The staging directories are private
to the user. The copies stay after the drop: a terminal inserts the path of the copy, and a command
line tool can still read the content later. At startup the shell removes the staging directories that
are older than 24 hours.

The context menu of an item uses the same staging for `Send`: the copies go to one online device
through `SendFiles`. `Copy` does not use the staging: it calls `CopyHistoryItem`, so the daemon
writes the item to the clipboard with the echo marker of its own writes. The copy is therefore not
announced as a new item and reaches no other device. `Delete` removes the item from the history on
every Personal device.

### Application log

Settings > General > Application log turns on a log for support. The daemon marks it with
`logs/journal.on` in the data directory of the platform, which is
`~/Library/Application Support/Unison` on macOS, `%LOCALAPPDATA%\Unison` on Windows and
`$XDG_DATA_HOME/unison` on Linux; it writes
`logs/journal.log`, and the shell writes `logs/app.log` next to it. Both files rotate and use the
same line format: a UTC timestamp with milliseconds, the level, the message and attributes. The
shell checks the marker for every record, so the toggle applies without a restart, and records only
daemon start failures, failed shell commands, failed copies or shares and panics.

- The export is plain text named `unison-log-YYYYMMDD-HHMMSS.log`. Its header carries the app and
  daemon versions, the version of the system, the device, the network condition, the addresses and
  every setting without secrets; `downloads_dir` is always `[path]`.
- Copy and Share always include all levels, including DEBUG, while the page display level only
  changes what the window shows.
- The file is written to the temporary directory and offered by the macOS share sheet. When the
  sheet cannot be shown, the app asks where to save the file and reveals it in the file manager.

### Updates

Settings > General > Updates holds the update behavior. Automatic checks are off by default, and so
is the beta channel, which adds the GitHub pre-releases. The interval is every 6 hours, every 24
hours (the default) or every 7 days. "Check now" works even when the background checks are off.

The shell does the network work; the window only reads a snapshot. `updater/` owns the modules:

| Module | Responsibility |
|--------|----------------|
| `mod.rs` | the state, the commands and the `updater-state` event |
| `config.rs` | `updater.json` in the data directory |
| `version.rs` | the SemVer comparison, including a pre-release and a dev version |
| `github.rs` | the releases of `lebe-dev/unison`, their selection and the rate limit |
| `verify.rs` | the download and the verification of the signature and the hash |
| `scheduler.rs` | the background checks, also while the window is hidden |
| `macos.rs`, `windows.rs`, `linux.rs` | the installer of the platform |

A check reads `https://api.github.com/repos/lebe-dev/unison/releases?per_page=20` with an ETag. It
skips drafts, reads the tag with SemVer, keeps a pre-release only on the beta channel, and offers
only a release that is newer and carries the artifact of the platform, `SHA256SUMS` and
`SHA256SUMS.sig`. Every artifact URL must use HTTPS on a host of GitHub; a redirect to another host
is refused. An exhausted rate limit is reported with its reset time.

The download goes to `updates/<version>/` in the cache directory and is verified before anything
runs. The `SHA256SUMS` is accepted only when its Ed25519 signature matches the public key that
`build.rs` embedded from `packaging/release-public-key.txt` or `UNISON_UPDATE_PUBLIC_KEY`. The
artifact is streamed into a `.part` file while it is hashed, capped at 256 MiB, and the part file is
removed on a mismatch, a cancellation or a network failure. Only the verified file reaches the
installer. The staging directories of a previous run are removed at startup after a day.

`main.rs` asks `updater::run_arg()` before the app starts, so the installer of the platform can
finish a detached replacement. A build whose installer is missing reports the reason in the dialog
and leaves the installation button disabled.

## Packaging

`tauri.conf.json` holds the common configuration: identifier `app.unison`, minimum macOS 13, the
entitlements file, the `Info.plist` keys and the LaunchAgent property list as a bundled file. The
version comes from `VERSION` and is passed with `--config '{"version": ...}'` at build time.

| Platform | Configuration | Bundle |
|----------|---------------|--------|
| macOS | `tauri.conf.json` | `.app` with the daemon and the LaunchAgent, then a signed and notarized DMG |
| Windows | `tauri.windows.conf.json` | NSIS installer with the daemon as an external binary |
| Linux | `tauri.linux.conf.json` | `.deb` with the daemon, the CLI and the user unit, built in a container of the target platform |

The window of macOS has no title bar and the traffic lights sit over the sidebar. Windows and Linux
keep the decoration of the desktop, so the window of Linux sets it in its own configuration file.

The app declares only the hardened runtime entitlements `com.apple.security.network.client` and
`com.apple.security.network.server`, and the Bonjour service types of the three mDNS services.

The updater is part of the shell, so no updater plugin is used. A single-instance plugin is unused
too: a second instance is refused by the daemon, not by the shell, and the user starts a second
daemon with its own data directory.

Release artifacts are signed with `just release-sign`, which writes `release/SHA256SUMS` and
`release/SHA256SUMS.sig` through `cmd/unison-release-sign` and verifies both. The private seed stays
outside git (`UNISON_RELEASE_SIGN_KEY`, `UNISON_RELEASE_SIGN_KEY_FILE` or
`.project-infra/release-signing.key`), and the public key is committed as
`packaging/release-public-key.txt`. `just release-all` signs when a key is configured and warns
loudly when it is not.

## Development

`just desktop-dev` runs Vite on `127.0.0.1:1420` with hot reload and starts the Tauri shell, which
opens the daemon of the source tree. `just desktop-build` writes the bundle to
`desktop/src-tauri/target/release/bundle`, `just desktop-test` runs the component tests, and
`just lint-desktop` runs ESLint, Prettier and `svelte-check`.

The app of Linux needs WebKitGTK and the toolchain of the platform, so `just package-linux` builds
it and the package in a linux/amd64 container on any host. The daemon and the CLI are cross built.
