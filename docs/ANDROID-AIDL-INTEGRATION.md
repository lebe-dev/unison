# Keyboard bridge

The Unison Android app can serve as the transport of a keyboard that is built by the same vendor.
The keyboard hands the local clipboard to the app and receives the items that arrive from the other
devices of the user. The whole contract is a small AIDL interface in the `app.unison.bridge`
package: the keyboard never talks to the network, never opens the database and never sees a key.

This document is the integration guide for that keyboard. It covers the artifact, the manifest, the
connection lifecycle, every method and callback, the data types, the rules for capturing the
clipboard and the checks that have to be made on a device.

## What the integration gives the keyboard

- **Silent transfer to the desktop.** The keyboard publishes the copy that the user just made. The
  item appears on the clipboard of the paired desktop or laptop in well under a second on the local
  network, without the user leaving the keyboard and without a share sheet.
- **Incoming items in the suggestion strip.** The keyboard is told when another device copied
  something, with a short preview, the kind of content and the name of the device. A chip in the
  strip commits the text into the editor, or hands an image or a file to it.
- **Pause control.** The keyboard reads the pause of the synchronization and can set it, so a
  hardware key or a long press can silence the synchronization for a while.

The integration is optional in both directions. Without it the keyboard is a normal keyboard, and
without the keyboard Unison still takes the clipboard from its share target, its quick settings
tile and its own screen.

## What the integration does not do

- It does not transfer anything by itself. Both parts have to be installed and signed with the same
  key.
- It does not carry files out of the phone. The app publishes text, HTML and images; an item that
  carries files can be received and opened, file by file.
- It does not read the clipboard for the keyboard. Android lets an app read the clipboard only while
  it is in the foreground, so the keyboard reads it from the input method and hands the content over.
- It does not decide what is sensitive. The app skips a clip that carries
  `ClipDescription.EXTRA_IS_SENSITIVE`, the keyboard has to skip password fields.
- It does not queue what it cannot deliver. A publish while the synchronization is paused, or content
  the core cannot carry, is dropped.

## Requirements

| Requirement | Detail |
|-------------|--------|
| Unison | Installed, package `app.unison`, version 0.1.0 or newer |
| Signing key | The keyboard and Unison signed with the same certificate |
| Android | API 29 or newer for the keyboard |
| Pairing | At least one Personal device of the user, otherwise nothing arrives and nothing leaves |
| Synchronization | Not paused; see the pause section |

The permission of the service is declared with `protectionLevel="signature"`. A keyboard signed with
another certificate is refused by the system at bind time, and a keyboard that gets past the
permission is checked again by the app on every call.

### Checking the signing key

Both APKs have to be signed with the same certificate. Compare the SHA-256 digest of the signer:

```sh
apksigner verify --print-certs path/to/unison.apk
apksigner verify --print-certs path/to/keyboard.apk
```

The line `Signer #1 certificate SHA-256 digest` has to be the same. Debug builds taken from the same
machine already match, because Gradle signs every debug build with `~/.android/debug.keystore`. A
build from another machine, another user account or a release keystore needs the same key as the
Unison build it talks to.

`app.unison` is the only package name that matters. A debug build and a release build of Unison
replace each other, because both use that package, and the bridge always talks to the installed one.

## The contract artifact

The contract is published as `app.unison:bridge-aidl:<version>`. The version is the version of the
Unison build, 0.1.0 for the first release.

### From the local Maven repository

The Unison repository publishes the artifact with its own task, which writes it to
`~/.m2/repository` and to `android/build/maven` of the Unison checkout:

```sh
just bridge-publish
```

The keyboard then resolves it like any other dependency:

```kotlin
// settings.gradle.kts
dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
        mavenLocal()
        // Or the build output of a sibling checkout.
        maven { url = uri("../unison/android/build/maven") }
    }
}
```

```kotlin
// build.gradle.kts of the keyboard module
dependencies {
    implementation("app.unison:bridge-aidl:0.1.0")
}
```

The artifact is an AAR with the AIDL interfaces, the parcelables and `BridgeContract`. Nothing else
of Unison is dragged in: the keyboard gets no dependency on the core, the protobuf definitions or
Compose.

### Vendored sources

Without a Maven repository, copy the sources into the keyboard module. Keep the package names:

```
src/main/aidl/app/unison/bridge/IUnisonBridge.aidl
src/main/aidl/app/unison/bridge/IUnisonBridgeListener.aidl
src/main/aidl/app/unison/bridge/ClipPayload.aidl
src/main/aidl/app/unison/bridge/ClipSummary.aidl
src/main/aidl/app/unison/bridge/BridgeState.aidl
src/main/java/app/unison/bridge/BridgeContract.java
src/main/java/app/unison/bridge/ClipPayload.java
src/main/java/app/unison/bridge/ClipSummary.java
src/main/java/app/unison/bridge/BridgeState.java
```

AIDL compilation has to be enabled in the module:

```kotlin
android {
    buildFeatures {
        aidl = true
    }
}
```

A vendored copy is a fork. The protocol version in `BridgeContract.PROTOCOL_VERSION` tells which
contract the copy describes, so update the copy when the app starts to serve a newer version.

## Manifest

The keyboard needs the permission of the service and a package visibility declaration:

```xml
<uses-permission android:name="app.unison.permission.BRIDGE" />

<queries>
    <package android:name="app.unison" />
</queries>
```

The permission is declared by Unison. A keyboard that declares it while Unison is absent installs
normally and simply never gets the permission. The `<queries>` element makes Unison visible to
`PackageManager` and to the service resolution from API 30; without it the keyboard cannot tell
whether Unison is installed or open its settings page. Both elements are children of `<manifest>`,
next to `<application>`.

No `INTERNET` permission and no foreground service of the keyboard are involved.

## Connecting

The service is `app.unison.service.BridgeService` in the package `app.unison`, with the action
`app.unison.bridge.BIND`. Bind with an explicit component, never with the action alone, so the
intent cannot be answered by another app:

```kotlin
val intent = Intent(BridgeContract.ACTION_BIND)
    .setComponent(ComponentName(BridgeContract.PACKAGE, BridgeContract.SERVICE_CLASS))
val bound = bindService(intent, connection, Context.BIND_AUTO_CREATE)
```

