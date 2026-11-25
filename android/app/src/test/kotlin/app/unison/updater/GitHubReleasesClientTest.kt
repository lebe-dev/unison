package app.unison.updater

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/** Tests of the release selection and of the URLs of the client. */
internal class GitHubReleasesClientTest {
    private val apkUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/unison-v0.3.0-android.apk"
    private val sumsUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS"
    private val signatureUrl = "https://github.com/lebe-dev/unison/releases/download/0.3.0/SHA256SUMS.sig"

    @Test
    fun `the newest release with every artifact is offered`() {
        val http =
            fake(
                status = 200,
                body =
                    JSONArray()
                        .put(release(tag = "0.2.0", assetVersion = "0.2.0", prerelease = true))
                        .put(release(tag = "0.3.0"))
                        .put(release(tag = "0.2.1", assetVersion = "0.2.1"))
                        .toString(),
            )

        val outcome = client(http).check(UpdateConfig(), currentVersion = "0.2.0", manual = true)

        assertEquals("0.3.0", outcome.release?.version)
        assertEquals("unison-v0.3.0-android.apk", outcome.release?.assetName)
        assertFalse(outcome.notModified)
    }

    @Test
    fun `a draft is skipped`() {
        val http = fake(status = 200, body = JSONArray().put(release(tag = "0.3.0", draft = true)).toString())
        assertNull(client(http).check(UpdateConfig(), "0.2.0", manual = true).release)
    }

    @Test
    fun `a pre-release is skipped without the beta channel`() {
        val body = JSONArray().put(release(tag = "0.3.0-beta.1", assetVersion = "0.3.0-beta.1", prerelease = true)).toString()
        assertNull(client(fake(status = 200, body = body)).check(UpdateConfig(), "0.2.0", manual = true).release)
    }

    @Test
    fun `a pre-release is offered with the beta channel`() {
        val body = JSONArray().put(release(tag = "0.3.0-beta.1", assetVersion = "0.3.0-beta.1", prerelease = true)).toString()
        val outcome = client(fake(status = 200, body = body)).check(UpdateConfig(betaChannel = true), "0.2.0", manual = true)
        assertEquals("0.3.0-beta.1", outcome.release?.version)
    }

    @Test
    fun `a release without the manifest or its signature is skipped`() {
        val body =
            JSONArray()
                .put(release(tag = "0.3.0", sums = false))
                .put(release(tag = "0.4.0", signature = false))
                .toString()
        assertNull(client(fake(status = 200, body = body)).check(UpdateConfig(), "0.2.0", manual = true).release)
    }

    @Test
    fun `a tag that is not a version is skipped`() {
        val body =
            JSONArray()
                .put(release(tag = "nightly", assetVersion = "nightly"))
                .put(release(tag = "0.3.0"))
                .toString()
        assertEquals("0.3.0", client(fake(status = 200, body = body)).check(UpdateConfig(), "0.2.0", manual = true).release?.version)
    }

    @Test
    fun `a release that is not newer is skipped`() {
        val body = JSONArray().put(release(tag = "0.2.0", assetVersion = "0.2.0")).toString()
        assertNull(client(fake(status = 200, body = body)).check(UpdateConfig(), "0.2.0", manual = true).release)
    }

    @Test
    fun `the answer keeps the etag of the request`() {
        val http = fake(status = 200, body = "[]", etag = "\"abc\"")
        assertEquals("\"abc\"", client(http).check(UpdateConfig(), "0.2.0", manual = true).etag)
    }

    @Test
    fun `an unchanged answer is reported`() {
        val http = fake(status = 304, body = "", etag = null)
        val outcome = client(http).check(UpdateConfig(lastEtag = "\"abc\""), "0.2.0", manual = false)
        assertTrue(outcome.notModified)
        assertEquals("\"abc\"", outcome.etag)
        assertNull(outcome.release)
    }

    @Test
    fun `the cached tag is sent only in the background`() {
        val background = fake(status = 200, body = "[]")
        client(background).check(UpdateConfig(lastEtag = "\"abc\""), "0.2.0", manual = false)
        assertEquals("\"abc\"", background.requests.single()["If-None-Match"])

        val manual = fake(status = 200, body = "[]")
        client(manual).check(UpdateConfig(lastEtag = "\"abc\""), "0.2.0", manual = true)
        assertNull(manual.requests.single()["If-None-Match"])
    }

    @Test
    fun `the headers name the client and the API`() {
        val http = fake(status = 200, body = "[]")
        client(http).check(UpdateConfig(), "0.2.0", manual = true)
        val headers = http.requests.single()
        assertEquals("application/vnd.github+json", headers["Accept"])
        assertEquals("2022-11-28", headers["X-GitHub-Api-Version"])
        assertEquals("test-agent", headers["User-Agent"])
    }

    @Test
    fun `a rate limit is reported with its reset time`() {
        val http = fake(status = 403, body = "{}", rateLimitRemaining = 0, rateLimitResetMs = 1_700_000_000_000L)
        val error = failure { client(http).check(UpdateConfig(), "0.2.0", manual = true) }
        assertEquals(UpdateCheckCode.RATE_LIMITED, error.code)
        assertEquals(1_700_000_000_000L, error.retryAfterMs)
    }

