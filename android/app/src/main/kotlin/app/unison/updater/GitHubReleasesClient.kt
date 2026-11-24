@file:Suppress("SwallowedException", "ThrowsCount", "TooGenericExceptionCaught")

package app.unison.updater

import android.os.Build
import app.unison.BuildConfig
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.Closeable
import java.io.IOException
import java.io.InputStream
import java.net.HttpURLConnection
import java.net.URL
import javax.net.ssl.HttpsURLConnection

/** A release of GitHub with the addresses the app downloads from. */
data class ReleaseInfo(
    val version: String,
    val tagName: String,
    val name: String,
    val body: String,
    val publishedAt: String,
    val prerelease: Boolean,
    val htmlUrl: String,
    val assetName: String,
    val assetUrl: String,
    val assetSize: Long,
    val checksumsUrl: String,
    val signatureUrl: String,
) {
    /** The release as the settings keep it, so a banner opens without a request. */
    fun toJson(): String =
        JSONObject()
            .put("version", version)
            .put("tag_name", tagName)
            .put("name", name)
            .put("body", body)
            .put("published_at", publishedAt)
            .put("prerelease", prerelease)
            .put("html_url", htmlUrl)
            .put("asset_name", assetName)
            .put("asset_url", assetUrl)
            .put("asset_size", assetSize)
            .put("checksums_url", checksumsUrl)
            .put("signature_url", signatureUrl)
            .toString()

    companion object {
        /** Reads a release the settings kept, null when the text is not one. */
        fun fromJson(text: String): ReleaseInfo? =
            runCatching {
                val json = JSONObject(text)
                ReleaseInfo(
                    version = json.optString("version"),
                    tagName = json.optString("tag_name"),
                    name = json.optString("name"),
                    body = json.optString("body"),
                    publishedAt = json.optString("published_at"),
                    prerelease = json.optBoolean("prerelease"),
                    htmlUrl = json.optString("html_url"),
                    assetName = json.optString("asset_name"),
                    assetUrl = json.optString("asset_url"),
                    assetSize = json.optLong("asset_size"),
                    checksumsUrl = json.optString("checksums_url"),
                    signatureUrl = json.optString("signature_url"),
                )
            }.getOrNull()?.takeIf { it.version.isNotEmpty() && it.assetUrl.isNotEmpty() }
    }
}

/** The answer of one check. */
data class CheckOutcome(
    val notModified: Boolean,
    val release: ReleaseInfo?,
    val etag: String?,
)

/** What went wrong with a check. */
enum class UpdateCheckCode {
    NETWORK,
    RATE_LIMITED,
    STATUS,
    INVALID_RESPONSE,
    INVALID_VERSION,
    UNTRUSTED_URL,
}

/** A failed check with a code the screens translate. */
class UpdateCheckException(
    val code: UpdateCheckCode,
    message: String? = null,
    val retryAfterMs: Long? = null,
) : IOException(message)

/** One answer of the metadata request. */
internal data class UpdateHttpResponse(
    val status: Int,
    val body: ByteArray,
    val etag: String?,
    val rateLimitRemaining: Int?,
    val rateLimitResetMs: Long?,
)

/** The metadata request of the client, replaced in the tests. */
internal fun interface UpdateHttp {
    fun get(
        url: String,
        headers: Map<String, String>,
    ): UpdateHttpResponse
}

/** A verified download source. */
internal class DownloadStream(
    val contentLength: Long,
    val stream: InputStream,
) : Closeable {
    override fun close() = stream.close()
}

/** The download of an artifact, replaced in the tests. */
internal fun interface ReleaseDownloader {
    fun open(url: String): DownloadStream
}

/** Hosts whose HTTPS addresses may carry a release or an artifact. */
internal object UpdateUrls {
    private val HOSTS =
        setOf(
            "github.com",
            "api.github.com",
            "objects.githubusercontent.com",
            "release-assets.githubusercontent.com",
        )