`bindService` returns `false` when the system cannot find the service, which is the usual outcome
when Unison is not installed or not visible to the keyboard. It throws `SecurityException` when
Unison is installed but the keyboard does not hold the signature permission.

Use `Context.BIND_AUTO_CREATE` alone. Do not add `BIND_ABOVE_CLIENT`, `BIND_WAIVE_PRIORITY` or a
foreground binding: the app starts its own foreground service when it needs one, and the bridge
itself costs nothing while idle.

The service connection has to deal with four things:

- **A null binder.** `onServiceConnected` can carry a null binder, and `onNullBinding` is called when
  the service returns nothing. Drop the connection and try again later.
- **A dead process.** `onServiceDisconnected` means the app process is gone. The system keeps the
  binding and calls `onServiceConnected` again once the app is restarted, but the new binder is a new
  service instance: the listener has to be registered again.
- **An update.** `onBindingDied` means the binding is dead for good, which happens when the app is
  updated. Unbind and bind again.
- **A refusal.** `SecurityException` from the version check or from a call means the signatures
  differ, or the permission was revoked by a reinstall with another key. There is nothing to retry
  until the app is reinstalled.

```kotlin
private val connection = object : ServiceConnection {
    override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
        val service = binder?.let { IUnisonBridge.Stub.asInterface(it) } ?: return
        // Version check and registration, see below.
    }

    override fun onServiceDisconnected(name: ComponentName?) {
        bridge = null
    }

    override fun onBindingDied(name: ComponentName?) {
        unbind()
        bind()
    }

    override fun onNullBinding(name: ComponentName?) {
        unbind()
    }
}
```

### Binding lifetime

Bind when the keyboard becomes the active input method, or on the first use of the bridge, and unbind
when the keyboard is destroyed. A binding is a cheap reference to a service of another app, but it
keeps that app process alive as long as the keyboard holds it, so do not bind from a background
helper and forget about it.

```kotlin
override fun onCreate() {
    super.onCreate()
    client = UnisonBridgeClient(this)
    client.bind()
}

override fun onDestroy() {
    client.unbind()
    super.onDestroy()
}
```

Call `unregisterListener` before `unbindService`, and wrap `unbindService` in a `try` for
`IllegalArgumentException`, which is thrown when the binding is already gone.

### Rebinding after an install or an update

An update of Unison kills its process and the binding dies; a fresh install of Unison after the
keyboard started leaves the keyboard without a binding. Listen for the package of the app and bind
again:

```kotlin
private val packages = object : BroadcastReceiver() {
    override fun onReceive(context: Context?, intent: Intent?) {
        if (intent?.data?.schemeSpecificPart != BridgeContract.PACKAGE) {
            return
        }
        if (client.bind()) {
            showRecentChips()
        }
    }
}

private fun watchPackages() {
    val filter = IntentFilter().apply {
        addAction(Intent.ACTION_PACKAGE_ADDED)
        addAction(Intent.ACTION_PACKAGE_REPLACED)
        addDataScheme("package")
    }
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
        registerReceiver(packages, filter, Context.RECEIVER_EXPORTED)
    } else {
        @Suppress("UnspecifiedRegisterReceiverFlag")
        registerReceiver(packages, filter)
    }
}
```

The data scheme `package` is required, otherwise no package broadcast matches the filter. Unregister
the receiver in `onDestroy`.

### Death of the service

`linkToDeath` is only needed when the keyboard caches something the app owns, which it should not.
A `DeathRecipient` still helps to drop the cached proxy at once instead of waiting for
`onServiceDisconnected`:

```kotlin
private val death = IBinder.DeathRecipient {
    // Drop the proxy at once; the next call rebinds or fails cleanly.
    bridge = null
}

// In onServiceConnected, after the version check and the registration:
try {
    binder?.linkToDeath(death, 0)
} catch (e: RemoteException) {
    return  // the app is already gone
}
```

Unlink before unbinding, and ignore the result, which reports `false` when the linker is gone:

```kotlin
runCatching { binder?.unlinkToDeath(death, 0) }
```

Everything the bridge returns is a copy; nothing outlives the call.

## Version check

Read the version of the service right after connecting and compare it with the version of the
artifact:

```kotlin
val version = service.protocolVersion()
if (version < BridgeContract.PROTOCOL_VERSION) {
    // The app is older than this artifact, do not use the interface.
    return
}
```

The interface is append-only: new methods are added at the end of `IUnisonBridge` and of
`IUnisonBridgeListener`, existing methods, their parameters, their order and their constants never
change. Transaction codes are therefore stable, and a keyboard built against version 1 keeps working
with every later version of the app.

| Comparison | Meaning | What to do |
|------------|---------|------------|
| `service == artifact` | Same contract | Use everything in this document |
| `service > artifact` | Newer app, more methods | Use the methods you know, they behave as documented; update the artifact to reach the new ones |
| `service < artifact` | Older app | Use nothing beyond `protocolVersion`; this document describes version 1, the first one |

`BridgeContract.PROTOCOL_VERSION` is the version of the artifact. Never hardcode the number in your
own code, and never call a method that a lower version does not have: the transaction fails with a
runtime exception, not with a useful error.

## Method reference

| Method | Blocks | Returns | Notes |
|--------|--------|---------|-------|
| `protocolVersion` | No | `int` | Call once after connecting |
| `publishText` | No | - | Content rides in the transaction |
| `publishBinary` | No | - | Content rides in a pipe |
| `registerListener` | No | - | Immediate state callback |
| `unregisterListener` | No | - | Stops the state poll |
| `recentRemoteItems` | No | `List<ClipSummary>` | Read from a cache of the app |
| `openItem` | No | `ParcelFileDescriptor` | A pipe, content is streamed later |
| `setPaused` | No | - | Result arrives through the listener |

All methods are checked against the signature of the caller and throw `SecurityException` when the
check fails. A call after the app process died throws `RemoteException` or its subclass
`DeadObjectException`; treat both as "not connected" and rebind.

### protocolVersion

```aidl
int protocolVersion();
```

Returns the version of the contract served by the app. It does not change within one installed
version of the app.

### publishText

```aidl
void publishText(in ClipPayload payload);
```

Publishes one clipboard copy as text.

- `payload.text` becomes the `text/plain` representation, `payload.html` the `text/html` one. Both
  are optional, but at least one has to be non-empty; with both empty nothing is sent and nothing is
  reported. `mime` and `size` are ignored here.
