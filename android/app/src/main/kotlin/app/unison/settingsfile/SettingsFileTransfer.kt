package app.unison.settingsfile

import android.content.ContentResolver
import android.content.Context
import android.net.Uri
import app.unison.R
import unison.local.v1.Local
import java.io.ByteArrayOutputStream
import java.io.IOException

/** Reads and writes settings documents through the Android content resolver. */
internal object SettingsFileTransfer {
    /** Largest settings document the app reads, the same bound as the core. */
    private const val MAX_DOCUMENT_BYTES = 512 shl 10

    /** Size of one read of a settings document. */
    private const val READ_CHUNK_BYTES = 8192

    /** Writes [document] to the file the user picked at [uri]. */
    fun writeDocument(
        resolver: ContentResolver,
        uri: Uri,
        document: ByteArray,
    ) {
        val stream =
            resolver.openOutputStream(uri)
                ?: throw IOException("the document cannot be written")
        stream.use { it.write(document) }
    }

    /** Reads the settings document at [uri], failing if it exceeds [MAX_DOCUMENT_BYTES]. */
    fun readDocument(
        resolver: ContentResolver,
        uri: Uri,
    ): ByteArray {
        val stream =
            resolver.openInputStream(uri)
                ?: throw IOException("the document cannot be read")
        return stream.use { input ->
            val chunks = ByteArrayOutputStream()
            val buffer = ByteArray(READ_CHUNK_BYTES)
            while (true) {
                val read = input.read(buffer)
                if (read < 0) {
                    break
                }
                chunks.write(buffer, 0, read)
                if (chunks.size() > MAX_DOCUMENT_BYTES) {
                    throw IOException("the document is too large")
                }
            }
            chunks.toByteArray()
        }
    }

    /** Builds the user-facing summary of what an import applied and skipped. */
    fun importNotice(
        context: Context,
        response: Local.ImportSettingsResponse,
    ): String =
        buildList {
            add(context.getString(R.string.settings_file_imported))
            if (response.fromNewerVersion) {
                add(context.getString(R.string.settings_file_newer))
            }
            if (response.ignoredCount > 0) {
                add(context.getString(R.string.settings_file_ignored, response.ignoredList.joinToString(", ")))
            }
        }.joinToString(" ")
}
