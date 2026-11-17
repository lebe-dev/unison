# Install

Unison runs on macOS and on Android. Windows and Linux are not supported yet.

## macOS

Unison needs macOS 13 Ventura or newer.

The macOS build is a disk image named `Unison-<version>.dmg`. Open it and drag `Unison` into
`Applications`.

The app carries the daemon, the background process that holds the identity, the paired devices and
the connections. Starting the app starts the daemon as well; the footer of the window shows
`Daemon <version>`, or `Daemon offline` with the reason of the last failed start.

On macOS 15 and newer the first start asks whether Unison may find and connect to the devices of
the local network. Allow it. Without the permission the daemon still runs, but it neither announces
itself nor finds other devices, and the window shows a warning with a button that opens the Local
Network settings; see [Troubleshooting](troubleshooting.md).

`Start Unison at login` in the settings keeps the daemon running after a restart. macOS may ask you
to approve the login item in System Settings > General > Login Items.

| What | Where |
|------|-------|
| Database, cached payloads, log | `~/Library/Application Support/Unison` |
| Log file | `~/Library/Application Support/Unison/logs/unisond.log` |
| Keys | the login Keychain, under the service `app.unison` |

A release build is signed with a Developer ID and notarized, so it opens like any other app. An
unsigned build has to be allowed in System Settings > Privacy & Security, and it cannot reach the
login Keychain from inside the app, so the window reports `Daemon offline`.

### Remove the app

1. Revoke this device from another Personal device, so that the group drops it. The settings of the
   app have no way to do it on the Mac itself; the command line client does, see
   [Command line](cli.md).
2. Quit Unison and drag `Unison` from `Applications` to the bin.
3. Delete `~/Library/Application Support/Unison`.
4. Delete the items of the service `app.unison` in Keychain Access.

Copies of received files stay in `~/Downloads/Unison`, or in the folder chosen in Settings.

## Android

Unison needs Android 10 (API 29) or newer.

The Android build is a debug APK at `android/app/build/outputs/apk/debug/app-debug.apk`.
`just android-build` produces it, and `just deploy-android` rebuilds it and installs it on the one
phone that is connected over USB. `adb install -r <path>` works as well. There is no signed
release build for Android yet.

The first start asks for notifications and, on Android 17 and newer, for access to the local
network. Allow both: the notification belongs to the foreground service that keeps the daemon
alive, and the local network permission lets the phone find the other devices and be found.

The service runs while its notification is visible. `Settings > Service mode` decides when it runs:

- `Always` - in the background, also after a restart of the phone;
- `Wi-Fi only` - only while the phone is on Wi-Fi;
- `While the app is open` - only while Unison is on the screen.

Received files go to `Download/Unison`. The database, the cached payloads and the keys stay in the
private directory of the app. Uninstalling the app deletes the identity, the paired devices and the
history, so revoke the phone from a Personal device first, or use `Settings > Remove this device
from the group`.

Android does not let an app watch the clipboard in the background. The phone therefore sends the
clipboard when you ask it to, from the quick settings tile, the notification or the app; incoming
text and images are written to the clipboard without asking. See
[Clipboard and history](clipboard.md).

## Windows and Linux

Not available yet. There is no installer, no package and no supported way to run Unison on Windows
or Linux at the moment.