- The content travels inside the binder transaction, which is limited to about 1 MB for the pair of
  processes. A larger payload fails in your process with `TransactionTooLargeException`. Keep an
  ordinary copy below a few hundred kilobytes, and send larger text as `text/plain` through
  `publishBinary`.
- The call returns before the item is recorded. The app submits on a background thread and logs a
  failure, which covers the paused synchronization, an unreachable core and content the core cannot
  carry. A successful call means "accepted", not "delivered".
- An empty or null payload is ignored.

### publishBinary

```aidl
void publishBinary(in ClipPayload meta, in ParcelFileDescriptor data);
```

Publishes content that is too large for a binder transaction, streamed through a pipe.

- Create the pipe with `ParcelFileDescriptor.createPipe()`, pass the read end as `data`, write into
  the write end on a background thread and close the write end when the content ends. The app reads
  until EOF.
- `meta.mime` selects the type; an empty value becomes `application/octet-stream`. `meta.text`,
  `meta.html` and `meta.size` are ignored.
- The app accepts at most 16 MiB per publish. A larger stream is dropped without a report.
- The write has to finish within 30 seconds. After that the app closes the read end and the write
  fails with `IOException`. Never wait for a large download before calling: open the pipe, call, and
  stream into it.
- Only `text/plain`, `text/html` and `image/*` are carried by the core. A file, a binary blob or an
  archive with another type is dropped. Images are normalized to PNG by the core; an image above
  64 MiB, 8192 pixels on a side or 24 megapixels is dropped. The 16 MiB of the bridge is the
  practical limit.
- A null descriptor is ignored.
- The call returns at once and the reading runs in the app. Close your copy of the read end right
  after the call returns: the descriptor was duplicated into the app during the transaction, and your
  copy is not needed.

### registerListener

```aidl
void registerListener(IUnisonBridgeListener listener);
```

Subscribes to incoming items and state changes.

- The current `BridgeState` is delivered through `onStateChanged` right after the listener is
  registered. The callback is oneway, so it arrives shortly after the call returns.
- The app reloads its cache of recent items and starts polling the pause and the number of online
  devices every 30 seconds while at least one listener is registered.
- A null listener is ignored. Several listeners are allowed and every one gets every item.
- Registration belongs to the binder: a new connection, a restarted app or an update loses it.
  Register again in every `onServiceConnected`.
- A listener whose process dies is dropped by the app automatically.

### unregisterListener

```aidl
void unregisterListener(IUnisonBridgeListener listener);
```

Removes a listener registered earlier. The poll of the state stops when the last listener is gone,
and the cache of recent items stays. A null listener, an unknown one or one that is already gone is
ignored. Call it before unbinding and in `onDestroy`.

### recentRemoteItems

```aidl
List<ClipSummary> recentRemoteItems(int limit);
```

Returns the most recent items that came from other devices of the user, newest first.

- At most `limit` items, and at most 20 whatever the limit is. Zero or a negative limit gives an
  empty list.
- The summaries carry no content. `openItem` opens the content.
- The list is a cache of the app. It is filled when a listener registers, when an item arrives and at
  most once in 5 seconds after a publish. A read right after connecting can therefore be empty even
  when items exist: read again on the next `onStateChanged` or `onRemoteItem`.
- Deleted and expired items are left out. Items that arrived while the keyboard was not connected
  are included, which is what makes a chip survive a keyboard restart.
- Items of Friends devices are never included.

### openItem

```aidl
ParcelFileDescriptor openItem(String itemId, String mime);
```

Opens the content of an item as a read-only pipe.

- A null or empty id gives null. Any other id gives a descriptor, even when the item is unknown, gone
  or expired; the stream then ends without a single byte. Treat a stream without bytes as "not
  available" and drop the chip.
- `mime` selects the representation by its exact type from `ClipSummary.mimeTypes`. When the item has
  no representation of that type, the first one is used, so an empty string opens the first
  representation.
- A text item is served from the history at once. An image or file that is not on the phone yet is
  downloaded from an online device during the read, so a first read of such an item can take as long
  as the download.
- Read until EOF and close the descriptor. The app closes the pipe when the reader takes nothing
  within 60 seconds.
- The pipe is not seekable and has no length. Read it once, on a background thread, and keep the
  bytes you need.

### setPaused

```aidl
void setPaused(long untilEpochMs);
```

Turns the pause of the clipboard synchronization on or off.

| Value | Effect |
|-------|--------|
| `> 0` | Pause until that instant in Unix milliseconds, which has to be in the future |
| `0` | Resume |
| `< 0` | Pause until it is resumed |

- The deadline is checked against the clock of the phone. An instant in the past or in the present is
  refused and the pause stays as it was; use `0` to resume.
- There is no result and no error. The new state arrives through `onStateChanged`, and a request the
  app cannot pass to its core is dropped.
- The pause is stored by the core, so it survives a restart of both apps, and it covers both
  directions: while it is on, published items are dropped and arriving items are not delivered.
- The pause belongs to the phone: the quick settings tile, the app screen and the keyboard all see and
  change the same state. A pause set on the desktop affects the desktop only.

## Listener

```aidl
oneway interface IUnisonBridgeListener {
    void onRemoteItem(in ClipSummary summary);
    void onStateChanged(in BridgeState state);
}
```

Both callbacks are `oneway`. The app does not wait for your handler, and the call arrives on a binder
thread of your process, never on your main thread. Copy what you need and post it to the main thread:

```kotlin
override fun onRemoteItem(summary: ClipSummary?) {
    summary ?: return
    main.post { onItem?.invoke(summary) }
}
```

An exception thrown in a callback is logged by the app and the callback is lost for that event.

### onRemoteItem

Called for an item that came from a Personal device of the user.

- It fires when the item arrives, when its payload becomes available and when it is applied. The same
  id can be delivered more than once, so deduplicate by `ClipSummary.id`.
- An item that the policy only keeps as metadata is delivered too. `openItem` downloads it, which is
  the intended way to serve a chip.
- Only items from Personal devices are delivered. The clipboard of a Friends device never reaches the
  keyboard.
- Only while a listener is registered. Items that arrived earlier are in `recentRemoteItems`.
- A delivery can arrive right after you unregistered; check a flag of your own before you touch the UI.

### onStateChanged

Called when the pause or the number of online devices changes.

