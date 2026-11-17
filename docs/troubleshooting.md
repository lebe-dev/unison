# Troubleshooting

## The devices do not find each other

- Both devices have to be in the same network. A guest network, a network with client isolation and
  some VPNs block the multicast that discovery uses.
- On macOS 15 and newer the app needs the local network permission: System Settings >
  Privacy & Security > Local Network. Allow Unison there. A daemon started from a terminal belongs
  to the terminal: the permission of the terminal app is the one that matters.
- When the system refuses the daemon the local network, the window of the app says so above the
  screen and its `Open settings` button opens the Local Network pane. The daemon logs the same at
  the warning level. If the warning stays while the switch of `unisond` is on, the switch may
  belong to an earlier build of a daemon that you built yourself: remove the entry with the minus
  button and start the daemon again to get a new prompt. A bare daemon started from a terminal
  is refused even when the terminal is allowed; `just run-daemon` runs it as an app of its own, see
  `DEV.md`.
- A network that keeps its clients apart blocks the direct connection as well, not only discovery:
  pairing by code fails with "Could not reach the other device", and a paired device shows a red dot
  next to its name on Android or `Not reachable` with a `Details` toggle on macOS. The details list
  the addresses that were tried. Turn off client isolation or AP isolation on the router, leave the
  guest network, or connect both devices to the same VPN. See
  [Devices and pairing](devices.md#connection-problems).
- Pairing by code does not need discovery. `unison pair` prints a code, and the other device scans
  it, or joins with `unison pair --qr <code>`. See [Devices and pairing](devices.md#pairing).
- A paired device can be reached by an address that you enter. `unison status` prints the addresses
  of the device, and `unison devices address <id> <host:port>` stores one on the other side. The
  address survives a restart of both devices.

## The window reports "Daemon offline"

The line under the status tells what happened:

- the daemon is not running on the endpoint: start the app again, or start the daemon yourself;
- the login item waits for approval in System Settings: approve it in System Settings > General >
  Login Items;
- the daemon stopped during startup: the log file says why.

The log is `~/Library/Application Support/Unison/logs/unisond.log`. A build that is only ad-hoc
signed cannot read the Keychain the way a signed app does, and the daemon stops at startup with an
error about the keystore.

## Nothing is copied to the clipboard of this Mac

- The clipboard may be paused: `unison pause` prints the state, `unison pause off` resumes.
- Content that a password manager or the system marks as sensitive is never sent. That is not an
  error.
- macOS does not ask before an app reads the clipboard today. If the daemon reports in its log that
  reading may be refused, allow it in System Settings > Privacy & Security > Paste from Other Apps.

## Keychain errors

- macOS asks once for a service, and again after the program changed. Choosing `Always Allow` keeps
  the answer. A daemon run from a temporary path becomes a new program on every start, which is why
  the recipes of the repository run a binary at a fixed path.
- `keystore: no keychain available, unlock the login keychain` means exactly that: unlock the login
  Keychain and start again. The daemon does not show a dialog of its own.
- The database is encrypted with a key from the Keychain. If the key is gone while the database is
  still there, the daemon reports a missing data encryption key. Delete the data directory and pair
  the device again.

## Two daemons on one Mac

A second daemon that uses the same data directory exits with `another instance is running`. Give it
its own directory and endpoint; each data directory has its own identity and its own Keychain
service.

## Nothing arrives on Android or the service stops

- The foreground service shows a notification. Without the notification permission the system does
  not keep it alive, so allow notifications.
- Android 17 and newer ask for access to the local network. Without it the phone neither announces
  itself nor finds the other devices.
- A phone that stops syncing while the screen is off is usually restricted by the battery
  optimization of the system. Exempt Unison from it in the system settings, and check
  `Settings > Service mode`: `Wi-Fi only` stops the service outside Wi-Fi, and `While the app is
  open` stops it in the background.
- An emulator is not a usable test device: it has no Wi-Fi, so the service discovery of Android
  cannot register or resolve the other devices.

## Items arrive but are not applied

- A confirmation flag is on: this device holds the items in its history, and an item is applied
  when you press `Apply`. Files wait in `Requests` (`Inbox` on Android) the same way. See [Clipboard and history](clipboard.md#holding-items-until-you-apply-them).
- The network policy keeps the item as metadata, or asks for a confirmation, on this kind of
  network. `unison policy` shows the table.
- A Friends device cannot send clipboard items at all; it can only offer files.
- The clipboard may be paused, which drops incoming items without recording them.

## A file does not arrive

- An offer of a Friends device, or of a Personal device you ask about, waits in `Requests`
  (`Inbox` on Android, `unison requests`) and expires after ten minutes.
- An item that is listed as not downloaded needs the device that has it to be online.
- Files land in `~/Downloads/Unison` on macOS and in `Download/Unison` on Android, unless you chose
  another folder in Settings.
- The cache of a device holds at most 2 GiB. A larger file is refused, and a full disk stops the
  download as well.

## A revoked device still appears on another device

The revocation spreads over the sessions between Personal devices. A device that was offline
applies it when it meets one of them, so its list can show the revoked device for a while. The
revoked device cannot connect in the meantime.
