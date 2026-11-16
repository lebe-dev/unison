package app.unison.bridge

import java.io.ByteArrayOutputStream
import java.io.InputStream

/** Reads content of a bounded size from a stream. */
object BridgeBinary {

    /** Content of the stream, or null when it is larger than limit. */
    fun read(input: InputStream, limit: Int): ByteArray? {
        val output = ByteArrayOutputStream()
        val chunk = ByteArray(CHUNK_BYTES)
        var total = 0
        while (true) {
            val read = input.read(chunk)
            if (read < 0) {
                return output.toByteArray()
            }
            total += read
            if (total > limit) {
                return null
            }
            output.write(chunk, 0, read)
        }
    }

    private const val CHUNK_BYTES = 64 * 1024
}