- The first call comes from `registerListener` with the current state.
- Later calls come from a change of the pause, a device that comes online or goes offline, and the
  30 second poll that catches what the events missed.
- It is not a heartbeat: nothing arrives while nothing changes. Do not use it as a liveness check.
- Compare the values with the previous state before you refresh the UI; only changes are delivered.

## Data types

The parcelables are plain Java classes with public final fields and a constructor with all fields.

### ClipPayload

| Field | Type | Meaning |
|-------|------|---------|
| `text` | `String` | Plain text, null for binary content |
| `html` | `String` | HTML of the same copy, null when there is none |
| `mime` | `String` | MIME type, used by `publishBinary` only |
| `size` | `long` | Size in bytes, -1 when unknown; informational, the app ignores it |

### ClipSummary

| Field | Type | Meaning |
|-------|------|---------|
| `id` | `String` | Opaque item id, the argument of `openItem` |
| `deviceName` | `String` | Name of the device the item came from, empty when the device is not in the paired list |
| `kind` | `int` | One of the `KIND_*` constants |
| `preview` | `String` | Short text for the chip, empty for an image |
| `mimeTypes` | `String[]` | Types of the available representations, never null |
| `createdAtMs` | `long` | Capture time on the origin device in Unix milliseconds |

The preview is the text of a text item shortened to 160 characters with whitespace collapsed, and the
names of the first two files for a file item with a count of the rest. An image has an empty preview;
use `kind` and draw an icon.

### BridgeState

| Field | Type | Meaning |
|-------|------|---------|
| `paused` | `boolean` | True while the synchronization is paused |
| `untilMs` | `long` | End of a timed pause in Unix milliseconds, 0 when the pause has no end or there is none |
| `onlinePeers` | `int` | Number of devices that are online now |

Check `paused` before `untilMs`: a `0` means either no pause or a pause without a deadline.

### Kinds, ids and times

| Constant | Value | Content |
|----------|-------|---------|
| `BridgeContract.KIND_UNSPECIFIED` | 0 | Unknown, draw a generic icon |
| `BridgeContract.KIND_TEXT` | 1 | Text and optionally HTML |
| `BridgeContract.KIND_IMAGE` | 2 | An image, normally `image/png` |
| `BridgeContract.KIND_FILES` | 3 | One or more files, one representation per file |

Ids are opaque strings. Do not parse them, do not sort by them, do not build a file name from them.
They are stable for the lifetime of the item and are only valid for `openItem` and for comparisons
within the keyboard.

Times are Unix milliseconds taken on the origin device. Two phones rarely agree on the second, so
show a relative age rather than an exact clock time, and tolerate a time in the near future.

## Capturing the local clipboard

The keyboard is the only part of the system that can read the clipboard in the background and hand
it to the app, so the capture rules below are what keeps passwords out of the synchronization.

### When to read

Read only while an editor is attached to the keyboard and the clipboard changed:

```kotlin
override fun onPrimaryClipChanged() {
    main.removeCallbacks(capture)
    main.postDelayed(capture, DEBOUNCE_MS)
}
```

The debounce is not optional. Apps write the clipboard several times in a row, first the plain text
and then the HTML, and some write a placeholder before the real content. 200 to 300 milliseconds
after the last change, read the clipboard once and publish once.

### What to skip

Skip the whole capture when any of these is true:

- `primaryClipDescription.extras` carries `android.content.extra.IS_SENSITIVE` set to true. The
  constant `ClipDescription.EXTRA_IS_SENSITIVE` exists from API 33, the string works from API 29 and
  is what the system writes.
- The focused editor is a password field: `TYPE_TEXT_VARIATION_PASSWORD`,
  `TYPE_TEXT_VARIATION_VISIBLE_PASSWORD`, `TYPE_TEXT_VARIATION_WEB_PASSWORD` or
  `TYPE_TEXT_VARIATION_NUMBER_PASSWORD`.
- The focused editor is not a text editor, for example `TYPE_NULL`, or `currentInputEditorInfo` is
  null, which means no editor is attached.
- The content is empty, or it is the content that this keyboard just committed from an incoming
  item.

```kotlin
private fun isTextEditor(inputType: Int): Boolean {
    val cls = inputType and InputType.TYPE_MASK_CLASS
    return cls == InputType.TYPE_CLASS_TEXT || cls == InputType.TYPE_CLASS_NUMBER
}

private fun isPasswordField(inputType: Int): Boolean = when (inputType and InputType.TYPE_MASK_VARIATION) {
    InputType.TYPE_TEXT_VARIATION_PASSWORD,
    InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
    InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
    InputType.TYPE_TEXT_VARIATION_NUMBER_PASSWORD -> true

    else -> false
}
```

The check runs on the state of the editor at the moment of the read, not at the moment of the copy.
A password manager that copies a password while the editor is a password field is therefore caught,
and a copy in a search field is not.

### Text

Read `item.text` and `item.html`. When there is no plain text but there is HTML, derive the plain text
from the HTML, because the app publishes each of the two parts separately and a text item without
`text/plain` has no preview:

```kotlin
val text = item.text?.toString()
    ?: item.htmlText?.let { Html.fromHtml(it, Html.FROM_HTML_MODE_LEGACY).toString() }
```

Then call `publishText(text, html)`.

### Images and URIs

A clip without text can carry a `content://` URI, which is how other apps copy an image. The
keyboard holds the temporary read grant of the clipboard, so it can open the stream itself and hand
the bytes over:

```kotlin
val mime = contentResolver.getType(uri) ?: return
if (!mime.startsWith("image/")) {
    return  // the bridge carries text, HTML and images only
}
contentResolver.openInputStream(uri)?.use { stream ->
    val bytes = stream.readBytes()
    if (bytes.size <= MAX_BINARY_BYTES) {
        client.publishBinary(mime, bytes)
    }
}
```

Do not pass the URI to the app. The grant belongs to the keyboard and cannot be handed to another
process, which is exactly why the contract takes a pipe.

### Rate limits and echo

- Publish at most one item per clipboard change. Duplicate changes with the same content are wasted
  work; remember the hash of the last content and skip it within a few seconds.
- Never publish content that arrived from Unison. The app writes an applied item into the system
  clipboard, which fires `onPrimaryClipChanged` again; without suppression the item travels back to
  the device it came from. Keep the digest of the text you commit from a chip for about ten seconds
  and skip it, and skip the preview of a summary the same way.
