package app.unison.updater

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/** Tests of the update settings. */
internal class UpdateConfigTest {
    @Test
    fun `the defaults leave the checks off`() {
        val config = UpdateConfig.DEFAULT
        assertEquals(false, config.enabled)
        assertEquals(false, config.betaChannel)
        assertEquals(UpdateCheckInterval.DAILY, config.interval)
        assertEquals(0L, config.lastCheckedMs)
        assertNull(config.dismissedVersion)
        assertNull(config.lastEtag)
        assertNull(config.cachedRelease)
    }

    @Test
    fun `the intervals carry their hours`() {
        assertEquals(6L, UpdateCheckInterval.SIX_HOURS.hours)
        assertEquals(24L, UpdateCheckInterval.DAILY.hours)
        assertEquals(168L, UpdateCheckInterval.WEEKLY.hours)
    }

    @Test
    fun `an unknown interval falls back to daily`() {
        assertEquals(UpdateCheckInterval.DAILY, UpdateCheckInterval.fromValue("hourly"))
        assertEquals(UpdateCheckInterval.DAILY, UpdateCheckInterval.fromValue(null))
        assertEquals(UpdateCheckInterval.WEEKLY, UpdateCheckInterval.fromValue("weekly"))
    }

    @Test
    fun `a channel switch drops the answer of the old channel`() {
        val config =
            UpdateConfig(
                betaChannel = false,
                lastEtag = "\"abc\"",
                dismissedVersion = "0.3.0",
                cachedRelease = release("0.3.0"),
            )

        val switched = config.withBetaChannel(true)

        assertEquals(true, switched.betaChannel)
        assertNull(switched.lastEtag)
        assertNull(switched.dismissedVersion)
        assertNull(switched.cachedRelease)
    }

    @Test
    fun `the same channel keeps the cached answer`() {
        val config = UpdateConfig(betaChannel = true, lastEtag = "\"abc\"", dismissedVersion = "0.3.0")
        assertEquals(config, config.withBetaChannel(true))
    }

    @Test
    fun `a cached release is offered only when it is newer`() {
        val config = UpdateConfig(cachedRelease = release("0.3.0"))
        assertEquals("0.3.0", config.cachedUpdate("0.2.0")?.version)
        assertNull(config.cachedUpdate("0.3.0"))
        assertNull(config.cachedUpdate("0.4.0"))
        assertNull(config.cachedUpdate("dev"))
    }

    @Test
    fun `a release survives its JSON form`() {
        val original = release("0.3.0-beta.1")
        val read = ReleaseInfo.fromJson(original.toJson())
        assertEquals(original, read)
    }

    @Test
    fun `a broken JSON release is refused`() {
        assertNull(ReleaseInfo.fromJson("not json"))
        assertNull(ReleaseInfo.fromJson("""{"version": "0.3.0"}"""))
    }

    private fun release(version: String): ReleaseInfo =
        ReleaseInfo(
            version = version,
            tagName = version,
            name = "Unison $version",
            body = "Notes",
            publishedAt = "2026-11-24T10:20:30Z",
            prerelease = version.contains('-'),
            htmlUrl = "https://github.com/lebe-dev/unison/releases/tag/$version",
            assetName = "unison-v$version-android.apk",
            assetUrl = "https://github.com/lebe-dev/unison/releases/download/$version/unison-v$version-android.apk",
            assetSize = 1024,
            checksumsUrl = "https://github.com/lebe-dev/unison/releases/download/$version/SHA256SUMS",
            signatureUrl = "https://github.com/lebe-dev/unison/releases/download/$version/SHA256SUMS.sig",
        )
}
