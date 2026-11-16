# Keyboard bridge

The Android app serves a small interface for keyboard apps: a keyboard shows what arrived from the
other devices and sends what you type or copy. The interface is a bound service of the app; the
keyboard itself is a separate application that is not part of this repository.

## Binding

| What | Value |
|------|-------|
| Contract artifact | `app.unison:bridge-aidl` |
| Version of the contract | 1 |
| Package of the service | `app.unison` |
| Service class | `app.unison.service.BridgeService` |
| Intent action | `app.unison.bridge.BIND` |
| Permission | `app.unison.permission.BRIDGE` |

The permission is declared with the `signature` protection level, so a keyboard has to be signed
with the same key as the app; another application cannot bind. The service checks the signature of
the caller on every call as well.

The contract artifact is published to the local Maven repository with `just bridge-publish`. It
holds the interface, the parcelable classes and `BridgeContract` with the names above, so the
keyboard does not need the AIDL sources.

```java
Intent intent = new Intent(BridgeContract.ACTION_BIND);
intent.setClassName(BridgeContract.PACKAGE, BridgeContract.SERVICE_CLASS);
bindService(intent, connection, Context.BIND_AUTO_CREATE);
```

A caller reads `protocolVersion()` first and does not use methods of a newer version. Methods are
only appended to the interface, so version 1 stays what it is.

## Interface

| Method | What it does |
|--------|--------------|
| `protocolVersion()` | version of the contract served by the app |
| `publishText(ClipPayload)` | sends text, plain and HTML, as a clipboard item |
| `publishBinary(ClipPayload, ParcelFileDescriptor)` | sends one binary payload that the caller streams into the pipe |
| `registerListener(IUnisonBridgeListener)` | subscribes to incoming items and state changes; the current state follows at once |
| `unregisterListener(IUnisonBridgeListener)` | removes a subscription |
| `recentRemoteItems(int limit)` | the newest items that came from other devices, at most 20 |
| `openItem(String itemId, String mime)` | opens the content of an item; the caller closes the descriptor |
| `setPaused(long untilEpochMs)` | pauses or resumes the synchronization |

`setPaused` pauses until the given instant when the value is positive, resumes with 0 and pauses
until it is turned off with a negative value.

The listener has two calls: `onRemoteItem(ClipSummary)` for an item that arrived from a Personal
device, and `onStateChanged(BridgeState)` when the pause state or the number of online devices
changes.

The parcelables carry:

- `ClipPayload` - `text`, `html`, `mime`, `size`;
- `ClipSummary` - `id`, `deviceName`, `kind`, `preview`, `mimeTypes`, `createdAtMs`, where the kind
  is 1 for text, 2 for an image and 3 for files;
- `BridgeState` - `paused`, `untilMs`, `onlinePeers`.

## Limits

- A binder transaction is about one megabyte, so binary content goes through a pipe. The service
  reads at most 16 MiB and gives up after 30 seconds.
- Unison accepts an image as binary content; a file cannot be published this way.
- `openItem` returns the content directly for a small item and downloads a large one into the cache
  of the app first, which needs the device that has it to be online.
- `recentRemoteItems` never returns the items of this device, and the pause state is polled every
  30 seconds while a listener is registered.