- Never publish while `BridgeState.paused` is true. The app drops it anyway, and the keyboard avoids
  the binder call and the clipboard read.
- Do not replay the clipboard in a loop. A peer accepts five clipboard announcements per second with
  a burst of ten, and a stream above that is treated as a protocol violation by the receiving
  device. A keyboard publishes one item per user copy, which is far below the limit.

## Showing incoming items

### The chip

Draw one chip per item in the suggestion strip of the keyboard, from `recentRemoteItems(3)` when the
keyboard becomes active and from `onRemoteItem` afterwards. Keep the list short and deduplicated by
id: the same item is delivered more than once, and the most recent item is the one the user wants.

A chip shows `preview` for text and the name of the device from `deviceName`. For
`KIND_IMAGE` and `KIND_FILES` the preview is short or empty, so draw an icon and put the device name
in the label. A `deviceName` that is empty means the device is not in the paired list; show a neutral
label rather than an empty one.

### Committing text

```kotlin
client.openItem(item, "text/plain") { bytes ->
    val text = bytes?.toString(Charsets.UTF_8)
    if (text.isNullOrEmpty()) {
        return@openItem  // expired, or the download failed
    }
    suppress(text)
    currentInputConnection?.commitText(text, 1)
}
```

`commitText` inserts the text at the cursor and is what the user expects from a chip. Do not send the
text as a key event: it would break for long text, newlines and multilingual input.

### Committing images and files

An image or a file has to reach the editor as a content URI of the keyboard, because the app cannot
share its own. Copy the bytes into the cache of the keyboard, expose them through a `FileProvider`,
and use `commitContent`:

```xml
<provider
    android:name="androidx.core.content.FileProvider"
    android:authorities="${applicationId}.files"
    android:exported="false"
    android:grantUriPermissions="true">
    <meta-data
        android:name="android.support.FILE_PROVIDER_PATHS"
        android:resource="@xml/file_provider_paths" />
</provider>
```

```xml
<paths>
    <cache-path name="incoming" path="incoming/" />
</paths>
```

```kotlin
private fun commitContent(item: ClipSummary, mime: String, bytes: ByteArray) {
    val connection = currentInputConnection ?: return
    val info = currentInputEditorInfo ?: return
    if (info.contentMimeTypes?.any { mimeMatches(it, mime) } != true) {
        return  // the editor does not take content of this type
    }
    val uri = storeForCommit(item, mime, bytes) ?: return
    val description = ClipDescription(mime, arrayOf(mime))
    try {
        connection.commitContent(
            InputContentInfo(uri, description, null),
            InputConnection.INPUT_CONTENT_GRANT_READ_URI_PERMISSION,
            null,
        )
    } catch (e: Exception) {
        Log.w(TAG, "the editor did not take the item: ${e.message}")
    }
}
```

`EditorInfo.contentMimeTypes` is the list the editor accepts, with wildcards such as `image/*`. An
editor that does not list anything does not take content: keep the chip and do nothing. For a
`KIND_FILES` item, commit one file per chip: `mimeTypes` lists the types of the representations, and
`openItem` with a specific type returns that file.

The file in the cache stays until the editor reads it, so clean the directory on start and delete
files older than a day.

### Missing and expired items

The history of Unison has a lifetime: seven days for text, three days for images and one day for
files by default. Past it, the item disappears.

- `openItem` gives a descriptor with no bytes. Drop the chip, and read `recentRemoteItems` again to
  pick up the current list.
- The same happens when an item is metadata only and no device with the content is online. The chip
  stays valid, so do not delete it on the first failed read; retry when a device comes online, which
  is reported by `onStateChanged`.
- Close every descriptor in a `finally`. A descriptor that is never closed leaks a file descriptor in
  your process and keeps the pipe in the app until the timeout.

## Pause and device state

`BridgeState` is the only source of the state of the synchronization:

| Situation | What the keyboard sees | What to do |
|-----------|------------------------|------------|
| Unison not installed | `bindService` returns false | Keep the bridge off and retry on a package broadcast |
| Unison installed, nothing paired | Bridge works, publishes are recorded locally, no item arrives | Nothing; an item waits in the history |
| No device online | `onlinePeers` is 0, publishes are accepted | Nothing; the item reaches the device when it connects |
| Paused | `paused` is true, publishes are dropped | Skip the capture, show the pause in the UI |
| Core stopped | `onlinePeers` is 0, `recentRemoteItems` is empty | Publish once or set a pause; the app starts its core on demand |
| Refused | `SecurityException` | The signatures differ, see the requirements |

Publishing with no device online is not an error: the item is recorded in the history of the phone
and is synchronized when a Personal device connects, within the lifetime of the item. The desktop
clipboard is not touched on the way, only the history travels.

## Privacy

- The bridge carries content, not metadata about a person. Do not log the text, the file names or the
  preview: an id, a kind and a size are enough for a debug line.
- The keyboard is the trusted peer of the app. It reads the clipboard in the foreground and decides
  what to publish; the signature permission and the caller check only make sure that no other app
  takes its place.
- The app checks the signature of the caller by uid on every call, not only at bind time, because a
  package can be replaced while a binding is alive.
- Do not keep a copy of an item after the user committed it. Write the bytes for `commitContent` into
  the cache, let the editor read them, and let the cleanup remove them. Do not copy an incoming item
  into the database, the preferences or the backup of the keyboard.
- Never read the clipboard when the keyboard is not on screen. Android refuses it in the background
  anyway, and a retry loop only drains the battery.

## Complete example

Two files: the connection wrapper and the input method that uses it. The wrapper is the same for any
keyboard and can be copied as is.

### The connection wrapper

