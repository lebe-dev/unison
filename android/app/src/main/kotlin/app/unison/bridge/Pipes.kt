package app.unison.bridge

import android.os.ParcelFileDescriptor
import android.util.Log

/** Closes a descriptor, ignoring a failure to do so. */
internal fun ParcelFileDescriptor.closeQuietly(tag: String) {
    try {
        close()
    } catch (e: Exception) {
        Log.w(tag, "cannot close a pipe: ${e.message}")
    }
}
