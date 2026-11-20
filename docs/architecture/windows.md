# Windows

[Русский](windows.ru.md) | English

On Windows the daemon runs as `unisond.exe` next to the app, as an external binary of the bundle.
It has no window and no console, holds the DPAPI protected secrets, watches the clipboard and serves
the local API on a named pipe.

```
%ProgramFiles%\Unison\
  Unison.exe          Tauri shell, the window
  unisond.exe         the daemon, GUI subsystem
```

## Platform ports

`platform/windows` provides two of the four ports and builds on every host, so `just vet-windows`
checks it from any machine. Outside Windows the operations return `ErrUnsupported`.

### Keystore

`platform/windows/keystore` keeps every secret in its own file, protected with DPAPI for the current
Windows user.

- the secret name maps to the file path with a `.bin` suffix, so `identity/ed25519-seed` becomes
  `identity/ed25519-seed.bin` below `<data dir>/keys`;
- files are replaced atomically through a temporary file and a rename;
- the optional entropy value passed to the constructor is mixed into the protection;
- protected blobs belong to the account that wrote them, so another account or another machine
  cannot read them.

The tests call the real DPAPI and run only on Windows.

### Clipboard

`platform/windows/clipboard` uses Win32 through `golang.org/x/sys/windows`, without cgo. A
message-only window on a locked OS thread receives `WM_CLIPBOARDUPDATE` through
`AddClipboardFormatListener`, so the watcher does not poll.

- reads cover `CF_UNICODETEXT`, `HTML Format`, PNG and device independent bitmaps, and a dropped file
  list;
- the CF_HTML header carries the byte offsets of the document and of the fragment, and writes wrap
  the fragment into such a header again;
- an image is written as PNG plus a bitmap for applications without PNG support, and files as a
  dropped file list;
- content with `ExcludeClipboardContentFromMonitorProcessing`, or with
  `CanIncludeInClipboardHistory` or `CanUploadToCloudClipboard` set to 0, is reported as sensitive
  and its payload is never read;
- own writes carry `Unison.Origin`;
- a clipboard that another application holds open is retried for up to a second.

The integration tests are behind the `clipboardtest` build tag and run on Windows with
`just test-clipboard`.

## Daemon

The daemon uses the GUI subsystem and starts without a console, so the log file is the only output.
Its data directory is `%LOCALAPPDATA%\Unison` and holds the database, the blob store, the logs and
the `keys` directory. The endpoint is the named pipe `\\.\pipe\unison-<user-sid>`, and a data
directory of its own gets a pipe name derived from its path. `--endpoint` and `UNISON_LOCAL_API`
override it.

Because the installation directory and the data of a user are independent, an installation for all
users still keeps the secrets and the database per user.

## Packaging

`just package-windows` builds the daemon for the host architecture, puts it at
`desktop/src-tauri/binaries/unisond-<target triple>.exe`, and builds the NSIS installer with
`tauri build --bundles nsis`. The result is
`packaging/windows/build/Unison-<version>-setup.exe`, and the version comes from `VERSION`.

The bundle configuration of Windows lives in `desktop/src-tauri/tauri.windows.conf.json`, which
Tauri merges with the shared configuration. The bundler places the daemon next to `Unison.exe`.

### Installation

The installer installs for all users into `%ProgramFiles%\Unison` by default, lets the user pick
another directory and asks for elevation once. A firewall rule needs elevation, so a per-machine
installation keeps the rule in the same place as the files.

The hooks in `desktop/src-tauri/installer-hooks.nsh` do three things:

| Step | Action |
|------|--------|
| after the copy | writes the startup entry and adds the firewall rule |
| before the uninstall | stops `unisond.exe`, removes the startup entry and the rule |
| after the uninstall | removes `%LOCALAPPDATA%\Unison` when the app data option is set |

- **Firewall.** A rule named `Unison daemon` allows `unisond.exe` on the private and domain
  profiles, UDP only. The public profile stays closed, so a device on a public network neither
  reaches the daemon nor is seen by it. A reinstall deletes the old rule first.
- **Startup entry.** `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\Unison` starts
  `unisond.exe` at the login of the user that ran the installer, so a re-login starts the daemon
  without the app. The startup toggle of the settings writes and removes the same value, and the
  uninstaller removes it. A change of the toggle affects the next login only; it leaves a running
  daemon alone.

The uninstaller leaves the data directory alone unless its Delete app data option is used.

### Installing the daemon from the app

The app installs the daemon on its own when the installation is missing, so a repair needs no
terminal. The window shows "Install the daemon" under the daemon state as soon as the daemon does
not answer and the installation is not in place; the button is absent when the app is installed and
running, and on the other platforms.

The app runs `Unison.exe --install-daemon` once with the elevation prompt. The hidden mode leaves
before the window starts, copies the daemon, writes the startup entry and adds the firewall rule,
and the app then starts the daemon as the user.

- **Source.** `unisond.exe` next to the app, or `UNISON_DAEMON_BIN` for a development build. Without
  a source the app changes nothing and says why; the hidden mode also accepts
  `--daemon-source=<path>` for the path the app resolved.
- **Directory.** `%ProgramFiles%\Unison`, the directory of the default installation. An app that
  already runs from there installs into its own directory, and an app the installer put elsewhere
  still installs the daemon into `%ProgramFiles%\Unison` and starts it from there, since the
  elevated process must not write into a directory the calling user chose. The app never installs
  into a user directory.
- **Elevation.** One prompt covers the copy, the entry and the rule. A prompt the user cancels
  changes nothing, and the window shows the reason in place.
- **Daemon.** The daemon runs as the logged-in user, never as the elevated process, since it needs
  the clipboard and the DPAPI secrets of that user. The hidden mode writes the files, the app starts
  the daemon, and a repeat of the installation leaves a running daemon alone.
- **Report.** The hidden mode writes its steps to a file the app passes, and the app records them in
  its log. An elevated process has no console, so the file is the only channel of the reason.

The startup entry belongs to the account the elevated process runs as. An administrator who
confirms the prompt with other credentials writes the entry for that account, and the current user
gets none; the installer remains the way to install for everybody.

The installer stays the regular way to install and update Unison. The names it uses are the same,
and a test compares them with the constants of the app.

### Signing

The thumbprint of an Authenticode certificate comes from the environment and is never stored:

```sh
set UNISON_WINDOWS_CERT_THUMBPRINT=...
just package-windows
```

`UNISON_WINDOWS_TIMESTAMP_URL` sets the timestamp server and defaults to
`http://timestamp.digicert.com`. Without the thumbprint the installer and the daemon are unsigned:
Windows shows an unknown publisher and SmartScreen warns about the downloaded installer until it has
enough reputation.

`just verify-windows [path]` checks the installer, the subsystem of the daemon and, when the app is
installed, the installation directory, the startup entry and the firewall rule. It prints the checks
that need the user: the re-login, the clipboard between two devices, the firewall dialog and the
uninstall.

## Windows and the rest of the core

The Windows clipboard and the DPAPI keystore are the only platform specific parts, and both build
without cgo. Discovery, the transport, the session, the blob store and the local API are the same
code as on macOS, so a Windows daemon is cross-compiled from any host with
`just build-unisond windows amd64`.

## Current state

The desktop app and the Windows daemon are packaged together and the installer is checked by
`just verify-windows`, but Windows is not a released platform yet: the clipboard integration tests
and the installer flow run on Windows only, and the regular test suite of another host does not
cover them.

