@file:Suppress("NestedBlockDepth", "ThrowsCount", "TooGenericExceptionCaught")

package app.unison.updater

import app.unison.BuildConfig
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.ensureActive
import java.io.File
import java.io.FileOutputStream
import java.io.IOException
import java.io.InputStream
import java.security.MessageDigest
import java.util.Base64
import kotlin.coroutines.cancellation.CancellationException

/** Checks an Ed25519 signature of the release manifest. */
fun interface ReleaseSignatureVerifier {
    fun verify(
        publicKey: ByteArray,
        payload: ByteArray,
        signature: ByteArray,
    ): Boolean
}

/** The step at which a download failed. */
enum class UpdateVerificationError {
    INVALID_KEY,
    SIGNATURE,
    CHECKSUMS,
    CHECKSUM,
    TOO_LARGE,
    DOWNLOAD,
}

/** A download or a verification that failed. */
class UpdateVerificationException(
    val error: UpdateVerificationError,
    message: String? = null,
    cause: Throwable? = null,
) : Exception(message, cause)

/**
 * Downloads the APK of a release and verifies it against the signed manifest
 * before the app hands it to Android.
 */
internal class UpdateVerifier(
    private val workDir: File,
    private val verifier: ReleaseSignatureVerifier,
    private val downloader: ReleaseDownloader,
    private val publicKey: String = BuildConfig.UPDATE_PUBLIC_KEY,
) {
    /**
     * Returns the verified APK of [release], downloading it when the staging
     * directory does not hold it yet.
     */
    suspend fun verify(
        release: ReleaseInfo,
        onProgress: (downloaded: Long, total: Long) -> Unit = { _, _ -> },
    ): File {
        val staged = verifiedFile(release)
        if (staged.isFile && staged.length() > 0) {
            return staged
        }
        val expected = expectedDigest(release)
        return downloadApk(release, expected, onProgress)
    }

    /** The address of the verified APK of [release]. */
    fun verifiedFile(release: ReleaseInfo): File = File(stagingDir(release), release.assetName)

    /** Reads and verifies the manifest, returning the digest of the APK. */
    private fun expectedDigest(release: ReleaseInfo): ByteArray {
        val key = decodeKey()
        val manifest = fetch(release.checksumsUrl, UpdateLimits.MAX_SUMS_BYTES, UpdateVerificationError.CHECKSUMS)
        val signature =
            fetch(release.signatureUrl, UpdateLimits.MAX_SIGNATURE_BYTES, UpdateVerificationError.SIGNATURE)
        val signatureBytes =
            decodeBase64(signature.toString(Charsets.UTF_8))?.takeIf { it.size == SIGNATURE_BYTES }
                ?: throw UpdateVerificationException(UpdateVerificationError.SIGNATURE)
        if (!verifier.verify(key, manifest, signatureBytes)) {
            throw UpdateVerificationException(UpdateVerificationError.SIGNATURE)
        }
        return checksumOf(manifest, release.assetName)
            ?: throw UpdateVerificationException(UpdateVerificationError.CHECKSUMS)
    }

    /** Streams the APK into the staging directory while hashing it. */
    private suspend fun downloadApk(
        release: ReleaseInfo,
        expected: ByteArray,
        onProgress: (Long, Long) -> Unit,
    ): File {
        val directory = stagingDir(release)
        if (!directory.isDirectory && !directory.mkdirs()) {
            throw UpdateVerificationException(UpdateVerificationError.DOWNLOAD, "the staging directory cannot be created")
        }
        val part = File(directory, "${release.assetName}.part")
        val target = File(directory, release.assetName)
        try {
            val digest = MessageDigest.getInstance(DIGEST)
            copyTo(release, part, digest, onProgress)
            if (!MessageDigest.isEqual(digest.digest(), expected)) {
                throw UpdateVerificationException(UpdateVerificationError.CHECKSUM)
            }
            if (!part.renameTo(target)) {
                throw UpdateVerificationException(UpdateVerificationError.DOWNLOAD, "the verified file cannot be moved")
            }
            return target
        } catch (e: CancellationException) {
            part.delete()
            throw e
        } catch (e: UpdateVerificationException) {
            part.delete()
            throw e
        } catch (e: IOException) {
            part.delete()
            throw UpdateVerificationException(UpdateVerificationError.DOWNLOAD, e.message, e)
        } catch (e: Exception) {
            part.delete()
            throw UpdateVerificationException(UpdateVerificationError.DOWNLOAD, e.message, e)
        }
    }

    /** Copies the artifact into [part] and feeds every chunk into [digest]. */
    private suspend fun copyTo(
        release: ReleaseInfo,
        part: File,
        digest: MessageDigest,
        onProgress: (Long, Long) -> Unit,
    ) {
        val download =
            try {
                downloader.open(release.assetUrl)
            } catch (e: UpdateCheckException) {
                throw UpdateVerificationException(UpdateVerificationError.DOWNLOAD, e.message, e)
            }
        download.use { source ->
            val total = source.total(release.assetSize)
            if (total > UpdateLimits.MAX_APK_BYTES) {
                throw UpdateVerificationException(UpdateVerificationError.TOO_LARGE)
            }
            copyStream(source.stream, part, digest, total, onProgress)
        }
    }

    /** Copies [source] into [part], hashing every chunk and reporting progress. */
    private suspend fun copyStream(
        source: InputStream,
        part: File,
        digest: MessageDigest,
        total: Long,
        onProgress: (Long, Long) -> Unit,
    ) {
        FileOutputStream(part).use { output ->
            val buffer = ByteArray(BUFFER_BYTES)
            var downloaded = 0L
            var lastEmit = 0L
            while (true) {
                currentCoroutineContext().ensureActive()
                val read = source.read(buffer)
                if (read < 0) {
                    break
                }
                downloaded += read
                if (downloaded > UpdateLimits.MAX_APK_BYTES) {
                    throw UpdateVerificationException(UpdateVerificationError.TOO_LARGE)
                }
                digest.update(buffer, 0, read)
                output.write(buffer, 0, read)
                val now = System.currentTimeMillis()
                if (now - lastEmit >= PROGRESS_INTERVAL_MS) {
                    lastEmit = now
                    onProgress(downloaded, total)
                }
            }
            output.fd.sync()
            onProgress(downloaded, total)
        }
    }

    /** Reads a small file of the release into memory. */
    private fun fetch(
        url: String,
        limit: Int,
        error: UpdateVerificationError,
    ): ByteArray {
        val download =
            try {
                downloader.open(url)
            } catch (e: UpdateCheckException) {
                throw UpdateVerificationException(error, e.message, e)
            }
        return download.use { readLimitedOrThrow(it.stream, limit, error) }
    }

    /** The staging directory of one release. */
    private fun stagingDir(release: ReleaseInfo): File = File(workDir, release.version)

    /** Decodes the embedded key, refusing a build without a usable one. */
    private fun decodeKey(): ByteArray {
        val key =
            decodeBase64(publicKey)
                ?: throw UpdateVerificationException(UpdateVerificationError.INVALID_KEY)
        if (key.size != KEY_BYTES) {
            throw UpdateVerificationException(UpdateVerificationError.INVALID_KEY)
        }
        return key
    }

    /** The digest of [name] in the manifest, null when the manifest lacks it. */
    private fun checksumOf(
        manifest: ByteArray,
        name: String,
    ): ByteArray? {
        val text = manifest.toString(Charsets.UTF_8)
        for (line in text.lineSequence()) {
            val match = CHECKSUM_LINE.matchEntire(line.trim()) ?: continue
            if (match.groupValues[2] == name) {
                return hexToBytes(match.groupValues[1]) ?: return null
            }
        }
        return null
    }

    private companion object {
        const val DIGEST = "SHA-256"
        const val KEY_BYTES = 32
        const val SIGNATURE_BYTES = 64
        const val BUFFER_BYTES = 32 shl 10
        const val PROGRESS_INTERVAL_MS = 150L
        val CHECKSUM_LINE = Regex("""^([0-9a-fA-F]{64})\s+\*?(.+)$""")
    }
}

