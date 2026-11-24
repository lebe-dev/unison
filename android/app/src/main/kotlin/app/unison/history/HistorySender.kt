package app.unison.history

import android.content.Context
import app.unison.core.CoreApi
import app.unison.core.CoreClient
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import java.io.File
import java.util.UUID

/** Offers the payloads of a history entry to one paired device. */
fun interface HistorySender {
    /** Stages the payloads and starts the transfer, throws when it fails. */
    suspend fun send(
        id: String,
        deviceId: String,
    )
}

/**
 * Sends an entry as files. The core writes the payloads into the cache first
 * and reads them while the transfer runs, so the staged files stay until the
 * cleanup removes them.
 */
class HistoryOutbox(
    private val root: File,
    private val client: CoreApi = CoreClient(),
    private val clock: () -> Long = System::currentTimeMillis,
) : HistorySender {
    constructor(
        context: Context,
        client: CoreApi = CoreClient(),
    ) : this(File(context.cacheDir, OUTBOX_DIR), client)

    override suspend fun send(
        id: String,
        deviceId: String,
    ) {
        withContext(Dispatchers.IO) {
            if (id.isEmpty() || deviceId.isEmpty()) {
                error("the item and the device are required")
            }
            val staging = File(root, UUID.randomUUID().toString())
            if (!staging.mkdirs()) {
                error("cannot create the outbox directory")
            }
            var sent = false
            try {
                val response = client.fetchHistoryItem(id, staging.absolutePath)
                val paths = response.filesList.filter { File(it).isFile }
                if (paths.isEmpty()) {
                    error("the item has no payload to send")
                }
                client.sendFiles(paths, listOf(deviceId))
                sent = true
            } finally {
                if (!sent) {
                    staging.deleteRecursively()
                }
            }
        }
    }

    /** Removes staged files that no transfer needs any more. */
    fun clearExpired(now: Long = clock()) {
        val deadline = now - MAX_AGE_MS
        root.listFiles()?.forEach { entry ->
            if (entry.lastModified() < deadline) {
                entry.deleteRecursively()
            }
        }
    }

    private companion object {
        const val OUTBOX_DIR = "outbox"

        /** A transfer may resume for a while, older files are never read again. */
        const val MAX_AGE_MS = 24L * 60 * 60 * 1000
    }
}