    /** True when [url] is HTTPS on a host of GitHub. */
    fun allowed(url: String): Boolean {
        val parsed = runCatching { URL(url) }.getOrNull() ?: return false
        if (parsed.protocol != "https") {
            return false
        }
        val host = parsed.host.lowercase()
        return host in HOSTS || host.endsWith(".githubusercontent.com")
    }
}

/** Timeouts and limits of the requests. */
internal object UpdateLimits {
    const val CONNECT_TIMEOUT_MS = 15_000
    const val READ_TIMEOUT_MS = 15_000
    const val DOWNLOAD_READ_TIMEOUT_MS = 30_000
    const val MAX_REDIRECTS = 5
    const val MAX_METADATA_BYTES = 4 shl 20
    const val MAX_BODY_BYTES = 128 shl 10
    const val MAX_SUMS_BYTES = 256 shl 10
    const val MAX_SIGNATURE_BYTES = 4 shl 10
    const val MAX_APK_BYTES = 256L shl 20
}

/** HTTPS requests that follow a redirect only inside GitHub. */
internal object HttpsFetch {
    /** Opens a connection to [url], following up to [UpdateLimits.MAX_REDIRECTS] hops. */
    fun connect(
        url: String,
        headers: Map<String, String>,
        readTimeoutMs: Int,
    ): HttpsURLConnection {
        var target = url
        repeat(UpdateLimits.MAX_REDIRECTS + 1) {
            if (!UpdateUrls.allowed(target)) {
                throw UpdateCheckException(UpdateCheckCode.UNTRUSTED_URL, "the address $target is not HTTPS on GitHub")
            }
            val connection = open(target, headers, readTimeoutMs)
            val status = connection.responseCode
            if (status in REDIRECTS) {
                val location = connection.getHeaderField("Location")
                connection.disconnect()
                if (location.isNullOrEmpty()) {
                    throw UpdateCheckException(UpdateCheckCode.STATUS, "GitHub redirected without a location")
                }
                target = URL(URL(target), location).toString()
                return@repeat
            }
            return connection
        }
        throw UpdateCheckException(UpdateCheckCode.STATUS, "GitHub redirected too often")
    }

    private fun open(
        url: String,
        headers: Map<String, String>,
        readTimeoutMs: Int,
    ): HttpsURLConnection {
        val connection =
            runCatching { URL(url).openConnection() as HttpsURLConnection }
                .getOrElse { throw UpdateCheckException(UpdateCheckCode.NETWORK, it.message) }
        connection.instanceFollowRedirects = false
        connection.connectTimeout = UpdateLimits.CONNECT_TIMEOUT_MS
        connection.readTimeout = readTimeoutMs
        headers.forEach { (name, value) -> connection.setRequestProperty(name, value) }
        return connection
    }

    private val REDIRECTS = 300..399
}

/** The metadata request of the production client. */
internal class HttpsUpdateHttp : UpdateHttp {
    override fun get(
        url: String,
        headers: Map<String, String>,
    ): UpdateHttpResponse {
        val connection =
            try {
                HttpsFetch.connect(url, headers, UpdateLimits.READ_TIMEOUT_MS)
            } catch (e: UpdateCheckException) {
                throw e
            } catch (e: IOException) {
                throw UpdateCheckException(UpdateCheckCode.NETWORK, e.message)
            }
        try {
            val status = connection.responseCode
            val body = readBody(connection, status)
            return UpdateHttpResponse(
                status = status,
                body = body,
                etag = connection.getHeaderField("ETag"),
                rateLimitRemaining = connection.getHeaderField("X-RateLimit-Remaining")?.toIntOrNull(),
                rateLimitResetMs =
                    connection
                        .getHeaderField("X-RateLimit-Reset")
                        ?.toLongOrNull()
                        ?.times(1000L),
            )
        } finally {
            connection.disconnect()
        }
    }

    private fun readBody(
        connection: HttpURLConnection,
        status: Int,
    ): ByteArray {
        if (status == HTTP_NOT_MODIFIED) {
            return ByteArray(0)
        }
        val stream = if (status >= 400) connection.errorStream else connection.inputStream
        stream ?: return ByteArray(0)
        return stream.use { readLimited(it, UpdateLimits.MAX_METADATA_BYTES) }
    }