/** The whole size of an artifact, or the size the release announced. */
internal fun DownloadStream.total(announced: Long): Long = if (contentLength > 0) contentLength else announced

/** Decodes base64 with or without line breaks, null when it is not base64. */
internal fun decodeBase64(text: String): ByteArray? =
    runCatching {
        Base64.getMimeDecoder().decode(text.trim())
    }.getOrNull()?.takeIf { it.isNotEmpty() }

/** Decodes a hex string, null when it is not hex. */
internal fun hexToBytes(text: String): ByteArray? {
    if (text.length % 2 != 0) {
        return null
    }
    val bytes = ByteArray(text.length / 2)
    for (index in bytes.indices) {
        val high = Character.digit(text[index * 2], 16)
        val low = Character.digit(text[index * 2 + 1], 16)
        if (high < 0 || low < 0) {
            return null
        }
        bytes[index] = ((high shl 4) + low).toByte()
    }
    return bytes
}

/** Reads a small file of the release, failing above [limit]. */
private fun readLimitedOrThrow(
    stream: InputStream,
    limit: Int,
    error: UpdateVerificationError,
): ByteArray =
    try {
        stream.use { readLimited(it, limit) }
    } catch (e: UpdateCheckException) {
        throw UpdateVerificationException(error, e.message, e)
    } catch (e: IOException) {
        throw UpdateVerificationException(error, e.message, e)
    }
