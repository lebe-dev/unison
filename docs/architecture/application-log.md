# Application log

The app keeps a log the user can send to support. It is off by default and the user turns it on in
Settings > General > Application log, or Settings > Application log in the Android app. While it is
off the app writes no journal records.

## Files

Everything lives in `<data dir>/logs`:

| File | Writer | Purpose |
|------|--------|---------|
| `journal.log` | the core (daemon on macOS, the AAR on Android) | the records of the Go code, rotated at 5 MiB, three backups |
| `app.log` | the app shell (Rust or Kotlin) | the records of the shell, rotated around 2 MiB, two backups |
| `journal.on` | whoever toggles the setting | the marker: the file exists while the journal is on |

The marker is the shared state between the shell and the core. The core reads it at startup and the
local API flips it at runtime, so the shell sees a toggle without a restart and a core started later
keeps recording.

On macOS the daemon also keeps `unisond.log`, a JSON log that is written whether the journal is on
or not. It is not part of the shared application log.

## Format

One record per line:

```
2026-11-17T19:48:02.123Z DEBUG connected to the peer peer=3f2a attempt=2
```

The line starts with the UTC time with milliseconds, then the level (`DEBUG`, `INFO`, `WARN`,
`ERROR`), the message and the attributes as `key=value`. A value with spaces is quoted.

The core writes every record at debug level. The window filters what it shows: `INFO` hides the
debug records, `DEBUG` shows everything. Copy and Share always take every record and the header,
whatever the filter shows.

## Privacy

The records never carry clipboard content, file names, paths, keys, tokens or pairings. Absolute
paths are replaced with `[path]` before a record is written, in the same way as the crash reports.
The header of a shared log carries the versions, the device name and ID, the network state, the
addresses and the settings without secrets; the downloads directory appears as `[path]`.

## Header

A shared log starts with the versions of the app, the daemon (macOS) and the core (Android), the OS,
the device, the network, the addresses, the settings and the policy table. A `---` line separates the
header from the records that follow in chronological order.