    private companion object {
        const val HTTP_NOT_MODIFIED = 304
    }
}

/** The download of the production client. */
internal class HttpsReleaseDownloader : ReleaseDownloader {
    override fun open(url: String): DownloadStream {
        val connection =
            try {
                HttpsFetch.connect(url, emptyMap(), UpdateLimits.DOWNLOAD_READ_TIMEOUT_MS)
            } catch (e: UpdateCheckException) {
                throw e
            } catch (e: IOException) {
                throw UpdateCheckException(UpdateCheckCode.NETWORK, e.message)
            }
        val status = connection.responseCode
        if (status !in 200..299) {
            connection.disconnect()
            throw UpdateCheckException(UpdateCheckCode.STATUS, "the download answered with the status $status")
        }
        return DownloadStream(
            contentLength = connection.contentLengthLong,
            stream =
                connection.inputStream,
        )
    }
}

/** Reads the releases of Unison from GitHub and picks the newest to offer. */
internal class GitHubReleasesClient(
    private val apiUrl: String = BuildConfig.UPDATE_API_URL,
    private val http: UpdateHttp = HttpsUpdateHttp(),
    private val userAgent: String = defaultUserAgent(),
) {
    /**
     * Reads the releases and returns the newest one that fits the channel and is
     * newer than [currentVersion]. A manual check never sends the cached tag, so
     * the user sees the channel as it is now.
     */
    fun check(
        config: UpdateConfig,
        currentVersion: String,
        manual: Boolean,
    ): CheckOutcome {
        val current =
            SemVer.parse(currentVersion)
                ?: throw UpdateCheckException(UpdateCheckCode.INVALID_VERSION, "the version $currentVersion is not readable")

        val headers =
            buildMap {
                put("Accept", "application/vnd.github+json")
                put("X-GitHub-Api-Version", "2022-11-28")
                put("User-Agent", userAgent)
                if (!manual) {
                    config.lastEtag?.let { put("If-None-Match", it) }
                }
            }

        val response =
            try {
                http.get(apiUrl, headers)
            } catch (e: UpdateCheckException) {
                throw e
            } catch (e: IOException) {
                throw UpdateCheckException(UpdateCheckCode.NETWORK, e.message)
            }

        when {
            response.status == HTTP_NOT_MODIFIED ->
                return CheckOutcome(notModified = true, release = null, etag = config.lastEtag)
            response.status == HTTP_FORBIDDEN || response.status == HTTP_TOO_MANY_REQUESTS -> {
                if (response.rateLimitRemaining == 0 || response.status == HTTP_TOO_MANY_REQUESTS) {
                    throw UpdateCheckException(
                        UpdateCheckCode.RATE_LIMITED,
                        retryAfterMs = response.rateLimitResetMs,
                    )
                }
                throw UpdateCheckException(UpdateCheckCode.STATUS, "GitHub answered with the status ${response.status}")
            }
            response.status !in 200..299 ->
                throw UpdateCheckException(UpdateCheckCode.STATUS, "GitHub answered with the status ${response.status}")
        }

        val releases = parseReleases(response.body)
        return CheckOutcome(
            notModified = false,
            release = select(releases, config, current),
            etag = response.etag,
        )
    }

    private fun parseReleases(body: ByteArray): JSONArray =
        try {
            JSONArray(body.toString(Charsets.UTF_8))
        } catch (e: Exception) {
            throw UpdateCheckException(UpdateCheckCode.INVALID_RESPONSE, e.message)
        }

    private fun select(
        releases: JSONArray,
        config: UpdateConfig,
        current: SemVer,
    ): ReleaseInfo? =
        (0 until releases.length())
            .mapNotNull { releases.optJSONObject(it) }
            .mapNotNull { candidate(it, config, current) }
            .maxByOrNull { it.first }
            ?.second

    @Suppress("ReturnCount")
    private fun candidate(
        item: JSONObject,
        config: UpdateConfig,
        current: SemVer,
    ): Pair<SemVer, ReleaseInfo>? {
        if (item.optBoolean("draft")) {
            return null
        }
        val version = SemVer.parse(item.optString("tag_name")) ?: return null
        if (!config.betaChannel && (item.optBoolean("prerelease") || version.isPreRelease)) {
            return null
        }
        if (version <= current) {
            return null
        }
        val assets = item.optJSONArray("assets") ?: return null
        val asset = asset(assets, "unison-v${version.text}-android.apk") ?: return null
        val checksums = asset(assets, "SHA256SUMS") ?: return null
        val signature = asset(assets, "SHA256SUMS.sig") ?: return null
        val urls = listOf(asset.url, checksums.url, signature.url)
        if (urls.any { !UpdateUrls.allowed(it) }) {
            return null
        }
        val info =
            ReleaseInfo(
                version = version.text,
                tagName = item.optString("tag_name"),
                name = item.optString("name"),
                body = truncateUtf8(item.optString("body"), UpdateLimits.MAX_BODY_BYTES),
                publishedAt = item.optString("published_at"),
                prerelease = item.optBoolean("prerelease"),
                htmlUrl = item.optString("html_url"),
                assetName = asset.name,
                assetUrl = asset.url,
                assetSize = asset.size,
                checksumsUrl = checksums.url,
                signatureUrl = signature.url,
            )
        return version to info
    }

    private fun asset(
        assets: JSONArray,
        name: String,
    ): ApiAsset? =
        (0 until assets.length())
            .mapNotNull { assets.optJSONObject(it) }
            .firstOrNull { it.optString("name") == name }
            ?.let { item ->
                ApiAsset(
                    name = name,
                    url = item.optString("browser_download_url"),
                    size = item.optLong("size"),
                )
            }

    private data class ApiAsset(
        val name: String,
        val url: String,
        val size: Long,
    )

    private companion object {
        const val HTTP_NOT_MODIFIED = 304
        const val HTTP_FORBIDDEN = 403
        const val HTTP_TOO_MANY_REQUESTS = 429
    }
}

