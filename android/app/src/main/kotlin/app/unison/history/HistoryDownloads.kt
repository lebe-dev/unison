package app.unison.history

import android.content.Context
import androidx.core.app.NotificationManagerCompat
import app.unison.core.CoreClient
import app.unison.storage.Downloads
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.UUID

/** Saves the payloads of a history entry into the device storage. */
fun interface HistorySaver {
    /** Returns the number of saved files and throws when nothing could be saved. */
    suspend fun save(id: String): Int
}

/**
 * Downloads the payloads of an entry through the core and copies them into the
 * Downloads collection. The core writes them into a temporary directory first,
 * because it writes plain files only.
 */
class HistoryDownloads(
    private val context: Context,
    private val client: CoreClient = CoreClient(),
) : HistorySaver {
    override suspend fun save(id: String): Int =
        withContext(Dispatchers.IO) {
            val staging = File(context.cacheDir, "$STAGING_DIR/${UUID.randomUUID()}")
            if (!staging.mkdirs()) {
                throw IllegalStateException("cannot create the staging directory")
            }
            try {
                val response = client.fetchHistoryItem(id, staging.absolutePath)
                val files = response.filesList.map(::File).filter { it.isFile }
                if (files.isEmpty()) {
                    throw IllegalStateException("the item has no payload to write")
                }
                val saved = files.count { copy(it) }
                // The entry is on the device now, the download offer is gone.
                NotificationManagerCompat.from(context).cancel(HistoryNotifications.idOf(id))
                saved
            } finally {
                staging.deleteRecursively()
            }
        }

    /** Copies one payload into the Downloads collection. */
    private fun copy(file: File): Boolean = Downloads.save(context, file, file.name, Downloads.mimeTypeOf(file.name))

    private companion object {
        const val STAGING_DIR = "history"
    }
}