    @Test
    fun `another forbidden answer is a status error`() {
        val http = fake(status = 403, body = "{}", rateLimitRemaining = 42)
        val error = failure { client(http).check(UpdateConfig(), "0.2.0", manual = true) }
        assertEquals(UpdateCheckCode.STATUS, error.code)
    }

    @Test
    fun `an answer that is not JSON is refused`() {
        val error = failure { client(fake(status = 200, body = "not json")).check(UpdateConfig(), "0.2.0", manual = true) }
        assertEquals(UpdateCheckCode.INVALID_RESPONSE, error.code)
    }

    @Test
    fun `a build with an unreadable version is refused`() {
        val error = failure { client(fake(status = 200, body = "[]")).check(UpdateConfig(), "dev", manual = true) }
        assertEquals(UpdateCheckCode.INVALID_VERSION, error.code)
    }

    @Test
    fun `an artifact on another host is refused`() {
        val body =
            JSONArray()
                .put(
                    release(tag = "0.3.0").apply {
                        getJSONArray("assets")
                            .getJSONObject(0)
                            .put("browser_download_url", "https://example.com/unison-v0.3.0-android.apk")
                    },
                ).toString()
        assertNull(client(fake(status = 200, body = body)).check(UpdateConfig(), "0.2.0", manual = true).release)
    }

    @Test
    fun `only HTTPS addresses of GitHub are allowed`() {
        assertTrue(UpdateUrls.allowed("https://github.com/lebe-dev/unison/releases/download/0.3.0/file"))
        assertTrue(UpdateUrls.allowed("https://api.github.com/repos/lebe-dev/unison/releases"))
        assertTrue(UpdateUrls.allowed("https://objects.githubusercontent.com/some/file"))
        assertTrue(UpdateUrls.allowed("https://release-assets.githubusercontent.com/some/file"))

        assertFalse(UpdateUrls.allowed("http://github.com/lebe-dev/unison"))
        assertFalse(UpdateUrls.allowed("https://example.com/file"))
        assertFalse(UpdateUrls.allowed("https://github.com.evil.example/file"))
        assertFalse(UpdateUrls.allowed("ftp://github.com/file"))
        assertFalse(UpdateUrls.allowed("not a url"))
    }

    @Test
    fun `a long changelog is cut`() {
        val body = "x".repeat(UpdateLimits.MAX_BODY_BYTES + 1024)
        val json = JSONArray().put(release(tag = "0.3.0", body = body)).toString()
        val release = client(fake(status = 200, body = json)).check(UpdateConfig(), "0.2.0", manual = true).release
        assertEquals(UpdateLimits.MAX_BODY_BYTES, release?.body?.toByteArray()?.size)
    }

    private fun client(http: FakeHttp): GitHubReleasesClient =
        GitHubReleasesClient(
            apiUrl = API_URL,
            http = http,
            userAgent = "test-agent",
        )

    private fun fake(
        status: Int,
        body: String,
        etag: String? = null,
        rateLimitRemaining: Int? = null,
        rateLimitResetMs: Long? = null,
    ): FakeHttp =
        FakeHttp(
            UpdateHttpResponse(
                status = status,
                body = body.toByteArray(),
                etag = etag,
                rateLimitRemaining = rateLimitRemaining,
                rateLimitResetMs = rateLimitResetMs,
            ),
        )

    /** One release of the answer of GitHub. */
    private fun release(
        tag: String,
        assetVersion: String = tag,
        prerelease: Boolean = false,
        draft: Boolean = false,
        body: String = "Notes of $tag",
        sums: Boolean = true,
        signature: Boolean = true,
    ): JSONObject {
        val assets =
            JSONArray().put(
                JSONObject()
                    .put("name", "unison-v$assetVersion-android.apk")
                    .put("browser_download_url", apkUrl)
                    .put("size", 1024),
            )
        if (sums) {
            assets.put(JSONObject().put("name", "SHA256SUMS").put("browser_download_url", sumsUrl).put("size", 64))
        }
        if (signature) {
            assets.put(JSONObject().put("name", "SHA256SUMS.sig").put("browser_download_url", signatureUrl).put("size", 88))
        }
        return JSONObject()
            .put("tag_name", tag)
            .put("name", "Unison $tag")
            .put("body", body)
            .put("published_at", "2026-11-24T10:20:30Z")
            .put("prerelease", prerelease)
            .put("draft", draft)
            .put("html_url", "https://github.com/lebe-dev/unison/releases/tag/$tag")
            .put("assets", assets)
    }

    /** The failure of a check. */
    private fun failure(block: () -> Unit): UpdateCheckException =
        try {
            block()
            error("the check did not fail")
        } catch (e: UpdateCheckException) {
            e
        }

    /** A client whose answer the test writes. */
    private class FakeHttp(
        private val response: UpdateHttpResponse,
    ) : UpdateHttp {
        val requests = mutableListOf<Map<String, String>>()

        override fun get(
            url: String,
            headers: Map<String, String>,
        ): UpdateHttpResponse {
            requests += headers
            return response
        }
    }

    private companion object {
        const val API_URL = "https://api.github.com/repos/lebe-dev/unison/releases?per_page=20"
    }
}