/** Name of the app, its version and the device, sent with every request. */
internal fun defaultUserAgent(): String =
    "Unison/${BuildConfig.VERSION_NAME} (Android ${Build.VERSION.RELEASE}; API ${Build.VERSION.SDK_INT})"

/** Reads a stream up to [limit] bytes, failing when it carries more. */
internal fun readLimited(
    stream: InputStream,
    limit: Int,
): ByteArray {
    val bytes = ByteArrayOutputStream()
    val chunk = ByteArray(READ_CHUNK)
    while (true) {
        val read = stream.read(chunk)
        if (read < 0) {
            break
        }
        bytes.write(chunk, 0, read)
        if (bytes.size() > limit) {
            throw UpdateCheckException(UpdateCheckCode.INVALID_RESPONSE, "the answer is larger than $limit bytes")
        }
    }
    return bytes.toByteArray()
}

/** Cuts a text to [limit] UTF-8 bytes without splitting a character. */
internal fun truncateUtf8(
    text: String,
    limit: Int,
): String {
    if (text.toByteArray(Charsets.UTF_8).size <= limit) {
        return text
    }
    val out = StringBuilder()
    var used = 0
    var index = 0
    while (index < text.length) {
        val codePoint = text.codePointAt(index)
        val width = Character.charCount(codePoint)
        val bytes = utf8Length(codePoint)
        if (used + bytes > limit) {
            break
        }
        out.appendCodePoint(codePoint)
        used += bytes
        index += width
    }
    return out.toString()
}

/** Number of bytes a code point takes in UTF-8. */
private fun utf8Length(codePoint: Int): Int =
    when {
        codePoint <= 0x7F -> 1
        codePoint <= 0x7FF -> 2
        codePoint <= 0xFFFF -> 3
        else -> 4
    }

/** Size of one read of a request. */
private const val READ_CHUNK = 16 shl 10
