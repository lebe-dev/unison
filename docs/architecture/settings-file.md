# Settings file

[Русский](settings-file.ru.md) | English

A settings file is a JSON document that carries the settings of a device, so the user can keep them
and restore them on this device or on another one. The daemon writes and reads the part it owns, and
an app adds its own local settings to the same document. The file is named after the platform and
the time of the export: `unison-<platform>-YYYYMMDD-HHMMSS.json`, with `macos`, `windows`, `linux`
or `android` as the platform and the time in UTC.

`core/settingsfile` owns the format, `ExportSettings` and `ImportSettings` of the local API move the
document, and the CLI, the desktop app and the Android app show it to the user.

## Document

```json
{
  "schema": "unison-settings",
  "format_version": 1,
  "exported_at": "2026-11-24T10:20:30Z",
  "platform": "macos",
  "app_version": "0.2.0",
  "core": {
    "require_confirmation": false,
    "require_file_confirmation": false,
    "allow_remote_settings": true,
    "blob_max_size_bytes": 1073741824,
    "strip_tracking": true,
    "sentry_enabled": false,
    "journal_enabled": false,
    "history_ttl": { "text": 604800, "image": 259200, "files": 86400 },
    "policy": {
      "relay_max_size_bytes": 1048576,
      "unmetered": {
        "relay": true,
        "text": { "action": "auto", "max_size_bytes": 10485760 },
        "image": { "action": "auto", "max_size_bytes": 10485760 },
        "files": { "action": "metadata", "max_size_bytes": 0 }
      }
    }
  },
  "app": {
    "desktop": {
      "theme": "dark",
      "login_item": true,
      "updates": { "enabled": false, "beta_channel": false, "interval": "daily" }
    },
    "android": { "operating_mode": "wifi_only" }
  }
}
```

The `core` part is the settings of the daemon: the confirmation flags, the blob limit, the tracking
parameters, the error reporting consent, the application log, the history lifetimes and the network
policy. It is written with the values that are in effect, so a restore reproduces the device. The
`app` part holds the local settings of a client, one object per platform family, and the daemon
stores it without interpreting it.

The `app.desktop.updates` object carries the update behavior: whether the background checks are on,
whether the beta channel is included, and the interval `six_hours`, `daily` or `weekly`. It holds no
installed version, no download and no signing key, so a restore changes only what the user chose.

The following values are deliberately absent:

| Value | Why |
|-------|-----|
| device name | it identifies this device, and a restored file would give two devices one name |
| downloads directory | it is a path of this device |
| relay access | it belongs to the group and a token is a secret the API never returns |
| paired devices, history, clipboard | they are not settings, and a file would carry identities and content |

## Import

An import is partial: a key that the document does not carry keeps its stored value. That makes a
hand edited file and a file from another platform usable, and it keeps an import from clearing a
setting the file never knew. Values are checked before anything is written, so a file with an
invalid limit or action changes nothing.

A client applies its own section of `app` and ignores the others, so the same file restores a
desktop and an Android device. The desktop applies the theme, the login item and the update
settings; Android applies the service mode and the notification categories.

## Versions

`format_version` is the version of the format the file was written with. This version of the app
writes version 1.

- An older file is read by a newer app through the migrations of `core/settingsfile`, one step per
  version. Every step turns the decoded document of the previous version into the current one.
- A file without `format_version` is taken as a file of the current version, so a hand written file
  stays usable.
- A file of a newer version is applied with the keys this version knows. The import reports the
  newer version, and the user is told that the rest needs a newer app.
- A key this version does not know is ignored and reported in dotted form, such as
  `core.future_setting`, so a document can carry an experimental setting without breaking an older
  app.
- A file of another schema is refused, and so is a file larger than 512 KiB.

A new setting therefore only has to be added to the struct and to the known keys; older apps ignore
it, and a future version can move a value with a migration step. See `core/settingsfile` for the
steps.

## Clients

| Client | Export | Import |
|--------|--------|--------|
| CLI | `unison settings export [path]` writes the file, or prints the document without a path | `unison settings import <path\|->` applies a file or standard input |
| Desktop | Settings > General > Settings file > Export opens the save dialog | Import opens the file dialog, applies the document and the local settings |
| Android | Settings > Settings file > Export opens the system file picker | Import reads the picked file, applies the document and the local settings |

The desktop shell writes and reads the picked file through `settings_write` and `settings_read`; the
Android app uses the system document picker, so neither needs a storage permission.
