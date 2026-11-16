# Keyboard bridge

The Android app serves an interface for keyboard apps. A keyboard that is signed with the same key
can hand the clipboard over without leaving the keyboard, and it can show the items that arrive from
the other devices as a chip in its suggestion strip.

The interface is a bound service in the package `app.unison`, protected by the signature permission
`app.unison.permission.BRIDGE`. The keyboard is a separate application: Unison works without it, and
the bridge stays inactive while no such keyboard is installed.

## What it enables

- A copy on the phone reaches the clipboard of the paired computer within a second on the local
  network. Text, HTML and images travel this way.
- An item copied on another device appears as a chip in the suggestion strip of the keyboard. A tap
  inserts the text at the cursor, or hands an image or a file to the editor.
- The keyboard sees the pause of the synchronization and can change it.
- The keyboard learns what happened to a publish: accepted by the core, refused by the pause, refused
  because the core is unavailable or refused because the type is not carried. The result methods
  answer every publish.

## What it needs

- Both applications signed with the same certificate, because the permission is signature level. A
  keyboard with another key cannot bind.
- At least one paired Personal device. Without one, an item stays in the history until a device
  connects.
- No file leaves the phone through the bridge: text and images are published, a file can only be
  received and opened. A publish of another type is refused and reported.
- The app serves contract version 2. The base methods work with version 1 too; the publish results
  need `BridgeContract.PUBLISH_RESULTS_VERSION`.

## For the keyboard developer

The artifact is published as `app.unison:bridge-aidl` with `just bridge-publish`, which writes it to
the local Maven repository. The interface, the binding lifecycle, the rules for capturing the
clipboard, a complete example and the troubleshooting table are described in the
[integration guide](ANDROID-AIDL-INTEGRATION.md).