```kotlin
package com.example.keyboard.bridge

import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.ServiceConnection
import android.content.pm.PackageManager
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.os.RemoteException
import android.util.Log
import app.unison.bridge.BridgeContract
import app.unison.bridge.BridgeState
import app.unison.bridge.ClipPayload
import app.unison.bridge.ClipSummary
import app.unison.bridge.IUnisonBridge
import app.unison.bridge.IUnisonBridgeListener
import java.io.IOException
import java.util.concurrent.ExecutorService
import java.util.concurrent.Executors

/**
 * Connection to the Unison bridge service.
 *
 * All callbacks are delivered on the main thread.
 */
class UnisonBridgeClient(private val context: Context) {

    /** Called with every item that arrives from another device. */
    var onItem: ((ClipSummary) -> Unit)? = null

    /** Called with the pause and the number of online devices. */
    var onState: ((BridgeState) -> Unit)? = null

    /** Called when the connection is ready, after the version check. */
    var onReady: (() -> Unit)? = null

    private val main = Handler(Looper.getMainLooper())
    private val io: ExecutorService = Executors.newSingleThreadExecutor()

    private var bridge: IUnisonBridge? = null
    private var bound = false

    private val listener = object : IUnisonBridgeListener.Stub() {
        override fun onRemoteItem(summary: ClipSummary?) {
            summary ?: return
            main.post { onItem?.invoke(summary) }
        }

        override fun onStateChanged(state: BridgeState?) {
            state ?: return
            main.post { onState?.invoke(state) }
        }
    }

    private val connection = object : ServiceConnection {
        override fun onServiceConnected(name: ComponentName?, binder: IBinder?) {
            val service = binder?.let { IUnisonBridge.Stub.asInterface(it) } ?: return
            val version = try {
                service.protocolVersion()
            } catch (e: RemoteException) {
                Log.w(TAG, "version check failed: ${e.message}")
                return
            } catch (e: SecurityException) {
                Log.w(TAG, "Unison refuses this app: ${e.message}")
                return
            }
            if (version < BridgeContract.PROTOCOL_VERSION) {
                Log.w(TAG, "Unison serves $version, this client needs ${BridgeContract.PROTOCOL_VERSION}")
                return
            }
            try {
                service.registerListener(listener)
            } catch (e: RemoteException) {
                Log.w(TAG, "registration failed: ${e.message}")
                return
            }
            bridge = service
            onReady?.invoke()
        }

        override fun onServiceDisconnected(name: ComponentName?) {
            bridge = null
        }

        override fun onBindingDied(name: ComponentName?) {
            unbind()
            bind()
        }

        override fun onNullBinding(name: ComponentName?) {
            unbind()
        }
    }

    /** Binds to Unison when it is installed; returns false otherwise. */
    fun bind(): Boolean {
        if (bound) {
            return true
        }
        if (!installed()) {
            return false
        }
        val intent = Intent(BridgeContract.ACTION_BIND)
            .setComponent(ComponentName(BridgeContract.PACKAGE, BridgeContract.SERVICE_CLASS))
        bound = try {
            context.bindService(intent, connection, Context.BIND_AUTO_CREATE)
        } catch (e: SecurityException) {
            Log.w(TAG, "binding is not allowed: ${e.message}")
            false
        }
        return bound
    }

    fun unbind() {
        if (!bound) {
            return
        }
        bound = false
        bridge?.let { service ->
            try {
                service.unregisterListener(listener)
            } catch (e: RemoteException) {
                Log.w(TAG, "unregistration failed: ${e.message}")
            }
        }
        bridge = null
        try {
            context.unbindService(connection)
        } catch (e: IllegalArgumentException) {
            Log.w(TAG, "the binding is already gone")
        }
    }

    fun publishText(text: String, html: String? = null) {
        val service = bridge ?: return
        val size = text.toByteArray().size.toLong()
        try {
            service.publishText(ClipPayload(text, html, "text/plain", size))
        } catch (e: RemoteException) {
            bridge = null
        }
    }

    /** Streams the bytes through a pipe; the write runs on the io thread. */
    fun publishBinary(mime: String, bytes: ByteArray) {
        val service = bridge ?: return
        val pipe = ParcelFileDescriptor.createPipe()
        io.execute {
            try {
                ParcelFileDescriptor.AutoCloseOutputStream(pipe[1]).use { it.write(bytes) }
            } catch (e: IOException) {
                Log.w(TAG, "writing the content failed: ${e.message}")
            }
        }
        try {
            service.publishBinary(ClipPayload(null, null, mime, bytes.size.toLong()), pipe[0])
        } catch (e: RemoteException) {
            bridge = null
        } finally {
            closeQuietly(pipe[0])
        }
    }

    /** Reads the content of an item and delivers it on the main thread. */
    fun openItem(item: ClipSummary, mime: String, onContent: (ByteArray?) -> Unit) {
        val service = bridge
        if (service == null) {
            onContent(null)
            return
        }
        io.execute {
            val content = try {
                val descriptor = service.openItem(item.id, mime)
                if (descriptor == null) {
                    null
                } else {
                    ParcelFileDescriptor.AutoCloseInputStream(descriptor).use { it.readBytes() }
                }
            } catch (e: IOException) {
                Log.w(TAG, "reading item ${item.id} failed: ${e.message}")
                null
            } catch (e: RemoteException) {
                null
            }
            main.post { onContent(content) }
        }
    }

    fun recentItems(limit: Int): List<ClipSummary> = try {
        bridge?.recentRemoteItems(limit).orEmpty()
    } catch (e: RemoteException) {
        emptyList()
    }

    fun setPaused(untilEpochMs: Long) {
        val service = bridge ?: return
        try {
            service.setPaused(untilEpochMs)
        } catch (e: RemoteException) {
            bridge = null
        }
    }

    private fun installed(): Boolean = try {
        context.packageManager.getPackageInfo(BridgeContract.PACKAGE, 0)
        true
    } catch (e: PackageManager.NameNotFoundException) {
        false
    }

    private fun closeQuietly(descriptor: ParcelFileDescriptor) {
        try {
            descriptor.close()
        } catch (e: IOException) {
            Log.w(TAG, "closing the pipe failed: ${e.message}")
        }
    }

    private companion object {
        private const val TAG = "UnisonBridgeClient"
    }
}
```

### The input method

