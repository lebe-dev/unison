// Contract between the Unison app and the keyboard.
//
// The interface is frozen: methods are only appended at the end, existing methods are never
// changed or removed. A caller reads protocolVersion() and compares it with
// BridgeContract.PROTOCOL_VERSION before it uses a method of a newer version.

package app.unison.bridge;

import android.os.ParcelFileDescriptor;
import app.unison.bridge.BridgeState;
import app.unison.bridge.ClipPayload;
import app.unison.bridge.ClipSummary;
import app.unison.bridge.IUnisonBridgeListener;

interface IUnisonBridge {
    // Version of the contract served by the app.
    int protocolVersion();

    // Publishes a local clipboard item as text. Binary content goes through publishBinary
    // because a binder transaction is limited to about 1 MB.
    void publishText(in ClipPayload payload);

    // Publishes a local clipboard item whose content is streamed through the pipe. The caller
    // writes the content, then closes the pipe.
    void publishBinary(in ClipPayload meta, in ParcelFileDescriptor data);

    // Subscribes to incoming items and state changes.
    void registerListener(IUnisonBridgeListener listener);

    // Removes a listener registered earlier.
    void unregisterListener(IUnisonBridgeListener listener);

    // Returns at most limit recent items that came from other devices, newest first.
    List<ClipSummary> recentRemoteItems(int limit);

    // Opens the content of an item for the given MIME type; the caller closes the descriptor.
    ParcelFileDescriptor openItem(String itemId, String mime);

    // Sets the pause: untilEpochMs > 0 pauses until that instant, 0 resumes, a negative value
    // pauses until it is resumed.
    void setPaused(long untilEpochMs);
}