```kotlin
package com.example.keyboard

import android.content.BroadcastReceiver
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.text.Html
import android.text.InputType
import android.util.Log
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputContentInfo
import android.view.inputmethod.InputMethodService
import androidx.core.content.FileProvider
import app.unison.bridge.BridgeContract
import app.unison.bridge.ClipSummary
import com.example.keyboard.bridge.UnisonBridgeClient
import java.io.File
import java.io.IOException
import java.security.MessageDigest

class MyKeyboardService : InputMethodService(), ClipboardManager.OnPrimaryClipChangedListener {

    private lateinit var client: UnisonBridgeClient
    private lateinit var clipboard: ClipboardManager
    private val main = Handler(Looper.getMainLooper())
    private val suppressed = LinkedHashMap<String, Long>()
    private val chips = mutableListOf<ClipSummary>()
    private var paused = false

    private val capture = Runnable { captureClipboard() }

    private val packages = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            if (intent?.data?.schemeSpecificPart != BridgeContract.PACKAGE) {
                return
            }
            if (client.bind()) {
                showRecentChips()
            }
        }
    }

    override fun onCreate() {
        super.onCreate()
        client = UnisonBridgeClient(this).apply {
            onReady = { showRecentChips() }
            onItem = { showChip(it) }
            onState = { state -> paused = state.paused }
        }
        client.bind()
        clipboard = getSystemService(ClipboardManager::class.java)
        clipboard.addPrimaryClipChangedListener(this)
        watchPackages()
    }

    override fun onFinishInput() {
        main.removeCallbacks(capture)
        super.onFinishInput()
    }

    override fun onDestroy() {
        main.removeCallbacks(capture)
        clipboard.removePrimaryClipChangedListener(this)
        unregisterReceiver(packages)
        client.unbind()
        super.onDestroy()
    }

    // Capture.

    override fun onPrimaryClipChanged() {
        main.removeCallbacks(capture)
        main.postDelayed(capture, DEBOUNCE_MS)
    }

    private fun captureClipboard() {
        if (paused) {
            return
        }
        val info = currentInputEditorInfo ?: return
        if (!isTextEditor(info.inputType) || isPasswordField(info.inputType)) {
            return
        }
        val description = clipboard.primaryClipDescription ?: return
        if (description.extras?.getBoolean(EXTRA_IS_SENSITIVE, false) == true) {
            return
        }
        val clip = clipboard.primaryClip ?: return
        if (clip.itemCount == 0) {
            return
        }

        val item = clip.getItemAt(0)
        val html = item.htmlText
        val text = item.text?.toString()
            ?: html?.let { Html.fromHtml(it, Html.FROM_HTML_MODE_LEGACY).toString() }
        if (!text.isNullOrEmpty()) {
            if (isSuppressed(text)) {
                return
            }
            client.publishText(text, html)
            return
        }

        val uri = item.uri ?: return
        publishUri(uri)
    }

    private fun publishUri(uri: Uri) {
        val mime = contentResolver.getType(uri) ?: return
        if (!mime.startsWith("image/")) {
            return
        }
        try {
            contentResolver.openInputStream(uri)?.use { stream ->
                val bytes = stream.readBytes()
                if (bytes.size <= MAX_BINARY_BYTES) {
                    client.publishBinary(mime, bytes)
                }
            }
        } catch (e: IOException) {
            Log.w(TAG, "reading ${uri.scheme} content failed: ${e.message}")
        }
    }

    private fun isTextEditor(inputType: Int): Boolean {
        val cls = inputType and InputType.TYPE_MASK_CLASS
        return cls == InputType.TYPE_CLASS_TEXT || cls == InputType.TYPE_CLASS_NUMBER
    }

    private fun isPasswordField(inputType: Int): Boolean = when (inputType and InputType.TYPE_MASK_VARIATION) {
        InputType.TYPE_TEXT_VARIATION_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_VISIBLE_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_WEB_PASSWORD,
        InputType.TYPE_TEXT_VARIATION_NUMBER_PASSWORD -> true

        else -> false
    }

    private fun isSuppressed(text: String): Boolean = synchronized(suppressed) {
        val now = SystemClock.elapsedRealtime()
        suppressed.entries.removeAll { now - it.value > SUPPRESS_MS }
        suppressed.containsKey(digest(text))
    }

    private fun suppress(text: String) {
        synchronized(suppressed) {
            suppressed[digest(text)] = SystemClock.elapsedRealtime()
        }
    }

    private fun digest(text: String): String =
        MessageDigest.getInstance("SHA-256")
            .digest(text.toByteArray())
            .joinToString("") { "%02x".format(it) }

    // Incoming items.

    private fun showRecentChips() {
        chips.clear()
        chips.addAll(client.recentItems(CHIP_LIMIT))
        renderChips()
    }

    private fun showChip(item: ClipSummary) {
        chips.removeAll { it.id == item.id }
        chips.add(0, item)
        while (chips.size > CHIP_LIMIT) {
            chips.removeAt(chips.lastIndex)
        }
        renderChips()
    }

    /** Draws the chips in the suggestion strip of the keyboard. */
    private fun renderChips() {
        // One chip per item: item.preview and item.deviceName, or an icon for
        // KIND_IMAGE and KIND_FILES.
    }

    private fun commit(item: ClipSummary) {
        val mime = preferredMime(item)
        if (item.kind == BridgeContract.KIND_TEXT) {
            client.openItem(item, mime) { bytes ->
                val text = bytes?.toString(Charsets.UTF_8)
                if (text.isNullOrEmpty()) {
                    return@openItem
                }
                suppress(text)
                currentInputConnection?.commitText(text, 1)
            }
            return
        }
        client.openItem(item, mime) { bytes ->
            if (bytes == null || bytes.isEmpty()) {
                return@openItem
            }
            commitContent(item, mime, bytes)
        }
    }

    private fun preferredMime(item: ClipSummary): String {
        val preferred = if (item.kind == BridgeContract.KIND_TEXT) "text/plain" else "image/png"
        return item.mimeTypes.firstOrNull { it == preferred }
            ?: item.mimeTypes.firstOrNull().orEmpty()
    }

    private fun commitContent(item: ClipSummary, mime: String, bytes: ByteArray) {
        val connection: InputConnection = currentInputConnection ?: return
        val info = currentInputEditorInfo ?: return
        if (info.contentMimeTypes?.any { mimeMatches(it, mime) } != true) {
            return
        }
        val uri = storeForCommit(item, bytes) ?: return
        val description = ClipDescription(mime, arrayOf(mime))
        try {
            connection.commitContent(
                InputContentInfo(uri, description, null),
                InputConnection.INPUT_CONTENT_GRANT_READ_URI_PERMISSION,
                null,
            )
        } catch (e: Exception) {
            Log.w(TAG, "the editor did not take ${item.kind}: ${e.message}")
        }
    }

    private fun storeForCommit(item: ClipSummary, bytes: ByteArray): Uri? = try {
        val dir = File(cacheDir, "incoming").apply { mkdirs() }
        val file = File(dir, "${item.id}-${SystemClock.elapsedRealtime()}")
        file.writeBytes(bytes)
        FileProvider.getUriForFile(this, "$packageName.files", file)
    } catch (e: IOException) {
        Log.w(TAG, "storing item ${item.id} failed: ${e.message}")
        null
    }

    private fun mimeMatches(pattern: String, mime: String): Boolean {
        if (pattern == "*/*" || pattern == mime) {
            return true
        }
        return pattern.endsWith("/*") && mime.startsWith(pattern.dropLast(1))
    }

    private fun watchPackages() {
        val filter = IntentFilter().apply {
            addAction(Intent.ACTION_PACKAGE_ADDED)
            addAction(Intent.ACTION_PACKAGE_REPLACED)
            addDataScheme("package")
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(packages, filter, Context.RECEIVER_EXPORTED)
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            registerReceiver(packages, filter)
        }
    }

    private companion object {
        private const val TAG = "KeyboardBridge"
        private const val EXTRA_IS_SENSITIVE = "android.content.extra.IS_SENSITIVE"
        private const val DEBOUNCE_MS = 250L
        private const val SUPPRESS_MS = 10_000L
        private const val CHIP_LIMIT = 3
        private const val MAX_BINARY_BYTES = 16 * 1024 * 1024
    }
}
```

The pause of the keyboard, for example on a long press of the chip row:

```kotlin
client.setPaused(System.currentTimeMillis() + 5 * 60_000L)  // five minutes
client.setPaused(0L)                                         // resume
client.setPaused(-1L)                                        // until resumed
```

## Testing

### Building and installing both apps

Both APKs have to be signed with the same certificate, which is automatic when both are built on the
same machine with the default debug keystore.

```sh
# Unison, from its checkout.
just android-build
adb install -r android/app/build/outputs/apk/debug/app-debug.apk

# The keyboard, from its repository.
./gradlew installDebug
```

```sh
# Both signers have to print the same digest.
apksigner verify --print-certs android/app/build/outputs/apk/debug/app-debug.apk
apksigner verify --print-certs app/build/outputs/apk/debug/app-debug.apk
```

Make the keyboard the active input method:

```sh
adb shell ime list -s
adb shell ime enable com.example.keyboard/.MyKeyboardService
adb shell ime set com.example.keyboard/.MyKeyboardService
```

### On an emulator

1. Install both APKs and start Unison. Complete the first run screen so its core runs.
2. Make the keyboard the active input method and open any app with a text field.
3. Copy a text in another app, switch back to the field and wait a moment. Check the logcat of the
   app for a submit line and the history screen of Unison for the item.
4. Pair a second emulator or a desktop, wait until the device shows as online, and copy a text on the
   other device. A chip has to appear in the strip within a second or two on the local network.
5. Tap the chip. The text is inserted at the cursor. Then copy a different text and confirm that it is
   published; the text you committed from the chip must never travel back.
6. Turn the pause on from the quick settings tile of Unison. The keyboard has to see `paused` and stop
   publishing; a copy on the other device arrives in the history but no chip appears.

The clipboard of the emulator also follows the clipboard of the host when clipboard sharing is on in
the extended controls, which is a quick way to produce a copy on the emulator side.

### On a phone

The two scenarios that matter:

- **Phone to desktop.** Copy a text in any app of the phone, then paste it on the desktop within a
  second or two. Repeat with a screenshot from the gallery and with a password field of a banking
  app: the text has to arrive, the image has to arrive as an image, and the password must never
  leave the phone.
- **Desktop to phone.** Copy a text on the desktop and open a text field on the phone. The chip
  appears in the strip with the name of the desktop; a tap inserts the text at the cursor. Repeat
  with an image (the editor has to accept content, for example a chat app) and with a file.

Check the following on the phone as well: the chip survives a restart of the keyboard, the pause set
in Unison is visible to the keyboard within half a minute, and a reinstall of Unison is followed by a
working bridge without a restart of the keyboard.

### Troubleshooting

| Symptom | Likely cause | What to do |
|---------|--------------|------------|
| `bindService` returns false | Unison is not installed, or the `<queries>` element is missing | Install Unison, add the package to `<queries>`, retry on a package broadcast |
| `SecurityException` from `bindService` | The permission is not granted: another signing key or a revoked permission | Compare the signer digests, reinstall both with the same key |
| `SecurityException` from a call | The app refuses the uid, which means the signatures differ | Same check as above; a package replaced under a live binding has to be rebound |
| `onServiceConnected` is never called | The binding was not made, or the component name is wrong | Use `ComponentName(BridgeContract.PACKAGE, BridgeContract.SERVICE_CLASS)` and `BIND_AUTO_CREATE` |
| No callbacks at all | No listener registered, or it was registered before the app was ready | Register in `onServiceConnected` after the version check, and register again on every reconnect |
| Callbacks stop after an update of Unison | The old binder is dead | Rebind in `onBindingDied`, and register the listener again |
| A clipboard change is not delivered | The clip is sensitive, the editor is a password field, the pause is on, or the keyboard was not focused when the change happened | Check the logcat of the keyboard and the pause state of the app |
| The app logs a failed submit | The core is not running, or the pause is on | Bring Unison to the foreground once, resume the pause |
| The keyboard shows zero online devices | The core of the app is not running, or no Personal device is online | Publish once or set a pause to start the core; check the device list of Unison |
| A chip opens nothing | The item expired, or it is metadata only and no device with the content is online | Drop or keep the chip, retry when a device comes online |
| `commitContent` does nothing on a phone | The editor does not accept content of that type | Check `EditorInfo.contentMimeTypes`; when it is null, the editor takes no content |
| `TransactionTooLargeException` on publish | The text and HTML are larger than one binder transaction | Publish the text as `text/plain` through `publishBinary` |
| An image never arrives | The type is not `image/*`, or the image is above the limit | Send `meta.mime` as a real image type and keep the bytes under 16 MiB |

The bridge logs of the app use the tags `UnisonBridge`, `BridgeContent` and `CallerVerifier`. They
carry sizes, kinds, ids and error messages, never content:

```sh
adb logcat -s UnisonBridge BridgeContent CallerVerifier
```
